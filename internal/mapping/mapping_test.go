package mapping

import (
	"reflect"
	"testing"

	"github.com/crazmoe/bt-headset-hid/internal/hfp"
	"github.com/crazmoe/bt-headset-hid/internal/hiddesc"
)

// Wire bytes of the reports, see package hiddesc.
var (
	onHook   = []byte{hiddesc.ReportTelephony, 0x00}
	offHook  = []byte{hiddesc.ReportTelephony, 0x01}
	redial   = []byte{hiddesc.ReportTelephony, 0x04}
	redialOH = []byte{hiddesc.ReportTelephony, 0x05}
	drop     = []byte{hiddesc.ReportTelephony, 0x08}
	volUp    = []byte{hiddesc.ReportConsumer, 0x01}
	volDown  = []byte{hiddesc.ReportConsumer, 0x02}
	volNone  = []byte{hiddesc.ReportConsumer, 0x00}
)

func expect(t *testing.T, m *Mapper, e hfp.Event, want ...[]byte) {
	t.Helper()
	got := m.Event(e)
	if len(want) == 0 {
		want = nil
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Event(%v %d) = % x, want % x", e.Kind, e.Value, got, want)
	}
}

func output(t *testing.T, m *Mapper, l hiddesc.LEDs, want hfp.CallState) {
	t.Helper()
	s, err := m.Output(l.Pack())
	if err != nil || s != want {
		t.Fatalf("Output(%+v) = %v, %v, want %v", l, s, err, want)
	}
}

func TestIncomingCallAnswerAndHangUp(t *testing.T) {
	m := New()
	output(t, m, hiddesc.LEDs{Ring: true}, hfp.Incoming)
	expect(t, m, hfp.Event{Kind: hfp.Answer}, offHook)
	expect(t, m, hfp.Event{Kind: hfp.Answer}) // already off-hook
	output(t, m, hiddesc.LEDs{OffHook: true}, hfp.Active)
	expect(t, m, hfp.Event{Kind: hfp.HangUp}, onHook)
	output(t, m, hiddesc.LEDs{}, hfp.Idle)
	expect(t, m, hfp.Event{Kind: hfp.HangUp}) // nothing to hang up
}

func TestRejectWhileRinging(t *testing.T) {
	m := New()
	output(t, m, hiddesc.LEDs{Ring: true}, hfp.Incoming)
	expect(t, m, hfp.Event{Kind: hfp.HangUp}, drop, onHook)
	if m.Hook() {
		t.Fatal("off-hook after reject")
	}
}

func TestButtonToggles(t *testing.T) {
	m := New()
	expect(t, m, hfp.Event{Kind: hfp.Button}, offHook)
	expect(t, m, hfp.Event{Kind: hfp.Button}, onHook)
	// A call started in the softphone: the next press hangs up.
	output(t, m, hiddesc.LEDs{OffHook: true}, hfp.Active)
	expect(t, m, hfp.Event{Kind: hfp.Button}, onHook)
}

func TestRedialKeepsHookState(t *testing.T) {
	m := New()
	expect(t, m, hfp.Event{Kind: hfp.Redial}, redial, onHook)
	expect(t, m, hfp.Event{Kind: hfp.Answer}, offHook)
	expect(t, m, hfp.Event{Kind: hfp.Redial}, redialOH, offHook)
}

func TestSpeakerGain(t *testing.T) {
	m := New()
	expect(t, m, hfp.Event{Kind: hfp.SpeakerGain, Value: 8}) // baseline
	expect(t, m, hfp.Event{Kind: hfp.SpeakerGain, Value: 9}, volUp, volNone)
	expect(t, m, hfp.Event{Kind: hfp.SpeakerGain, Value: 9})
	expect(t, m, hfp.Event{Kind: hfp.SpeakerGain, Value: 7}, volDown, volNone, volDown, volNone)
	// The host set the volume; the headset echoing it is not a key press.
	m.SetSpeakerGain(12)
	expect(t, m, hfp.Event{Kind: hfp.SpeakerGain, Value: 12})
	// Out of range values are clamped.
	expect(t, m, hfp.Event{Kind: hfp.SpeakerGain, Value: 99}, volUp, volNone, volUp, volNone, volUp, volNone)
}

func TestUnmappedEvents(t *testing.T) {
	m := New()
	for _, k := range []hfp.EventKind{hfp.Dial, hfp.VoiceRecognition, hfp.MicGain, 0} {
		expect(t, m, hfp.Event{Kind: k, Value: 1})
	}
}

func TestOutputStatePriority(t *testing.T) {
	m := New()
	// Off-hook wins over ring (call waiting).
	output(t, m, hiddesc.LEDs{OffHook: true, Ring: true}, hfp.Active)
	output(t, m, hiddesc.LEDs{Mute: true, Hold: true}, hfp.Idle)
	if l := m.LEDs(); !l.Mute || !l.Hold {
		t.Errorf("LEDs = %+v", l)
	}
}

func TestOutputRejectsBadReport(t *testing.T) {
	m := New()
	output(t, m, hiddesc.LEDs{Ring: true}, hfp.Incoming)
	s, err := m.Output([]byte{hiddesc.ReportTelephony, 0x01})
	if err == nil || s != hfp.Incoming || m.Hook() {
		t.Fatalf("Output(bad) = %v, %v, hook %v", s, err, m.Hook())
	}
}
