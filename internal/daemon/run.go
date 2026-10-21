package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/avrcp"
	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/hfp"
	"github.com/crazmoe/bt-headset-hid/internal/hiddesc"
	"github.com/crazmoe/bt-headset-hid/internal/modem"
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
	// media player in ModePipeWire and uses it as the call button, for
	// headsets that send their call button that way outside a call.
	GrabMediaKeys bool
	Logf          func(format string, args ...any)
}

// modemQuiet is how long after an answer or hang-up from PipeWire a media key
// press is ignored, in case the headset reported the same press both ways.
const modemQuiet = 1500 * time.Millisecond

// Run creates the HID device, serves the chosen mode on the system bus and
// returns when ctx ends or something fails.
func Run(ctx context.Context, cfg Config) error {
	logf := cfg.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if cfg.Mode != ModePipeWire && cfg.Mode != ModeHFP {
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

	switch cfg.Mode {
	case ModePipeWire:
		var mu sync.Mutex
		var lastModem time.Time
		srv, err := modem.Serve(conn, func(e hfp.Event) {
			mu.Lock()
			lastModem = time.Now()
			mu.Unlock()
			headsetEvent(e)
		})
		if err != nil {
			return err
		}
		defer srv.Close()
		if cfg.GrabMediaKeys {
			w := &avrcp.Watcher{
				Sysfs: "/sys", Dev: "/dev", Interval: 2 * time.Second, Debounce: 300 * time.Millisecond,
				Logf: logf,
				OnEvent: func(e hfp.Event) {
					mu.Lock()
					quiet := time.Since(lastModem) < modemQuiet
					mu.Unlock()
					if quiet {
						return
					}
					headsetEvent(e)
				},
			}
			go w.Run(ctx)
		}
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
		h := &headsets{bridge: b, profiles: ps, onEvent: headsetEvent, logf: logf}
		b.OnState(h.setState)
		go h.serve(ctx)
		logf("HFP/HSP Audio Gateway registered with BlueZ")
	}

	select {
	case <-ctx.Done():
		return nil
	case err := <-errc:
		return err
	}
}

// headsets keeps the current headset session in ModeHFP. Only the most
// recently connected headset is served.
type headsets struct {
	bridge   *Bridge
	profiles *bluez.Profiles
	onEvent  func(hfp.Event)
	logf     func(string, ...any)

	mu  sync.Mutex
	cur *Session
}

// setState runs with the bridge locked, so it must not call into the bridge.
func (h *headsets) setState(s hfp.CallState) {
	h.mu.Lock()
	cur := h.cur
	h.mu.Unlock()
	if cur != nil {
		if err := cur.SetState(s); err != nil {
			h.logf("headset: %v", err)
		}
	}
}

func (h *headsets) serve(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			if h.cur != nil {
				h.cur.Close()
			}
			h.mu.Unlock()
			return
		case c := <-h.profiles.Connections():
			h.logf("headset %s connected (%v)", c.Address(), c.Profile)
			sess := NewSession(c.File, c.Profile, hfp.DefaultFeatures, h.onEvent, h.logf)
			// Read the bridge state before taking h.mu; setState runs with
			// the bridge locked and takes h.mu.
			if err := sess.SetState(h.bridge.State()); err != nil {
				h.logf("headset: %v", err)
			}
			h.mu.Lock()
			old := h.cur
			h.cur = sess
			h.mu.Unlock()
			if old != nil {
				old.Close()
			}
			go func() {
				err := sess.Run()
				if err != nil && !errors.Is(err, context.Canceled) {
					h.logf("headset %s: %v", c.Address(), err)
				}
				h.logf("headset %s disconnected", c.Address())
				h.profiles.Forget(c.Device, c.File)
				h.mu.Lock()
				if h.cur == sess {
					h.cur = nil
				}
				h.mu.Unlock()
			}()
		}
	}
}
