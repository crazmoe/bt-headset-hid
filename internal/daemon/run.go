package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/avrcp"
	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/hfp"
	"github.com/crazmoe/bt-headset-hid/internal/hiddesc"
	"github.com/crazmoe/bt-headset-hid/internal/modem"
	"github.com/crazmoe/bt-headset-hid/internal/ofono"
	"github.com/crazmoe/bt-headset-hid/internal/sco"
	"github.com/crazmoe/bt-headset-hid/internal/uhid"
)

// Mode selects who talks HFP to the headset.
type Mode string

const (
	// ModePipeWire: PipeWire owns HFP and the audio; we pose as its modem
	// and only see answer and hang-up.
	ModePipeWire Mode = "pipewire"
	// ModeHFP: we own HFP/HSP and see every button and volume change, but
	// there is no call audio.
	ModeHFP Mode = "hfp"
	// ModeOFono: we own HFP/HSP like in ModeHFP and pose as oFono, so
	// PipeWire's oFono backend takes the call audio from us. ModemManager
	// stays free for real modems.
	ModeOFono Mode = "ofono"
)

// Timeouts for opening the call audio in ModeOFono. PipeWire waits for the
// answer and stalls meanwhile, so they are short.
const (
	codecTimeout = 2 * time.Second
	scoTimeout   = 5 * time.Second
)

// Default USB IDs: pid.codes (0x1209) test PID. Softphones that only accept
// known vendors may need other values.
const (
	DefaultVendor  = 0x1209
	DefaultProduct = 0x0001
)

// Config configures Run.
type Config struct {
	Mode    Mode
	Name    string
	Vendor  uint32
	Product uint32
	// GrabMediaKeys takes the headset's AVRCP play/pause key away from the
	// media player in ModePipeWire and ModeOFono and uses it as the call
	// button, for headsets that send their call button that way outside a
	// call.
	GrabMediaKeys bool
	Logf          func(format string, args ...any)
}

// modemQuiet is how long after an answer or hang-up from HFP a media key
// press is ignored, in case the headset reported the same press both ways.
const modemQuiet = 1500 * time.Millisecond

