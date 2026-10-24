package tray

import (
	"reflect"
	"strings"
	"testing"

	"github.com/crazmoe/bt-headset-hid/internal/doctor"
)

func labels(menu []Item) []string {
	var out []string
	for _, it := range menu {
		if it.Separator {
			out = append(out, "--")
			continue
		}
		l := it.Label
		switch it.Check {
		case Checked:
			l = "[x] " + l
		case Unchecked:
			l = "[ ] " + l
		}
		out = append(out, l)
	}
	return out
}

func TestShow(t *testing.T) {
	hs := []Headset{
		{Address: "AA:AA:AA:AA:AA:AA", Name: "Jabra", Connected: true, Selected: true},
		{Address: "BB:BB:BB:BB:BB:BB", Name: "", Selected: false},
	}
	v := Show(State{Version: "0.6.0", Mode: "ofono", Headsets: hs})
	if v.Icon != "audio-headset-symbolic" || v.Attention || v.Tooltip != "Jabra connected" {
		t.Errorf("view = %+v", v)
	}
	want := []string{
		"bt-headset-hid 0.6.0 · mode ofono", "--",
		"Headsets for calls:", "[x] Jabra (connected)", "[ ] BB:BB:BB:BB:BB:BB", "--",
		"Setup is fine", "Check now", "Help",
	}
	if got := labels(v.Menu); !reflect.DeepEqual(got, want) {
		t.Errorf("menu =\n%q\nwant\n%q", got, want)
	}
	for i, it := range v.Menu {
		if it.ID != int32(i+1) {
			t.Errorf("item %d has ID %d", i, it.ID)
		}
	}
	it, ok := Find(v.Menu, 5)
	if !ok || it.Action != ToggleHeadset || it.Arg != "BB:BB:BB:BB:BB:BB" || !it.Enabled {
		t.Errorf("Find(5) = %+v, %v", it, ok)
	}
	if _, ok := Find(v.Menu, 99); ok {
		t.Error("Find(99) found an item")
	}
}

func TestShowStates(t *testing.T) {
	hs := []Headset{{Address: "AA:AA:AA:AA:AA:AA", Name: "Jabra", Connected: true}}
	cases := []struct {
		name      string
		s         State
		icon, tip string
	}{
		{"not selected", State{Mode: "ofono", Headsets: hs}, "audio-headset-symbolic", "no headset connected"},
		{"hfp uses all", State{Mode: "hfp", Headsets: hs}, "audio-headset-symbolic", "Jabra connected"},
		{"call", State{Mode: "ofono", InCall: true}, "call-start-symbolic", "Call"},
		{"german", State{Mode: "ofono", German: true}, "audio-headset-symbolic", "kein Headset verbunden"},
		{"problem", State{Mode: "ofono", InCall: true, Problems: []doctor.Finding{{Level: doctor.Fail, Text: "Bluetooth is switched off"}}},
			"dialog-warning-symbolic", "Bluetooth is switched off"},
		{"problems", State{Mode: "ofono", Problems: make([]doctor.Finding, 2)}, "dialog-warning-symbolic", "2 problems, see the menu"},
	}
	for _, c := range cases {
		v := Show(c.s)
		if v.Icon != c.icon || v.Tooltip != c.tip || v.Attention != (len(c.s.Problems) > 0) {
			t.Errorf("%s: icon %q tooltip %q attention %v", c.name, v.Icon, v.Tooltip, v.Attention)
		}
	}
}

func TestShowHFPAndProblems(t *testing.T) {
	v := Show(State{Version: "0.6.0", Mode: "hfp", German: true,
		Headsets: []Headset{{Address: "AA:AA:AA:AA:AA:AA", Name: "Jabra", Connected: true}},
		Problems: []doctor.Finding{{Level: doctor.Fail, Text: "Dienst läuft nicht", Fix: "sudo systemctl restart bt-headset-hid"}},
	})
	want := []string{
		"bt-headset-hid 0.6.0 · Modus hfp", "--",
		"Headsets:", "Jabra (verbunden)", "--",
		"⚠ Dienst läuft nicht", "Jetzt prüfen", "Hilfe",
	}
	if got := labels(v.Menu); !reflect.DeepEqual(got, want) {
		t.Errorf("menu =\n%q\nwant\n%q", got, want)
	}
	if v.Menu[3].Enabled {
		t.Error("headsets in mode hfp can be clicked")
	}
	if it := v.Menu[5]; it.Action != ShowProblem || it.Arg != "0" {
		t.Errorf("problem item = %+v", it)
	}
	v = Show(State{Mode: "ofono"})
	if got := strings.Join(labels(v.Menu), "|"); !strings.Contains(got, "No headset paired") {
		t.Errorf("menu without headsets = %s", got)
	}
}

