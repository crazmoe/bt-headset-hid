package install

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeRunner struct {
	runs   [][]string
	asUser [][]string
}

func (f *fakeRunner) Run(argv []string) error { f.runs = append(f.runs, argv); return nil }
func (f *fakeRunner) RunAs(user string, uid int, argv []string) error {
	f.asUser = append(f.asUser, append([]string{user, string(rune('0' + uid))}, argv...))
	return nil
}

func TestFilesMatchContrib(t *testing.T) {
	for name, contrib := range map[string]string{
		"70-bt-headset-hid.rules":   "../../contrib/udev/70-bt-headset-hid.rules",
		"bt-headset-hid.conf":       "../../contrib/dbus/bt-headset-hid.conf",
		"bt-headset-hid-ofono.conf": "../../contrib/dbus/bt-headset-hid-ofono.conf",
	} {
		b, err := os.ReadFile(contrib)
		if err != nil || !bytes.Equal(b, asset(name)) {
			t.Errorf("%s differs from %s (%v)", name, contrib, err)
		}
	}
	b, err := os.ReadFile("../../contrib/systemd/bt-headset-hid.service")
	if err != nil || !bytes.Equal(b, UnitFile(Binary, "ofono")) {
		t.Errorf("contrib unit differs from UnitFile(ofono) (%v)", err)
	}
}

func TestPlanRejectsMode(t *testing.T) {
	if _, err := Plan(Options{Mode: "x"}); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func apply(t *testing.T, steps []Step, o Options, root string) *fakeRunner {
	t.Helper()
	r := &fakeRunner{}
	if err := Apply(steps, o, root, r, func(string) {}); err != nil {
		t.Fatal(err)
	}
	return r
}

func exists(root, p string) bool {
	_, err := os.Stat(filepath.Join(root, p))
	return err == nil
}

func TestInstallSwitchUninstall(t *testing.T) {
	root := t.TempDir()
	self := filepath.Join(root, "build", "bt-headset-hid")
	os.MkdirAll(filepath.Dir(self), 0o755)
	os.WriteFile(self, []byte("binary"), 0o755)
	o := Options{Mode: "pipewire", Self: self, User: "moe", UID: 5}

	steps, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	r := apply(t, steps, o, root)
	for _, p := range []string{Binary, Unit, UdevRule, DBusPolicy, ModulesCfg} {
		if !exists(root, p) {
			t.Errorf("%s not installed", p)
		}
	}
	if fi, err := os.Stat(filepath.Join(root, Binary)); err != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("binary mode %v, %v", fi.Mode(), err)
	}
	unit, _ := os.ReadFile(filepath.Join(root, Unit))
	if !strings.Contains(string(unit), "ExecStart="+Binary+" -mode pipewire\n") {
		t.Errorf("unit:\n%s", unit)
	}
	if !reflect.DeepEqual(r.asUser, [][]string{{"moe", "5", Binary, "wireplumber", "-mode", "pipewire"}}) {
		t.Errorf("as user %v", r.asUser)
	}
	// The service is restarted last, after WirePlumber was reconfigured.
	if last := r.runs[len(r.runs)-1]; !reflect.DeepEqual(last, []string{"systemctl", "restart", Service}) {
		t.Errorf("last command %v", last)
	}
	if !containsRun(r.runs, "systemctl", "disable", "--now", "ModemManager.service") {
		t.Errorf("ModemManager not disabled: %v", r.runs)
	}

	// Switching to ofono lets the daemon own org.ofono instead and gives
	// ModemManager back.
	o.Mode = "ofono"
	steps, _ = Plan(o)
	r = apply(t, steps, o, root)
	unit, _ = os.ReadFile(filepath.Join(root, Unit))
	policy, _ := os.ReadFile(filepath.Join(root, DBusPolicy))
	if !strings.Contains(string(unit), "-mode ofono\n") || !strings.Contains(string(policy), `own="org.ofono"`) ||
		strings.Contains(string(policy), "ModemManager1") {
		t.Errorf("ofono: policy:\n%s\nunit:\n%s", policy, unit)
	}
	if !containsRun(r.runs, "systemctl", "enable", "--now", "ModemManager.service") ||
		containsRun(r.runs, "systemctl", "disable", "--now", "ModemManager.service") {
		t.Errorf("ofono mode does not give ModemManager back: %v", r.runs)
	}
	if !reflect.DeepEqual(r.asUser, [][]string{{"moe", "5", Binary, "wireplumber", "-mode", "ofono"}}) {
		t.Errorf("as user %v", r.asUser)
	}

	// Switching to hfp rewrites the unit and drops the D-Bus policy.
	o.Mode = "hfp"
	steps, _ = Plan(o)
	r = apply(t, steps, o, root)
	unit, _ = os.ReadFile(filepath.Join(root, Unit))
	if !strings.Contains(string(unit), "-mode hfp\n") || exists(root, DBusPolicy) {
		t.Errorf("after switch: policy %v, unit:\n%s", exists(root, DBusPolicy), unit)
	}
	if containsRun(r.runs, "systemctl", "disable", "--now", "ModemManager.service") {
		t.Error("hfp mode touches ModemManager")
	}

	r = apply(t, PlanUninstall(o), o, root)
	for _, p := range []string{Binary, Unit, UdevRule, DBusPolicy, ModulesCfg} {
		if exists(root, p) {
			t.Errorf("%s left after uninstall", p)
		}
	}
	if len(r.asUser) != 1 || r.asUser[0][3] != "wireplumber" || r.asUser[0][4] != "-remove" {
		t.Errorf("uninstall as user %v", r.asUser)
	}
}

