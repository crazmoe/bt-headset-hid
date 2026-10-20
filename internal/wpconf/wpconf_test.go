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