func TestToggle(t *testing.T) {
	a, b := "AA:AA:AA:AA:AA:AA", "BB:BB:BB:BB:BB:BB"
	if got := Toggle([]string{a, b}, strings.ToLower(a)); !reflect.DeepEqual(got, []string{b}) {
		t.Errorf("remove: %v", got)
	}
	if got := Toggle([]string{a}, b); !reflect.DeepEqual(got, []string{a, b}) {
		t.Errorf("add: %v", got)
	}
	if got := Toggle(nil, a); !reflect.DeepEqual(got, []string{a}) {
		t.Errorf("add to none: %v", got)
	}
}

func TestHeadsets(t *testing.T) {
	paired := []Headset{{Address: "AA:AA:AA:AA:AA:AA"}, {Address: "BB:BB:BB:BB:BB:BB"}}
	got := Headsets(paired, []string{"bb:bb:bb:bb:bb:bb"})
	if got[0].Selected || !got[1].Selected || paired[1].Selected {
		t.Errorf("Headsets = %+v, paired = %+v", got, paired)
	}
}

func TestFailures(t *testing.T) {
	fs := []doctor.Finding{{Level: doctor.OK, Text: "a"}, {Level: doctor.Fail, Text: "b"}, {Level: doctor.Warn, Text: "c"}}
	if got := Failures(fs); len(got) != 1 || got[0].Text != "b" {
		t.Errorf("Failures = %+v", got)
	}
}

func TestShowBattery(t *testing.T) {
	hs := []Headset{
		{Address: "AA:AA:AA:AA:AA:AA", Name: "Jabra", Connected: true, Selected: true, Battery: 80, BatteryKnown: true},
		{Address: "BB:BB:BB:BB:BB:BB", Name: "JBL", Connected: true, Selected: true},
		{Address: "CC:CC:CC:CC:CC:CC", Name: "Old", Battery: 50, BatteryKnown: true},
	}
	v := Show(State{Version: "1", Mode: "ofono", Headsets: hs, German: true})
	if v.Tooltip != "Jabra (80 %), JBL verbunden" {
		t.Errorf("tooltip = %q", v.Tooltip)
	}
	got := labels(v.Menu)
	want := []string{"[x] Jabra (verbunden, Akku 80 %)", "[x] JBL (verbunden)", "[ ] Old"}
	if !reflect.DeepEqual(got[3:6], want) {
		t.Errorf("menu = %q", got)
	}
}

func TestLowBatteries(t *testing.T) {
	h := func(addr string, connected bool, level int) Headset {
		return Headset{Address: addr, Connected: connected, Battery: level, BatteryKnown: level >= 0}
	}
	var warned map[string]bool
	steps := []struct {
		name string
		hs   []Headset
		want []string
	}{
		{"fine", []Headset{h("A", true, 50), h("B", true, -1)}, nil},
		{"A low", []Headset{h("A", true, 15), h("B", false, 5)}, []string{"A"}},
		{"A still low", []Headset{h("A", true, 10)}, nil},
		{"A a bit better", []Headset{h("A", true, 25)}, nil},
		{"A low again", []Headset{h("A", true, 18)}, nil},
		{"A charged", []Headset{h("A", true, 30)}, nil},
		{"A low after charging", []Headset{h("A", true, 19)}, []string{"A"}},
		{"A disconnected", []Headset{h("A", false, 19)}, nil},
		{"A back", []Headset{h("A", true, 19)}, []string{"A"}},
	}
	for _, s := range steps {
		var low []Headset
		low, warned = LowBatteries(s.hs, warned)
		var got []string
		for _, l := range low {
			got = append(got, l.Address)
		}
		if !reflect.DeepEqual(got, s.want) {
			t.Errorf("%s: warned about %v, want %v", s.name, got, s.want)
		}
	}
}