func TestInstallFromBinaryPath(t *testing.T) {
	// Running the installed binary again must not copy it onto itself.
	steps, _ := Plan(Options{Mode: "hfp", Self: Binary})
	for _, s := range steps {
		if s.Copy != nil {
			t.Fatal("binary copied onto itself")
		}
	}
	// Without a desktop user, WirePlumber is left alone.
	for _, s := range steps {
		if s.AsUser != nil {
			t.Fatal("AsUser step without user")
		}
	}
}

func TestInstallFromPackage(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, filepath.Dir(Binary)), 0o755)
	os.WriteFile(filepath.Join(root, Binary), []byte("old copy"), 0o755)
	o := Options{Mode: "pipewire", Self: PackagedBinary, User: "moe", UID: 5}
	steps, _ := Plan(o)
	r := apply(t, steps, o, root)
	if exists(root, Binary) {
		t.Error("old copy in /usr/local/bin left")
	}
	unit, _ := os.ReadFile(filepath.Join(root, Unit))
	if !strings.Contains(string(unit), "ExecStart="+PackagedBinary+" -mode pipewire\n") {
		t.Errorf("unit:\n%s", unit)
	}
	if r.asUser[0][2] != PackagedBinary {
		t.Errorf("as user %v", r.asUser)
	}
	for _, s := range PlanUninstall(o) {
		if s.Remove == PackagedBinary {
			t.Error("uninstall removes the packaged binary")
		}
	}
}

func containsRun(runs [][]string, argv ...string) bool {
	for _, r := range runs {
		if reflect.DeepEqual(r, argv) {
			return true
		}
	}
	return false
}

func TestSudoUser(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if u, uid, ok := SudoUser(env(map[string]string{"SUDO_USER": "moe", "SUDO_UID": "1000"})); !ok || u != "moe" || uid != 1000 {
		t.Errorf("SudoUser = %q %d %v", u, uid, ok)
	}
	for _, m := range []map[string]string{{}, {"SUDO_USER": "root", "SUDO_UID": "0"}, {"SUDO_USER": "moe"}} {
		if _, _, ok := SudoUser(env(m)); ok {
			t.Errorf("SudoUser(%v) ok", m)
		}
	}
}
