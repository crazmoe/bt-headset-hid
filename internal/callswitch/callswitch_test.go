package callswitch

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/crazmoe/bt-headset-hid/internal/modem"
)

type fakeEnv struct {
	cards   []Card
	playing []string
	log     []string
	failSet bool
	output  string
	// moveOutput is the default output the desktop picks when a headset
	// leaves the music profile.
	moveOutput string
}

func (f *fakeEnv) Output() (string, error) { return f.output, nil }

func (f *fakeEnv) SetOutput(name string) error {
	f.log = append(f.log, "output "+name)
	f.output = name
	return nil
}

func (f *fakeEnv) Cards() ([]Card, error) { return f.cards, nil }

func (f *fakeEnv) SetProfile(c Card, profile string) error {
	if f.failSet {
		return fmt.Errorf("busy")
	}
	f.log = append(f.log, "profile "+c.Name+" "+profile)
	if IsPhone(profile) && f.moveOutput != "" {
		f.output = f.moveOutput
	}
	for i := range f.cards {
		if f.cards[i].Name == c.Name {
			f.cards[i].Profile = profile
		}
	}
	return nil
}

func (f *fakeEnv) Playing() ([]string, error) { return f.playing, nil }

func (f *fakeEnv) Pause(p string) error {
	f.log = append(f.log, "pause "+p)
	return nil
}

func (f *fakeEnv) Play(p string) error {
	f.log = append(f.log, "play "+p)
	return nil
}

func card(name, profile string) Card {
	return Card{Name: name, Profile: profile, Profiles: map[string]int{
		"off": 0, "a2dp-sink": 1, "a2dp-sink-sbc": 2, "headset-head-unit-cvsd": 3, "headset-head-unit": 4,
	}}
}

func TestSwitcher(t *testing.T) {
	env := &fakeEnv{
		cards:   []Card{card("bluez_card.JBL", "a2dp-sink"), card("bluez_card.JABRA", "headset-head-unit")},
		playing: []string{"org.mpris.MediaPlayer2.spotify"},
	}
	settled := 0
	s := &Switcher{Env: env, Settle: func() {
		settled++
		env.log = append(env.log, "settle")
	}}
	s.Call(true, Route{})
	s.Call(true, Route{})
	want := []string{"pause org.mpris.MediaPlayer2.spotify", "profile bluez_card.JBL headset-head-unit"}
	if !reflect.DeepEqual(env.log, want) || !s.InCall() {
		t.Fatalf("call start: %q, want %q", env.log, want)
	}
	env.log = nil
	s.Call(false, Route{})
	s.Call(false, Route{})
	want = []string{"profile bluez_card.JBL a2dp-sink", "settle", "play org.mpris.MediaPlayer2.spotify"}
	if !reflect.DeepEqual(env.log, want) || s.InCall() {
		t.Fatalf("call end: %q, want %q", env.log, want)
	}
	if env.cards[1].Profile != "headset-head-unit" {
		t.Errorf("headset already in the phone profile changed to %s", env.cards[1].Profile)
	}
}

func TestSwitcherDefaultOutput(t *testing.T) {
	// With two connected, only the one with the default output switches.
	jbl, jabra := card("bluez_card.JBL", "a2dp-sink"), card("bluez_card.JABRA", "a2dp-sink")
	jabra.Default = true
	env := &fakeEnv{cards: []Card{jbl, jabra}}
	s := &Switcher{Env: env}
	s.Call(true, Route{})
	s.Call(false, Route{})
	want := []string{"profile bluez_card.JABRA headset-head-unit", "profile bluez_card.JABRA a2dp-sink"}
	if !reflect.DeepEqual(env.log, want) {
		t.Errorf("log %q, want %q", env.log, want)
	}
}

func TestSwitcherRestoresOutput(t *testing.T) {
	jbl, jabra := card("bluez_card.JBL", "a2dp-sink"), card("bluez_card.JABRA", "a2dp-sink")
	jabra.Default = true
	env := &fakeEnv{cards: []Card{jbl, jabra}, output: "bluez_output.JABRA.1", moveOutput: "bluez_output.JBL.1"}
	s := &Switcher{Env: env, Settle: func() { env.log = append(env.log, "settle") }}
	s.Call(true, Route{})
	s.Call(false, Route{})
	want := []string{"profile bluez_card.JABRA headset-head-unit", "profile bluez_card.JABRA a2dp-sink", "settle", "output bluez_output.JABRA.1"}
	if !reflect.DeepEqual(env.log, want) {
		t.Errorf("log %q, want %q", env.log, want)
	}
}

func TestSwitcherNothingToDo(t *testing.T) {
	// Already in the phone profile and no music: nothing happens at all.
	env := &fakeEnv{cards: []Card{card("bluez_card.JABRA", "headset-head-unit-cvsd")}}
	s := &Switcher{Env: env, Settle: func() { t.Error("Settle without players") }}
	s.Call(true, Route{})
	s.Call(false, Route{})
	if env.log != nil {
		t.Errorf("log %q", env.log)
	}
}

