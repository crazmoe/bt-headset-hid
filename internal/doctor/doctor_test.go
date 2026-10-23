package doctor

import (
	"strings"
	"testing"

	"github.com/crazmoe/bt-headset-hid/internal/install"
)

// healthy is a working installation in mode ofono.
func healthy() Facts {
	return Facts{
		Mode:         "ofono",
		Service:      Active,
		LogReadable:  true,
		Log:          []string{"HFP/HSP Audio Gateway registered with BlueZ, serving org.ofono for PipeWire", "audio agent :1.42/MediaEndpoint/ofono registered, codecs [1 2]"},
		UHID:         true,
		UdevRule:     true,
		DBusPolicy:   install.Policy("ofono"),
		HIDDevice:    true,
		HIDAccess:    true,
		Bluetooth:    Active,
		Adapter:      2,
		Paired:       []Headset{{Address: "50:C2:75:0D:4E:03", Name: "Jabra", Connected: true}},
		ModemManager: Active,
		OFono:        Inactive,
		Hsphfpd:      Inactive,
		PipeWire:     Active,
		WirePlumber:  Active,
		PulseAudio:   Inactive,
		OFonoBackend: 1,
		WPMode:       "ofono",
		Configured:   []string{"50:C2:75:0D:4E:03"},
		Session:      Active,
	}
}

func TestHealthy(t *testing.T) {
	for _, de := range []bool{false, true} {
		for _, f := range Check(healthy(), de) {
			if f.Level != OK {
				t.Errorf("healthy (de %v): %+v", de, f)
			}
		}
	}
}

func TestProblems(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*Facts)
		fix    string // a failure with this fix must be reported
	}{
		{"not installed", func(f *Facts) { *f = Facts{} }, "sudo bt-headset-hid install"},
		{"service down", func(f *Facts) { f.Service = "failed" }, "sudo systemctl enable --now bt-headset-hid"},
		{"no uhid", func(f *Facts) { f.UHID = false }, "sudo modprobe uhid"},
		{"policy of pipewire", func(f *Facts) { f.DBusPolicy = install.Policy("pipewire") }, "sudo bt-headset-hid install -mode ofono"},
		{"bluetooth off", func(f *Facts) { f.Adapter = 1 }, "bluetoothctl power on"},
		{"nothing paired", func(f *Facts) { f.Paired = nil }, "pair it in the desktop's Bluetooth settings"},
		{"ofono running", func(f *Facts) { f.OFono = Active }, "sudo systemctl disable --now ofono"},
		{"wireplumber other mode", func(f *Facts) { f.WPMode = "pipewire" }, "bt-headset-hid wireplumber -mode ofono"},
		{"wireplumber not set up", func(f *Facts) { f.WPMode = "" }, "bt-headset-hid wireplumber -mode ofono"},
		{"pipewire without ofono", func(f *Facts) { f.OFonoBackend = 0 }, "sudo bt-headset-hid install -mode hfp"},
		{"session down", func(f *Facts) { f.Session = "failed" }, "bt-headset-hid wireplumber"},
		{"pulseaudio", func(f *Facts) { f.PulseAudio = Active }, "install PipeWire with pipewire-pulse"},
		{"no hid device", func(f *Facts) { f.HIDDevice = false }, "journalctl -u bt-headset-hid -b"},
		{"pipewire keeps HFP", func(f *Facts) {
			f.Log = []string{"bluez: another program (usually PipeWire) already provides the HFP/HSP Audio Gateway; ..."}
		}, "bt-headset-hid wireplumber && sudo systemctl restart bt-headset-hid"},
		{"modemmanager in mode pipewire", func(f *Facts) {
			f.Mode, f.WPMode, f.DBusPolicy = "pipewire", "pipewire", install.Policy("pipewire")
		}, "sudo bt-headset-hid install -mode pipewire"},
		{"old pipewire", func(f *Facts) {
			f.Mode, f.WPMode, f.DBusPolicy, f.ModemManager = "pipewire", "pipewire", install.Policy("pipewire"), Inactive
			f.PipeWireVersion = "pipewire\nCompiled with libpipewire 1.0.5\nLinked with libpipewire 1.0.5\n"
		}, "sudo bt-headset-hid install -mode ofono"},
	} {
		f := healthy()
		c.change(&f)
		got := Check(f, false)
		found := false
		for _, g := range got {
			if g.Level == Fail && g.Fix == c.fix {
				found = true
			}
		}
		if !found || !Failed(got) {
			t.Errorf("%s: no failure with fix %q in %+v", c.name, c.fix, got)
		}
	}
}

func TestWarnings(t *testing.T) {
	f := healthy()
	f.Log = f.Log[:1]
	if got := Check(f, false); !hasLevel(got, Warn, "no audio agent") {
		t.Errorf("no audio agent in mode ofono: %+v", got)
	}
	f = healthy()
	f.Paired[0].Connected = false
	if got := Check(f, true); !hasLevel(got, Warn, "kein Headset verbunden") {
		t.Errorf("not connected: %+v", got)
	}
	f = healthy()
	f.Mode, f.WPMode, f.DBusPolicy, f.Configured = "hfp", "hfp", nil, nil
	for _, g := range Check(f, false) {
		if g.Level != OK {
			t.Errorf("healthy hfp: %+v", g)
		}
	}
	f = healthy()
	f.Root = true
	if got := Check(f, false); !hasLevel(got, Info, "Started as root") {
		t.Errorf("root: %+v", got)
	}
}

func hasLevel(fs []Finding, l Level, text string) bool {
	for _, f := range fs {
		if f.Level == l && strings.Contains(f.Text, text) {
			return true
		}
	}
	return false
}

func TestModeFromUnit(t *testing.T) {
	for unit, want := range map[string]string{
		string(install.UnitFile("/usr/bin/bt-headset-hid", "pipewire")): "pipewire",
		string(install.UnitFile("/usr/bin/bt-headset-hid", "hfp")):      "hfp",
		"[Service]\nExecStart=/usr/bin/bt-headset-hid -mode=ofono\n":    "ofono",
		"[Service]\nExecStart=/usr/bin/bt-headset-hid\n":                "ofono",
		"[Unit]\n": "",
	} {
		if got := ModeFromUnit([]byte(unit)); got != want {
			t.Errorf("ModeFromUnit(%q) = %q, want %q", unit, got, want)
		}
	}
}

func TestHIDNameAndLastStart(t *testing.T) {
	uevent := []byte("DRIVER=hid-generic\nHID_ID=0005:00001209:00000001\nHID_NAME=bt-headset-hid\n")
	if !HIDName(uevent, "bt-headset-hid") || HIDName(uevent, "bt-headset") {
		t.Error("HIDName wrong")
	}
	lines := []string{"old error", "Started bt-headset-hid.service - Bluetooth headset buttons", "new"}
	if got := LastStart(lines); len(got) != 1 || got[0] != "new" {
		t.Errorf("LastStart = %q", got)
	}
	if got := LastStart(lines[:1]); len(got) != 1 {
		t.Errorf("LastStart without start = %q", got)
	}
}

func TestNoLongerPaired(t *testing.T) {
	f := healthy()
	f.Configured = append(f.Configured, "84:D3:52:8C:B9:D0")
	want := "bt-headset-hid wireplumber -headset 50:C2:75:0D:4E:03"
	for _, g := range Check(f, false) {
		if g.Level == Info && strings.Contains(g.Text, "84:D3:52:8C:B9:D0") {
			if g.Fix != want {
				t.Errorf("fix = %q, want %q", g.Fix, want)
			}
			return
		}
	}
	t.Error("no finding for the headset no longer paired")
}