// Run creates the HID device, serves the chosen mode on the system bus and
// returns when ctx ends or something fails.
func Run(ctx context.Context, cfg Config) error {
	logf := cfg.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if cfg.Mode != ModePipeWire && cfg.Mode != ModeHFP && cfg.Mode != ModeOFono {
		return fmt.Errorf("unknown mode %q", cfg.Mode)
	}
	dev, err := uhid.OpenDevice(uhid.Config{
		Name: cfg.Name, Bus: uhid.BusBluetooth, Vendor: cfg.Vendor, Product: cfg.Product,
		Version: 1, Descriptor: hiddesc.Descriptor,
	})
	if err != nil {
		return fmt.Errorf("create HID device: %w", err)
	}
	defer dev.Close()
	b := NewBridge(dev)
	errc := make(chan error, 2)
	go func() { errc <- fmt.Errorf("HID device: %w", b.ServeHID(logf)) }()

	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("system bus: %w", err)
	}
	defer conn.Close()

	headsetEvent := func(e hfp.Event) {
		logf("headset: %v %d", e.Kind, e.Value)
		if err := b.HeadsetEvent(e); err != nil {
			logf("input report: %v", err)
		}
	}
	// hfpEvent is headsetEvent for what arrives over HFP (from PipeWire or
	// our own connection), as opposed to AVRCP.
	var mu sync.Mutex
	var lastHFP time.Time
	hfpEvent := func(e hfp.Event) {
		if e.Kind == hfp.Answer || e.Kind == hfp.HangUp || e.Kind == hfp.Button {
			mu.Lock()
			lastHFP = time.Now()
			mu.Unlock()
		}
		headsetEvent(e)
	}
	if cfg.GrabMediaKeys && cfg.Mode != ModeHFP {
		w := &avrcp.Watcher{
			Sysfs: "/sys", Dev: "/dev", Interval: 2 * time.Second, Debounce: 300 * time.Millisecond,
			Logf: logf,
			OnEvent: func(e hfp.Event) {
				mu.Lock()
				quiet := time.Since(lastHFP) < modemQuiet
				mu.Unlock()
				if quiet {
					return
				}
				headsetEvent(e)
			},
		}
		go w.Run(ctx)
	}

	switch cfg.Mode {
	case ModePipeWire:
		srv, err := modem.Serve(conn, hfpEvent)
		if err != nil {
			return err
		}
		defer srv.Close()
		st := &settler{delay: time.Second, set: func(s hfp.CallState) {
			if err := srv.SetState(s); err != nil {
				logf("modem: %v", err)
			}
		}}
		b.OnState(func(s hfp.CallState) {
			logf("call state: %v", s)
			st.SetState(s)
		})
		logf("serving %s for PipeWire", modem.Service)
	case ModeHFP:
		ps, err := bluez.Register(conn, hfp.DefaultFeatures, hfp.HFP, hfp.HSP)
		if err != nil {
			return err
		}
		defer ps.Close()
		h := &headsets{bridge: b, conns: ps.Connections(), forget: ps.Forget, features: hfp.DefaultFeatures,
			onEvent: func(_ bluez.Connection, e hfp.Event) { hfpEvent(e) }, logf: logf}
		b.OnState(h.setState)
		go h.serve(ctx)
		logf("HFP/HSP Audio Gateway registered with BlueZ")
	case ModeOFono:
		srv, err := ofono.Serve(conn, logf)
		if err != nil {
			return err
		}
		defer srv.Close()
		features := hfp.DefaultFeatures | hfp.FeatureCodecNegotiation
		ps, err := bluez.Register(conn, features, hfp.HFP, hfp.HSP)
		if err != nil {
			return err
		}
		defer ps.Close()
		// The headset whose button answered or dialled takes the call
		// audio ("bt-headset-hid session" switches it), until the call ends.
		onEvent := func(c bluez.Connection, e hfp.Event) {
			switch e.Kind {
			case hfp.Answer, hfp.Button, hfp.Redial, hfp.Dial:
				srv.SetCallDevice(c.Address())
			}
			hfpEvent(e)
		}
		h := &headsets{bridge: b, conns: ps.Connections(), forget: ps.Forget, features: features, all: true, onEvent: onEvent, logf: logf}
		h.onConnected = func(c bluez.Connection, sess *Session) {
			if c.Profile != hfp.HFP {
				return // PipeWire takes the audio of HFP only
			}
			if err := addCard(conn, srv, c, sess, logf); err != nil {
				logf("headset %s: no call audio: %v", c.Address(), err)
			}
		}
		h.onClosed = func(c bluez.Connection) {
			if err := srv.RemoveCard(c.Device); err != nil {
				logf("headset %s: %v", c.Address(), err)
			}
		}
		st := &settler{delay: time.Second, set: func(s hfp.CallState) {
			h.setState(s)
			srv.SetCallState(s)
			if s == hfp.Idle {
				srv.SetCallDevice("")
			}
		}}
		b.OnState(func(s hfp.CallState) {
			logf("call state: %v", s)
			st.SetState(s)
		})
		go h.serve(ctx)
		logf("HFP/HSP Audio Gateway registered with BlueZ, serving %s for PipeWire", ofono.Service)
	}

	select {
	case <-ctx.Done():
		return nil
	case err := <-errc:
		return err
	}
}