func TestSwitcherUserChangedProfile(t *testing.T) {
	// Profiles the user changed during the call, and headsets gone, stay.
	env := &fakeEnv{cards: []Card{card("bluez_card.JBL", "a2dp-sink"), card("bluez_card.JABRA", "a2dp-sink-sbc")}}
	s := &Switcher{Env: env}
	s.Call(true, Route{})
	env.cards = []Card{card("bluez_card.JBL", "a2dp-sink")}
	env.log = nil
	s.Call(false, Route{})
	if env.log != nil {
		t.Errorf("log %q", env.log)
	}
}

func TestSwitcherSetFails(t *testing.T) {
	env := &fakeEnv{cards: []Card{card("bluez_card.JBL", "a2dp-sink")}, failSet: true}
	var logged []string
	s := &Switcher{Env: env, Log: func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }}
	s.Call(true, Route{})
	env.failSet = false
	s.Call(false, Route{})
	if env.log != nil || len(logged) != 1 {
		t.Errorf("log %q, logged %q", env.log, logged)
	}
}

func TestPhoneProfile(t *testing.T) {
	c := Card{Profiles: map[string]int{"a2dp-sink": 1, "headset-head-unit-msbc": 3, "headset-head-unit-cvsd": 2}}
	if got := c.phoneProfile(); got != "headset-head-unit-cvsd" {
		t.Errorf("phoneProfile = %q", got)
	}
	if got := (Card{Profiles: map[string]int{"a2dp-sink": 1}}).phoneProfile(); got != "" {
		t.Errorf("phoneProfile without HFP = %q", got)
	}
}

func TestBusy(t *testing.T) {
	for s, want := range map[modem.CallState]bool{
		modem.CallUnknown: false, modem.CallDialing: true, modem.CallRingingOut: true, modem.CallRingingIn: true,
		modem.CallActive: true, modem.CallHeld: true, modem.CallWaiting: true, modem.CallTerminated: false,
	} {
		if Busy(s) != want {
			t.Errorf("Busy(%d) = %v", s, !want)
		}
	}
}

const dump = `[
  { "id": 30, "type": "PipeWire:Interface:Node", "info": { "props": { "device.name": "bluez_card.AC_BF_71_12_34_56" } } },
  { "id": 41, "type": "PipeWire:Interface:Device", "info": {
      "props": { "device.name": "bluez_card.AC_BF_71_12_34_56", "device.api": "bluez5" },
      "params": {
        "EnumProfile": [
          { "index": 0, "name": "off", "description": "Off" },
          { "index": 1, "name": "a2dp-sink", "description": "High Fidelity Playback (A2DP Sink)" },
          { "index": 4, "name": "headset-head-unit", "description": "Headset Head Unit (HSP/HFP)" }
        ],
        "Profile": [ { "index": 1, "name": "a2dp-sink", "save": false } ]
      } } },
  { "id": 42, "type": "PipeWire:Interface:Device", "info": { "props": { "device.name": "alsa_card.pci-0000_00_1f.3" }, "params": {} } },
  { "id": 50, "type": "PipeWire:Interface:Node", "info": { "props": { "node.name": "bluez_output.AC_BF_71_12_34_56.1", "device.id": 41 } } },
  { "id": 51, "type": "PipeWire:Interface:Node", "info": { "props": { "node.name": "alsa_output.pci-0000_00_1f.3.analog-stereo", "device.id": 42 } } },
  { "id": 60, "type": "PipeWire:Interface:Metadata", "props": { "metadata.name": "default" }, "metadata": [
      { "subject": 0, "key": "default.configured.audio.sink", "type": "Spa:String:JSON", "value": { "name": "alsa_output.pci-0000_00_1f.3.analog-stereo" } },
      { "subject": 0, "key": "default.audio.sink", "type": "Spa:String:JSON", "value": { "name": "bluez_output.AC_BF_71_12_34_56.1" } }
  ] },
  { "id": 43, "type": "PipeWire:Interface:Device", "info": null }
]`

func TestParseOutput(t *testing.T) {
	out, err := ParseOutput([]byte(dump))
	if err != nil || out != "alsa_output.pci-0000_00_1f.3.analog-stereo" {
		t.Errorf("ParseOutput = %q, %v", out, err)
	}
	if out, err := ParseOutput([]byte("[]")); out != "" || err != nil {
		t.Errorf("ParseOutput without metadata = %q, %v", out, err)
	}
	if _, err := ParseOutput([]byte("x")); err == nil {
		t.Error("bad dump accepted")
	}
}

