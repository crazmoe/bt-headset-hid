// Package mapping translates headset events into HID input reports and HID
// LED output reports from the host into the call state for the headset. It
// performs no I/O.
//
// Headset to host:
//
//	ATA        sets Hook Switch (answer)
//	AT+CHUP    clears Hook Switch; while ringing on-hook it sends Drop (reject)
//	AT+CKPD    toggles Hook Switch (HSP button)
//	AT+BLDN    Redial
//	AT+VGS     Volume Increment/Decrement, one press per gain step
//
// Relative usages (Flash, Redial, Drop, Phone Mute) and the volume keys are
// sent as a press report followed by a release report.
//
// Host to headset: the Off-Hook LED is the hook state as the host sees it and
// becomes hfp.Active; otherwise the Ring LED becomes hfp.Incoming, and no LED
// means hfp.Idle.
package mapping

import (
	"github.com/crazmoe/bt-headset-hid/internal/hfp"
	"github.com/crazmoe/bt-headset-hid/internal/hiddesc"
)

// Mapper holds the hook and LED state of one virtual device.
type Mapper struct {
	hook    bool
	leds    hiddesc.LEDs
	speaker int // last speaker gain, -1 if unknown
}

// New returns a Mapper with the device on-hook and all LEDs off.
func New() *Mapper { return &Mapper{speaker: -1} }

// Hook reports whether the device is off-hook.
func (m *Mapper) Hook() bool { return m.hook }

// LEDs returns the LED state last set by the host.
func (m *Mapper) LEDs() hiddesc.LEDs { return m.leds }

// State returns the call state derived from the LEDs.
func (m *Mapper) State() hfp.CallState {
	switch {
	case m.leds.OffHook:
		return hfp.Active
	case m.leds.Ring:
		return hfp.Incoming
	}
	return hfp.Idle
}

// Event returns the input reports for a headset event, in the order they must
// be sent. Events without a HID equivalent return nil.
func (m *Mapper) Event(e hfp.Event) [][]byte {
	switch e.Kind {
	case hfp.Answer:
		return m.setHook(true)
	case hfp.HangUp:
		if !m.hook && m.leds.Ring {
			return m.press(hiddesc.Telephony{Drop: true})
		}
		return m.setHook(false)
	case hfp.Button:
		return m.setHook(!m.hook)
	case hfp.Redial:
		return m.press(hiddesc.Telephony{Redial: true})
	case hfp.SpeakerGain:
		return m.speakerGain(e.Value)
	}
	return nil
}

// SetSpeakerGain records a gain the host sent to the headset (+VGS), so a
// headset echoing it back does not produce volume key presses.
func (m *Mapper) SetSpeakerGain(n int) { m.speaker = min(max(n, 0), 15) }

// Output handles an LED output report from the host and returns the call
// state to pass to hfp.AG.SetState. The hook state follows the Off-Hook LED
// without sending a report, since the host already knows it.
func (m *Mapper) Output(report []byte) (hfp.CallState, error) {
	l, err := hiddesc.UnpackLEDs(report)
	if err != nil {
		return m.State(), err
	}
	m.leds = l
	m.hook = l.OffHook
	return m.State(), nil
}

func (m *Mapper) setHook(on bool) [][]byte {
	if m.hook == on {
		return nil
	}
	m.hook = on
	return [][]byte{hiddesc.Telephony{HookSwitch: on}.Pack()}
}

// press returns a press and a release report for the relative usages set in
// t, keeping the current hook state in both.
func (m *Mapper) press(t hiddesc.Telephony) [][]byte {
	t.HookSwitch = m.hook
	return [][]byte{t.Pack(), hiddesc.Telephony{HookSwitch: m.hook}.Pack()}
}

func (m *Mapper) speakerGain(n int) [][]byte {
	old := m.speaker
	m.SetSpeakerGain(n)
	n = m.speaker
	if old < 0 || n == old {
		// The first gain is the headset's starting point, not a key press.
		return nil
	}
	key := hiddesc.Consumer{VolumeUp: n > old, VolumeDown: n < old}
	steps := max(n-old, old-n)
	reports := make([][]byte, 0, 2*steps)
	for range steps {
		reports = append(reports, key.Pack(), hiddesc.Consumer{}.Pack())
	}
	return reports
}
