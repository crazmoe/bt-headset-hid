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
	b, err := DevicesContent("50:c2:75:0d:4e:03")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`target.object = "bluez_output.50_C2_75_0D_4E_03.1"`,
		`target.object = "bluez_input.50_C2_75_0D_4E_03.0"`,
		`node.description = "bt-headset-hid"`,
		`media.class = "Audio/Sink"`,
		`media.class = "Audio/Source"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in\n%s", want, b)
		}
	}
	for _, bad := range []string{"", "50:C2:75:0D:4E", `50:C2:75:0D:4E:0" ]`, "50_C2_75_0D_4E_03"} {
		if _, err := DevicesContent(bad); err == nil {
			t.Errorf("DevicesContent(%q) accepted", bad)
		}
	}

	dir := t.TempDir()
	p, err := InstallDevices(dir, "50:C2:75:0D:4E:03")
	if err != nil || p != filepath.Join(dir, "pipewire", "pipewire.conf.d", "51-bt-headset-hid.conf") {
		t.Fatalf("InstallDevices = %s, %v", p, err)
	}
	if got := ConfiguredHeadset(dir); got != "50:C2:75:0D:4E:03" {
		t.Errorf("ConfiguredHeadset = %q", got)
	}
	if ok, err := RemoveDevices(dir); !ok || err != nil {
		t.Fatalf("RemoveDevices = %v, %v", ok, err)
	}
	if got := ConfiguredHeadset(dir); got != "" {
		t.Errorf("ConfiguredHeadset without devices = %q", got)
	}
	if ok, err := RemoveDevices(dir); ok || err != nil {
		t.Fatalf("second RemoveDevices = %v, %v", ok, err)
	}
}

func TestHeadset(t *testing.T) {
	lua, err := HeadsetContent("50:c2:75:0d:4e:03", Version{0, 4})
	if err != nil || !strings.Contains(string(lua), `{ "device.name", "equals", "bluez_card.50_C2_75_0D_4E_03" }`) ||
		!strings.Contains(string(lua), `["device.profile"] = "headset-head-unit"`) {
		t.Errorf("Lua rule %q, %v", lua, err)
	}
	conf, err := HeadsetContent("50:C2:75:0D:4E:03", Version{0, 5})
	if err != nil || !strings.Contains(string(conf), `device.name = "bluez_card.50_C2_75_0D_4E_03"`) ||
		!strings.Contains(string(conf), `device.profile = "headset-head-unit"`) {
		t.Errorf("conf rule %q, %v", conf, err)
	}
	if _, err := HeadsetContent(`50:C2:75:0D:4E:0" }`, Version{0, 5}); err == nil {
		t.Error("bad address accepted")
	}

	dir := t.TempDir()
	p4, err := InstallHeadset(dir, "50:C2:75:0D:4E:03", Version{0, 4})
	if err != nil || !strings.HasSuffix(p4, filepath.Join("bluetooth.lua.d", "52-bt-headset-hid-headset.lua")) {
		t.Fatalf("InstallHeadset 0.4 = %s, %v", p4, err)
	}
	p5, err := InstallHeadset(dir, "50:C2:75:0D:4E:03", Version{0, 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p4); !os.IsNotExist(err) {
		t.Errorf("Lua rule still there after the switch to 0.5: %v", err)
	}
	if removed, err := RemoveHeadset(dir); err != nil || len(removed) != 1 || removed[0] != p5 {
		t.Fatalf("RemoveHeadset = %v, %v", removed, err)
	}
}
