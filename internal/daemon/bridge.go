// Package daemon wires the headset side (PipeWire's modem calls or our own
// HFP connection) to the virtual HID device.
package daemon

import (
	"errors"
	"sync"
	"syscall"

	"github.com/crazmoe/bt-headset-hid/internal/hfp"
	"github.com/crazmoe/bt-headset-hid/internal/hiddesc"
	"github.com/crazmoe/bt-headset-hid/internal/mapping"
	"github.com/crazmoe/bt-headset-hid/internal/uhid"
)

// HID is the virtual device as the bridge needs it.
type HID interface {
	Input(report []byte) error
	ReplyGetReport(id uint32, errno syscall.Errno, data []byte) error
	ReplySetReport(id uint32, errno syscall.Errno) error
	ReadEvent() (uhid.Event, error)
}

// Bridge connects headset events and the HID device through a
// mapping.Mapper. It is safe for concurrent use.
type Bridge struct {
	hid HID

	mu      sync.Mutex
	m       *mapping.Mapper
	onState func(hfp.CallState)
}

// NewBridge returns a bridge sending input reports to hid.
func NewBridge(hid HID) *Bridge { return &Bridge{hid: hid, m: mapping.New()} }

// OnState sets the function that receives the call state whenever the host
// sends an LED report. It is called with the bridge locked, so state changes
// arrive in order; it must not call back into the bridge.
func (b *Bridge) OnState(f func(hfp.CallState)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onState = f
}

// HeadsetEvent sends the input reports for a headset button or gain change.
func (b *Bridge) HeadsetEvent(e hfp.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var errs []error
	for _, r := range b.m.Event(e) {
		errs = append(errs, b.hid.Input(r))
	}
	return errors.Join(errs...)
}

// SetSpeakerGain records a gain sent to the headset, see mapping.SetSpeakerGain.
func (b *Bridge) SetSpeakerGain(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.m.SetSpeakerGain(n)
}

// State returns the call state from the last LED report.
func (b *Bridge) State() hfp.CallState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.m.State()
}

func (b *Bridge) hostOutput(report []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, err := b.m.Output(report)
	if err != nil {
		return err
	}
	if b.onState != nil {
		b.onState(s)
	}
	return nil
}

func (b *Bridge) getReport(rnum uint8) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch rnum {
	case hiddesc.ReportTelephony:
		return hiddesc.Telephony{HookSwitch: b.m.Hook()}.Pack(), true
	case hiddesc.ReportLEDs:
		return b.m.LEDs().Pack(), true
	case hiddesc.ReportConsumer:
		return hiddesc.Consumer{}.Pack(), true
	}
	return nil, false
}

// ServeHID reads events from the HID device until it fails and applies the
// LED reports the host sends. Malformed reports are answered with EINVAL and
// otherwise ignored; logf, if set, reports them.
func (b *Bridge) ServeHID(logf func(format string, args ...any)) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	for {
		e, err := b.hid.ReadEvent()
		if err != nil {
			return err
		}
		switch e.Type {
		case uhid.Output:
			if e.RType != uhid.OutputReport {
				continue
			}
			if err := b.hostOutput(e.Data); err != nil {
				logf("output report % x: %v", e.Data, err)
			} else {
				logLEDs(logf, e.Data)
			}
		case uhid.SetReport:
			errno := syscall.Errno(0)
			if e.RType != uhid.OutputReport {
				errno = syscall.EIO
			} else if err := b.hostOutput(e.Data); err != nil {
				logf("set report % x: %v", e.Data, err)
				errno = syscall.EINVAL
			} else {
				logLEDs(logf, e.Data)
			}
			if err := b.hid.ReplySetReport(e.ID, errno); err != nil {
				return err
			}
		case uhid.GetReport:
			data, ok := b.getReport(e.RNum)
			errno := syscall.Errno(0)
			if !ok {
				errno = syscall.EINVAL
			}
			if err := b.hid.ReplyGetReport(e.ID, errno, data); err != nil {
				return err
			}
		}
	}
}

// logLEDs logs an LED report the host sent, to see what the softphone does.
func logLEDs(logf func(format string, args ...any), report []byte) {
	if l, err := hiddesc.UnpackLEDs(report); err == nil {
		logf("softphone LEDs: off-hook=%v ring=%v mute=%v hold=%v", l.OffHook, l.Ring, l.Mute, l.Hold)
	}
}
