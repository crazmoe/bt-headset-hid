package wpconf

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	for out, want := range map[string]Version{
		"wireplumber\nCompiled with libwireplumber 0.4.17\nLinked with libwireplumber 0.4.17\n": {0, 4},
		"wireplumber\nCompiled with libwireplumber 0.5.6\nLinked with libwireplumber 0.5.7\n":   {0, 5},
		"Compiled with libwireplumber 0.4.9\nLinked with libwireplumber 0.5.0":                  {0, 5},
	} {
		v, err := ParseVersion(out)
		if err != nil || v != want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", out, v, err, want)
		}
	}
	if _, err := ParseVersion("command not found"); err == nil {
		t.Error("no error for missing version")
	}
	if !(Version{0, 4}).Lua() || (Version{0, 5}).Lua() || (Version{1, 0}).Lua() {
		t.Error("Lua() wrong")
	}
}

func TestContent(t *testing.T) {
	for _, c := range []struct {
		mode string
		v    Version
		want string
	}{
		{"hfp", Version{0, 4}, `bluez5.headset-roles`},
		{"pipewire", Version{0, 4}, `["bluez5.hfphsp-backend-native-modem"] = "any"`},
		{"hfp", Version{0, 5}, `bluez5.roles = [ a2dp_sink a2dp_source ]`},
		{"pipewire", Version{0, 5}, `bluez5.hfphsp-backend-native-modem = "any"`},
		{"ofono", Version{0, 4}, `bluez_monitor.properties["bluez5.hfphsp-backend"] = "ofono"`},
		{"ofono", Version{0, 5}, `bluez5.hfphsp-backend = "ofono"`},
	} {
		b, err := Content(c.mode, c.v)
		if err != nil || !strings.Contains(string(b), c.want) {
			t.Errorf("Content(%s, %v) = %q, %v", c.mode, c.v, b, err)
		}
	}
	if _, err := Content("other", Version{0, 5}); err == nil {
		t.Error("unknown mode accepted")
	}
}

// The embedded files are copies of contrib/wireplumber, which the README
// points to for manual installation.
func TestFilesMatchContrib(t *testing.T) {
	entries, err := files.ReadDir("files")
	if err != nil {
		t.Fatal(err)
	}
	contrib, err := os.ReadDir("../../contrib/wireplumber")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(contrib) {
		t.Fatalf("%d embedded files, %d in contrib/wireplumber", len(entries), len(contrib))
	}
	for _, e := range entries {
		a, _ := files.ReadFile("files/" + e.Name())
		b, err := os.ReadFile(filepath.Join("../../contrib/wireplumber", e.Name()))
		if err != nil || !bytes.Equal(a, b) {
			t.Errorf("%s differs from contrib/wireplumber (%v)", e.Name(), err)
		}
	}
}