// addCard offers the call audio of the headset of c to PipeWire.
func addCard(conn *dbus.Conn, srv *ofono.Server, c bluez.Connection, sess *Session, logf func(string, ...any)) error {
	local, err := bluez.LocalAddress(conn, c.Device)
	if err != nil {
		return err
	}
	remote := c.Address()
	// Some adapters cannot carry mSBC; after the first failure the card
	// keeps to CVSD, otherwise every call would try again.
	var mu sync.Mutex
	noMSBC := false
	return srv.AddCard(c.Device, &ofono.Card{
		Remote: remote, Local: local,
		Acquire: func(codecs []byte) (int, byte, error) {
			mu.Lock()
			defer mu.Unlock()
			var allowed []int
			for _, c := range codecs {
				if c != hfp.CodecMSBC || !noMSBC {
					allowed = append(allowed, int(c))
				}
			}
			codec, err := sess.NegotiateCodec(allowed, codecTimeout)
			if err != nil {
				return -1, 0, err
			}
			fd, err := sco.Dial(local, remote, codec == hfp.CodecMSBC, scoTimeout)
			if err != nil && codec == hfp.CodecMSBC {
				logf("headset %s: no wideband audio link (%v), using CVSD from now on", remote, err)
				noMSBC = true
				if codec, err = sess.NegotiateCodec([]int{hfp.CodecCVSD}, codecTimeout); err != nil {
					return -1, 0, err
				}
				fd, err = sco.Dial(local, remote, false, scoTimeout)
			}
			return fd, byte(codec), err
		},
	})
}

// headsets keeps the headset sessions in ModeHFP and ModeOFono.
type headsets struct {
	bridge   *Bridge
	conns    <-chan bluez.Connection
	forget   func(dbus.ObjectPath, *os.File) // see bluez.Profiles.Forget
	features int
	// all serves every connected headset; otherwise only the most recently
	// connected one.
	all     bool
	onEvent func(bluez.Connection, hfp.Event) // button presses and gain changes
	logf    func(string, ...any)
	// onConnected runs when a headset can take part in calls, onClosed
	// when its connection ended; both may be nil.
	onConnected func(bluez.Connection, *Session)
	onClosed    func(bluez.Connection)

	mu  sync.Mutex
	cur map[dbus.ObjectPath]*Session
}

// setState runs with the bridge locked, so it must not call into the bridge.
func (h *headsets) setState(s hfp.CallState) {
	h.mu.Lock()
	sessions := make([]*Session, 0, len(h.cur))
	for _, sess := range h.cur {
		sessions = append(sessions, sess)
	}
	h.mu.Unlock()
	for _, sess := range sessions {
		if err := sess.SetState(s); err != nil {
			h.logf("headset: %v", err)
		}
	}
}

func (h *headsets) serve(ctx context.Context) {
	h.mu.Lock()
	if h.cur == nil {
		h.cur = map[dbus.ObjectPath]*Session{}
	}
	h.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			for _, sess := range h.cur {
				sess.Close()
			}
			h.mu.Unlock()
			return
		case c := <-h.conns:
			h.logf("headset %s connected (%v)", c.Address(), c.Profile)
			var onEvent func(hfp.Event)
			if h.onEvent != nil {
				onEvent = func(e hfp.Event) { h.onEvent(c, e) }
			}
			sess := NewSession(c.File, c.Profile, h.features, onEvent, h.logf)
			if h.onConnected != nil {
				sess.OnConnected(func() { h.onConnected(c, sess) })
			}
			// Read the bridge state before taking h.mu; setState runs with
			// the bridge locked and takes h.mu.
			if err := sess.SetState(h.bridge.State()); err != nil {
				h.logf("headset: %v", err)
			}
			h.mu.Lock()
			var old []*Session
			for dev, s := range h.cur {
				if !h.all || dev == c.Device {
					old = append(old, s)
					delete(h.cur, dev)
				}
			}
			h.cur[c.Device] = sess
			h.mu.Unlock()
			for _, s := range old {
				s.Close()
			}
			go func() {
				err := sess.Run()
				if err != nil && !errors.Is(err, context.Canceled) {
					h.logf("headset %s: %v", c.Address(), err)
				}
				h.logf("headset %s disconnected", c.Address())
				h.forget(c.Device, c.File)
				h.mu.Lock()
				last := h.cur[c.Device] == sess
				if last {
					delete(h.cur, c.Device)
				}
				h.mu.Unlock()
				if last && h.onClosed != nil {
					h.onClosed(c)
				}
			}()
		}
	}
}