func TestParseCards(t *testing.T) {
	cards, err := ParseCards([]byte(dump), map[string]bool{"bluez_card.AC_BF_71_12_34_56": true, "bluez_card.50_C2_75_0D_4E_03": true})
	if err != nil {
		t.Fatal(err)
	}
	want := []Card{{ID: 41, Name: "bluez_card.AC_BF_71_12_34_56", Profile: "a2dp-sink",
		Profiles: map[string]int{"off": 0, "a2dp-sink": 1, "headset-head-unit": 4}, Default: true}}
	if !reflect.DeepEqual(cards, want) {
		t.Errorf("ParseCards = %+v", cards)
	}
	// pw-dump 1.0 may follow up with more arrays: an update and a removal.
	more := dump + `
[ { "id": 41, "type": "PipeWire:Interface:Device", "info": { "props": {}, "params": { "Profile": [ { "index": 4, "name": "headset-head-unit" } ] } } } ]
[ { "id": 42, "info": null } ]
[ { "id": 60, "type": "PipeWire:Interface:Metadata", "props": { "metadata.name": "default" }, "metadata": [
    { "subject": 0, "key": "default.audio.sink", "value": "{ \"name\": \"alsa_output.pci-0000_00_1f.3.analog-stereo\" }" } ] } ]`
	cards, err = ParseCards([]byte(more), map[string]bool{"bluez_card.AC_BF_71_12_34_56": true, "alsa_card.pci-0000_00_1f.3": true})
	if err != nil || len(cards) != 1 || cards[0].Profile != "headset-head-unit" || cards[0].Profiles["a2dp-sink"] != 1 || cards[0].Default {
		t.Errorf("ParseCards of several arrays = %+v, %v", cards, err)
	}
	for _, bad := range []string{"not json", "", dump + " x"} {
		if _, err := ParseCards([]byte(bad), nil); err == nil {
			t.Errorf("bad dump %q accepted", bad)
		}
	}
	if _, err := ParseCards([]byte("not json"), nil); err == nil {
		t.Error("bad dump accepted")
	}
}

// TestSwitcherRoute: in mode ofono the headset that answers takes the call,
// even when a Bluetooth speaker is the default output.
func TestSwitcherRoute(t *testing.T) {
	const jblAddr = "84:d3:52:8c:b9:d0"
	jbl, jabra := card(CardName(jblAddr), "a2dp-sink"), card("bluez_card.50_C2_75_0D_4E_03", "a2dp-sink")
	env := &fakeEnv{cards: []Card{jbl, jabra}, playing: []string{"player"}}
	s := &Switcher{Env: env}

	// Ringing: pause the music, switch nothing yet.
	s.Call(true, Route{Wait: true})
	if want := []string{"pause player"}; !reflect.DeepEqual(env.log, want) {
		t.Fatalf("ringing: %q, want %q", env.log, want)
	}
	// Answered on the JBL.
	env.log = nil
	s.Call(true, Route{Device: jblAddr})
	s.Call(true, Route{Device: jblAddr})
	if want := []string{"profile bluez_card.84_D3_52_8C_B9_D0 headset-head-unit"}; !reflect.DeepEqual(env.log, want) {
		t.Fatalf("answered: %q, want %q", env.log, want)
	}
	env.log = nil
	s.Call(false, Route{})
	if want := []string{"profile bluez_card.84_D3_52_8C_B9_D0 a2dp-sink", "play player"}; !reflect.DeepEqual(env.log, want) {
		t.Fatalf("end: %q, want %q", env.log, want)
	}
}

// TestSwitcherRouteChange: a call taken in the softphone switches both; when
// a headset then takes it, the other one goes back.
func TestSwitcherRouteChange(t *testing.T) {
	jbl, jabra := card("bluez_card.84_D3_52_8C_B9_D0", "a2dp-sink"), card("bluez_card.50_C2_75_0D_4E_03", "a2dp-sink")
	env := &fakeEnv{cards: []Card{jbl, jabra}}
	s := &Switcher{Env: env}
	s.Call(true, Route{})
	if len(env.log) != 2 {
		t.Fatalf("no headset known: %q", env.log)
	}
	env.log = nil
	s.Call(true, Route{Device: "50:C2:75:0D:4E:03"})
	if want := []string{"profile bluez_card.84_D3_52_8C_B9_D0 a2dp-sink"}; !reflect.DeepEqual(env.log, want) {
		t.Fatalf("route change: %q, want %q", env.log, want)
	}
	env.log = nil
	s.Call(false, Route{})
	if want := []string{"profile bluez_card.50_C2_75_0D_4E_03 a2dp-sink"}; !reflect.DeepEqual(env.log, want) {
		t.Fatalf("end: %q, want %q", env.log, want)
	}
	// A device that is not connected falls back to the usual choice.
	if got := forCall([]Card{jbl}, "11:22:33:44:55:66"); len(got) != 1 || got[0].Name != jbl.Name {
		t.Fatalf("forCall with unknown device = %v", got)
	}
}