func TestInstallSwitchesFormat(t *testing.T) {
	dir := t.TempDir()
	lua, err := Install(dir, "hfp", Version{0, 4})
	if err != nil || !strings.HasSuffix(lua, filepath.Join("bluetooth.lua.d", "51-bt-headset-hid.lua")) {
		t.Fatalf("Install 0.4: %s, %v", lua, err)
	}
	// After an upgrade to 0.5 the Lua file must go away.
	conf, err := Install(dir, "pipewire", Version{0, 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lua); !os.IsNotExist(err) {
		t.Errorf("Lua file still there: %v", err)
	}
	b, _ := os.ReadFile(conf)
	if !strings.Contains(string(b), "hfphsp-backend-native-modem") {
		t.Errorf("conf content %q", b)
	}
	removed, err := Remove(dir)
	if err != nil || len(removed) != 1 || removed[0] != conf {
		t.Fatalf("Remove = %v, %v", removed, err)
	}
	if removed, err := Remove(dir); err != nil || len(removed) != 0 {
		t.Fatalf("second Remove = %v, %v", removed, err)
	}
}

func TestDevices(t *testing.T) {
	b := DevicesContent()
	for _, want := range []string{
		`node.name = "bt-headset-hid.speaker.out"`,
		`node.name = "bt-headset-hid.microphone.in"`,
		`node.description = "bt-headset-hid"`,
		`media.class = "Audio/Sink"`,
		`media.class = "Audio/Source"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in\n%s", want, b)
		}
	}
	// The session service sets the target; a fixed one would win over
	// the default devices while no headset is there.
	if strings.Contains(string(b), "target.object") {
		t.Errorf("fixed target in\n%s", b)
	}

	dir := t.TempDir()
	p, err := InstallDevices(dir)
	if err != nil || p != filepath.Join(dir, "pipewire", "pipewire.conf.d", "51-bt-headset-hid.conf") {
		t.Fatalf("InstallDevices = %s, %v", p, err)
	}
	if ok, err := RemoveDevices(dir); !ok || err != nil {
		t.Fatalf("RemoveDevices = %v, %v", ok, err)
	}
	if ok, err := RemoveDevices(dir); ok || err != nil {
		t.Fatalf("second RemoveDevices = %v, %v", ok, err)
	}
}

func TestHeadset(t *testing.T) {
	jabra, jbl := "50:c2:75:0d:4e:03", "AC:BF:71:12:34:56"
	data, err := HeadsetsContent([]string{jabra, jbl, jabra}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "# "+PhoneMarker+"\n50:C2:75:0D:4E:03\nAC:BF:71:12:34:56\n") {
		t.Errorf("list:\n%s", data)
	}
	for _, bad := range [][]string{{""}, {"50:C2:75:0D:4E"}, {jabra, "50:C2:75:0D:4E:0\nX"}, {"50_C2_75_0D_4E_03"}} {
		if _, err := HeadsetsContent(bad, false); err == nil {
			t.Errorf("HeadsetsContent(%q) accepted", bad)
		}
	}

	dir := t.TempDir()
	if got := ConfiguredHeadsets(dir); got != nil {
		t.Errorf("ConfiguredHeadsets without a list = %q", got)
	}
	// Fixed devices from before carry the one headset's address.
	old := DevicesPath(dir)
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte(`target.object = "bluez_output.50_C2_75_0D_4E_03.1"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ConfiguredHeadsets(dir); len(got) != 1 || got[0] != "50:C2:75:0D:4E:03" {
		t.Errorf("ConfiguredHeadsets from old devices = %q", got)
	}
	// Rules for every headset from before, with the phone marker.
	lua, conf := HeadsetPaths(dir, Version{0, 4})
	oldRules := "-- " + PhoneMarker + "\n" + `{ "node.name", "equals", "bluez_output.AC_BF_71_12_34_56.1" }` + "\n"
	if err := os.MkdirAll(filepath.Dir(lua), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lua, []byte(oldRules), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ConfiguredHeadsets(dir); strings.Join(got, ",") != "AC:BF:71:12:34:56" || !ConfiguredPhone(dir) || !OldRules(dir) {
		t.Errorf("from old rules: %q, phone %v, old rules %v", got, ConfiguredPhone(dir), OldRules(dir))
	}

	noAuto := filepath.Join(dir, "wireplumber", "policy.lua.d", "52-bt-headset-hid-session.lua")
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	// 0.4, switching: the policy file, no headset file.
	p, err := InstallHeadset(dir, []string{jabra, jbl}, Version{0, 4}, false)
	if err != nil || p != HeadsetsPath(dir) {
		t.Fatalf("InstallHeadset 0.4 = %s, %v", p, err)
	}
	if got := ConfiguredHeadsets(dir); strings.Join(got, ",") != "50:C2:75:0D:4E:03,AC:BF:71:12:34:56" || ConfiguredPhone(dir) || OldRules(dir) {
		t.Errorf("0.4: %q, phone %v, old rules %v", got, ConfiguredPhone(dir), OldRules(dir))
	}
	if data, err := os.ReadFile(noAuto); err != nil || !strings.Contains(string(data), `bluetooth_policy.policy["media-role.use-headset-profile"] = false`) {
		t.Errorf("autoswitch policy for 0.4: %q, %v", data, err)
	}
	if exists(lua) || exists(conf) {
		t.Error("headset file with 0.4")
	}

	// 0.5, switching: the setting in the headset file, no policy file.
	if _, err := InstallHeadset(dir, []string{jbl}, Version{0, 5}, false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(conf); !strings.Contains(string(data), "bluetooth.autoswitch-to-headset-profile = false") {
		t.Errorf("0.5 headset file:\n%s", data)
	}
	if exists(noAuto) {
		t.Error("0.4 policy file with 0.5")
	}

	// Phone: neither, and the marker in the list.
	if _, err := InstallHeadset(dir, nil, Version{0, 5}, true); err != nil {
		t.Fatal(err)
	}
	if exists(conf) || exists(noAuto) || !ConfiguredPhone(dir) || ConfiguredHeadsets(dir) != nil {
		t.Errorf("phone: files %v %v, phone %v, headsets %q", exists(conf), exists(noAuto), ConfiguredPhone(dir), ConfiguredHeadsets(dir))
	}

	// The list alone.
	if err := SetHeadsets(dir, []string{jabra}, true); err != nil {
		t.Fatal(err)
	}
	if got := ConfiguredHeadsets(dir); strings.Join(got, ",") != "50:C2:75:0D:4E:03" || !ConfiguredPhone(dir) {
		t.Errorf("SetHeadsets: %q, phone %v", got, ConfiguredPhone(dir))
	}
	if removed, err := RemoveHeadset(dir); err != nil || len(removed) != 1 || removed[0] != HeadsetsPath(dir) {
		t.Fatalf("RemoveHeadset = %v, %v", removed, err)
	}
}

func TestSnapshot(t *testing.T) {
	dir := t.TempDir()
	before := Snapshot(dir)
	if err := SetHeadsets(dir, []string{"50:C2:75:0D:4E:03"}, false); err != nil {
		t.Fatal(err)
	}
	if Snapshot(dir) != before {
		t.Error("the headset list changed the snapshot")
	}
	if _, err := InstallDevices(dir); err != nil {
		t.Fatal(err)
	}
	after := Snapshot(dir)
	if after == before {
		t.Error("new devices did not change the snapshot")
	}
	if _, err := InstallDevices(dir); err != nil || Snapshot(dir) != after {
		t.Errorf("the same devices again changed the snapshot: %v", err)
	}
}

func TestSessionUnit(t *testing.T) {
	dir := t.TempDir()
	p, err := InstallSessionUnit(dir, "/usr/local/bin/bt-headset-hid")
	if err != nil || p != filepath.Join(dir, "systemd", "user", "bt-headset-hid-session.service") {
		t.Fatalf("InstallSessionUnit = %s, %v", p, err)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "\nExecStart=/usr/local/bin/bt-headset-hid session\n") {
		t.Errorf("unit:\n%s", data)
	}
	if ok, err := RemoveSessionUnit(dir); !ok || err != nil {
		t.Fatalf("RemoveSessionUnit = %v, %v", ok, err)
	}
	if ok, err := RemoveSessionUnit(dir); ok || err != nil {
		t.Fatalf("second RemoveSessionUnit = %v, %v", ok, err)
	}
}

func TestConfiguredMode(t *testing.T) {
	dir := t.TempDir()
	if m := ConfiguredMode(dir); m != "" {
		t.Fatalf("ConfiguredMode without files = %q", m)
	}
	for _, c := range []struct {
		mode string
		v    Version
	}{{"ofono", Version{0, 4}}, {"hfp", Version{0, 5}}, {"pipewire", Version{0, 4}}, {"ofono", Version{0, 5}}} {
		if _, err := Install(dir, c.mode, c.v); err != nil {
			t.Fatal(err)
		}
		if m := ConfiguredMode(dir); m != c.mode {
			t.Errorf("after Install(%s, %v): ConfiguredMode = %q", c.mode, c.v, m)
		}
	}
}

func TestKnown(t *testing.T) {
	dir := t.TempDir()
	if _, ok := Known(dir); ok {
		t.Fatal("Known without a list = true")
	}
	if err := SetKnown(dir, []string{"ac:bf:71:12:34:56", "50:C2:75:0D:4E:03", "AC:BF:71:12:34:56", "junk"}); err != nil {
		t.Fatal(err)
	}
	got, ok := Known(dir)
	if !ok || strings.Join(got, ",") != "50:C2:75:0D:4E:03,AC:BF:71:12:34:56" {
		t.Errorf("Known = %q, %v", got, ok)
	}
	if err := SetKnown(dir, nil); err != nil {
		t.Fatal(err)
	}
	if got, ok := Known(dir); !ok || got != nil {
		t.Errorf("Known of an empty list = %q, %v", got, ok)
	}
	if ok, err := RemoveKnown(dir); !ok || err != nil {
		t.Fatalf("RemoveKnown = %v, %v", ok, err)
	}
	if ok, err := RemoveKnown(dir); ok || err != nil {
		t.Fatalf("second RemoveKnown = %v, %v", ok, err)
	}
}

func TestNewHeadsets(t *testing.T) {
	const jabra, jbl, car = "50:C2:75:0D:4E:03", "AC:BF:71:12:34:56", "00:11:22:33:44:55"
	paired := []string{jabra, strings.ToLower(jbl), car}
	if got := NewHeadsets(paired, []string{jabra}, nil, false); got != nil {
		t.Errorf("without a known list: %q", got)
	}
	if got := NewHeadsets(paired, []string{jabra}, []string{car}, true); strings.Join(got, ",") != jbl {
		t.Errorf("NewHeadsets = %q, want %s", got, jbl)
	}
	if got := NewHeadsets(paired, nil, []string{jabra, jbl, car}, true); got != nil {
		t.Errorf("all known: %q", got)
	}
}

func TestOff(t *testing.T) {
	dir := t.TempDir()
	if Off(dir) {
		t.Fatal("off without the file")
	}
	if err := SetOff(dir, true); err != nil || !Off(dir) {
		t.Fatalf("SetOff(true): %v, Off %v", err, Off(dir))
	}
	if err := SetOff(dir, false); err != nil || Off(dir) {
		t.Fatalf("SetOff(false): %v, Off %v", err, Off(dir))
	}
	if err := SetOff(dir, false); err != nil {
		t.Fatalf("SetOff(false) twice: %v", err)
	}
}
