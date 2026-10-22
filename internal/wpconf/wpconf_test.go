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
		`target.object = "bluez_output.bt-headset-hid.1"`,
		`target.object = "bluez_input.bt-headset-hid.0"`,
		`node.description = "bt-headset-hid"`,
		`media.class = "Audio/Sink"`,
		`media.class = "Audio/Source"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in\n%s", want, b)
		}
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
	lua, err := HeadsetContent([]string{jabra, jbl, jabra}, Version{0, 4}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`{ "device.name", "equals", "bluez_card.50_C2_75_0D_4E_03" }`,
		`{ "device.name", "equals", "bluez_card.AC_BF_71_12_34_56" }`,
		`["device.profile"] = "headset-head-unit"`,
		`{ { "node.name", "equals", "bluez_output.AC_BF_71_12_34_56.1" }, { "factory.name", "equals", "api.bluez5.sco.sink" } }`,
		`{ { "node.name", "equals", "bluez_input.50_C2_75_0D_4E_03.0" }, { "factory.name", "equals", "api.bluez5.sco.source" } }`,
		`["node.name"] = "bluez_output.bt-headset-hid.1"`,
		`["node.name"] = "bluez_input.bt-headset-hid.0"`,
	} {
		if !strings.Contains(string(lua), want) {
			t.Errorf("missing %s in Lua rules\n%s", want, lua)
		}
	}
	if n := strings.Count(string(lua), "headset-head-unit"); n != 2 {
		t.Errorf("%d profile rules, want 2 (duplicate address)", n)
	}
	conf, err := HeadsetContent([]string{jabra, jbl}, Version{0, 5}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`device.name = "bluez_card.50_C2_75_0D_4E_03"`,
		`device.name = "bluez_card.AC_BF_71_12_34_56"`,
		`device.profile = "headset-head-unit"`,
		`{ node.name = "bluez_output.AC_BF_71_12_34_56.1", factory.name = "api.bluez5.sco.sink" }`,
		`update-props = { node.name = "bluez_input.bt-headset-hid.0" }`,
	} {
		if !strings.Contains(string(conf), want) {
			t.Errorf("missing %s in conf rules\n%s", want, conf)
		}
	}
	for _, v := range []Version{{0, 4}, {0, 5}} {
		sw, err := HeadsetContent([]string{jabra}, v, false)
		if err != nil || strings.Contains(string(sw), "device.profile") || strings.Contains(string(sw), PhoneMarker) ||
			!strings.Contains(string(sw), `"bluez_output.bt-headset-hid.1"`) || !strings.Contains(string(sw), `"bluez_input.50_C2_75_0D_4E_03.0"`) {
			t.Errorf("switching rules %d.%d: %v\n%s", v.Major, v.Minor, err, sw)
		}
	}
	for _, bad := range [][]string{nil, {""}, {"50:C2:75:0D:4E"}, {jabra, `50:C2:75:0D:4E:0" }`}, {"50_C2_75_0D_4E_03"}} {
		if _, err := HeadsetContent(bad, Version{0, 5}, false); err == nil {
			t.Errorf("HeadsetContent(%q) accepted", bad)
		}
	}

	dir := t.TempDir()
	if got := ConfiguredHeadsets(dir); got != nil {
		t.Errorf("ConfiguredHeadsets without rules = %q", got)
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

	p4, err := InstallHeadset(dir, []string{jabra, jbl}, Version{0, 4}, true)
	if err != nil || !strings.HasSuffix(p4, filepath.Join("bluetooth.lua.d", "52-bt-headset-hid-headset.lua")) {
		t.Fatalf("InstallHeadset 0.4 = %s, %v", p4, err)
	}
	if got := ConfiguredHeadsets(dir); strings.Join(got, ",") != "50:C2:75:0D:4E:03,AC:BF:71:12:34:56" {
		t.Errorf("ConfiguredHeadsets 0.4 = %q", got)
	}
	if !ConfiguredPhone(dir) {
		t.Error("ConfiguredPhone = false after phone rules")
	}
	noAuto := filepath.Join(dir, "wireplumber", "policy.lua.d", "52-bt-headset-hid-session.lua")
	if _, err := os.Stat(noAuto); !os.IsNotExist(err) {
		t.Errorf("autoswitch policy with phone rules: %v", err)
	}
	if _, err := InstallHeadset(dir, []string{jabra, jbl}, Version{0, 4}, false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(noAuto); err != nil || !strings.Contains(string(data), `bluetooth_policy.policy["media-role.use-headset-profile"] = false`) {
		t.Errorf("autoswitch policy for 0.4 switching: %q, %v", data, err)
	}
	p5, err := InstallHeadset(dir, []string{jbl}, Version{0, 5}, false)
	if err != nil {
		t.Fatal(err)
	}
	if ConfiguredPhone(dir) {
		t.Error("ConfiguredPhone = true after switching rules")
	}
	if _, err := os.Stat(noAuto); !os.IsNotExist(err) {
		t.Errorf("0.4 autoswitch policy left with 0.5: %v", err)
	}
	if data, _ := os.ReadFile(p5); !strings.Contains(string(data), "bluetooth.autoswitch-to-headset-profile = false") {
		t.Errorf("0.5 switching rules without autoswitch setting:\n%s", data)
	}
	if _, err := os.Stat(p4); !os.IsNotExist(err) {
		t.Errorf("Lua rule still there after the switch to 0.5: %v", err)
	}
	if got := ConfiguredHeadsets(dir); strings.Join(got, ",") != "AC:BF:71:12:34:56" {
		t.Errorf("ConfiguredHeadsets 0.5 = %q", got)
	}
	if removed, err := RemoveHeadset(dir); err != nil || len(removed) != 1 || removed[0] != p5 {
		t.Fatalf("RemoveHeadset = %v, %v", removed, err)
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
