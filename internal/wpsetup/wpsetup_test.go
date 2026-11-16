package wpsetup

import (
	"bytes"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/install"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
)

var (
	jabra = bluez.Headset{Address: "50:C2:75:0D:4E:03", Name: "Jabra", Connected: true, Battery: -1}
	jbl   = bluez.Headset{Address: "AC:BF:71:12:34:56", Name: "JBL", Battery: -1}
	car   = bluez.Headset{Address: "00:11:22:33:44:55", Name: "Car", Battery: -1}
)

// world is a fake Env: it records the commands run.
type world struct {
	Env
	out  bytes.Buffer
	ran  []string
	fail map[string]bool // command lines that fail
}

func newWorld(t *testing.T, paired ...bluez.Headset) *world {
	w := &world{fail: map[string]bool{}}
	w.Env = Env{
		ConfigDir: t.TempDir(),
		Out:       &w.out,
		In:        strings.NewReader(""),
		Run: func(name string, args ...string) ([]byte, error) {
			line := strings.Join(append([]string{name}, args...), " ")
			w.ran = append(w.ran, line)
			if w.fail[line] {
				return []byte("failed"), errors.New("exit status 1")
			}
			if line == "wireplumber --version" {
				return []byte("wireplumber\nCompiled with libwireplumber 0.5.6\nLinked with libwireplumber 0.5.6\n"), nil
			}
			return nil, nil
		},
		Paired:        func() ([]bluez.Headset, error) { return paired, nil },
		SessionBinary: install.PackagedBinary,
		Self:          install.PackagedBinary,
	}
	return w
}

func (w *world) ranAll(t *testing.T, want ...string) {
	t.Helper()
	for _, c := range want {
		if !slices.Contains(w.ran, c) {
			t.Errorf("did not run %q; ran %q", c, w.ran)
		}
	}
}

func TestSetupOFono(t *testing.T) {
	w := newWorld(t, jabra)
	session, err := Setup(Options{Mode: "ofono"}, w.Env)
	if err != nil || !session {
		t.Fatalf("Setup = %v, %v\n%s", session, err, w.out.String())
	}
	dir := w.ConfigDir
	if m := wpconf.ConfiguredMode(dir); m != "ofono" {
		t.Errorf("mode = %q", m)
	}
	if got := wpconf.ConfiguredHeadsets(dir); !reflect.DeepEqual(got, []string{jabra.Address}) {
		t.Errorf("headsets = %v", got)
	}
	if wpconf.ConfiguredPhone(dir) {
		t.Error("phone profile without -profile phone")
	}
	if _, err := os.Stat(wpconf.DevicesPath(dir)); err != nil {
		t.Errorf("devices: %v", err)
	}
	if _, err := os.Stat(wpconf.SessionUnitPath(dir)); err != nil {
		t.Errorf("session unit: %v", err)
	}
	w.ranAll(t,
		"systemctl --user restart pipewire wireplumber",
		"systemctl --user try-restart pipewire-pulse",
		"systemctl --user daemon-reload",
		"systemctl --user enable "+wpconf.SessionUnit,
		"systemctl --user restart "+wpconf.SessionUnit,
	)
	if !strings.Contains(w.out.String(), "now restart the daemon") {
		t.Errorf("new mode without the hint to restart the daemon:\n%s", w.out.String())
	}

	// Again: nothing changed, so PipeWire is not restarted, and the mode is
	// kept.
	w.ran, w.out = nil, bytes.Buffer{}
	if _, err := Setup(Options{}, w.Env); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(w.ran, "systemctl --user restart pipewire wireplumber") {
		t.Error("PipeWire restarted without a change")
	}
	if strings.Contains(w.out.String(), "now restart the daemon") {
		t.Error("asked to restart the daemon without a new mode")
	}
	if m := wpconf.ConfiguredMode(dir); m != "ofono" {
		t.Errorf("mode after a second run = %q", m)
	}
}

func TestSetupPackaged(t *testing.T) {
	w := newWorld(t, jabra)
	w.Packaged = true
	// A copy of the session unit from an older version hides the packaged
	// one.
	if _, err := wpconf.InstallSessionUnit(w.ConfigDir, install.Binary); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(Options{Mode: "pipewire", Profile: "phone", NoRestart: true}, w.Env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wpconf.SessionUnitPath(w.ConfigDir)); err == nil {
		t.Error("old session unit kept beside the packaged one")
	}
	if !wpconf.ConfiguredPhone(w.ConfigDir) {
		t.Error("-profile phone not kept")
	}
	if slices.Contains(w.ran, "systemctl --user restart pipewire wireplumber") || slices.Contains(w.ran, "systemctl --user restart "+wpconf.SessionUnit) {
		t.Errorf("restarted with -no-restart: %q", w.ran)
	}
	if !strings.Contains(w.out.String(), "systemctl --user restart pipewire wireplumber") {
		t.Errorf("no hint to restart PipeWire:\n%s", w.out.String())
	}
}

func TestSetupHeadsets(t *testing.T) {
	w := newWorld(t, jabra, jbl, car)
	dir := w.ConfigDir
	if _, err := Setup(Options{Mode: "ofono", Headset: "50:C2:75:0D:4E:03, AC:BF:71:12:34:56"}, w.Env); err != nil {
		t.Fatal(err)
	}
	if got := wpconf.ConfiguredHeadsets(dir); len(got) != 2 {
		t.Fatalf("headsets = %v", got)
	}
	// A shorter list drops the other headset, which says so.
	w.out.Reset()
	if _, err := Setup(Options{Headset: jabra.Address}, w.Env); err != nil {
		t.Fatal(err)
	}
	if got := wpconf.ConfiguredHeadsets(dir); !reflect.DeepEqual(got, []string{jabra.Address}) {
		t.Errorf("headsets = %v", got)
	}
	if !strings.Contains(w.out.String(), "no longer set up: "+jbl.Address) {
		t.Errorf("dropped headset not named:\n%s", w.out.String())
	}
	// Without -headset the list stays; all paired ones are known, so none
	// is added.
	if _, err := Setup(Options{}, w.Env); err != nil {
		t.Fatal(err)
	}
	if got := wpconf.ConfiguredHeadsets(dir); !reflect.DeepEqual(got, []string{jabra.Address}) {
		t.Errorf("headsets after a run without -headset = %v", got)
	}
}

func TestSetupChoosesHeadsets(t *testing.T) {
	w := newWorld(t, car, jabra, jbl)
	w.Interactive = true
	w.In = strings.NewReader("1,3\n")
	if _, err := Setup(Options{Mode: "pipewire"}, w.Env); err != nil {
		t.Fatal(err)
	}
	if got := wpconf.ConfiguredHeadsets(w.ConfigDir); !reflect.DeepEqual(got, []string{car.Address, jbl.Address}) {
		t.Errorf("headsets = %v", got)
	}
}

func TestSetupWithoutBlueZ(t *testing.T) {
	w := newWorld(t)
	w.Paired = func() ([]bluez.Headset, error) { return nil, errors.New("no bus") }
	if _, err := Setup(Options{Mode: "ofono"}, w.Env); err != nil {
		t.Fatal(err)
	}
	out := w.out.String()
	if !strings.Contains(out, "could not ask BlueZ") || !strings.Contains(out, "no headset paired yet") {
		t.Errorf("output:\n%s", out)
	}
}

func TestSetupHFP(t *testing.T) {
	w := newWorld(t, jabra)
	if _, err := Setup(Options{Mode: "ofono"}, w.Env); err != nil {
		t.Fatal(err)
	}
	// hfp has no call audio: the headsets, devices and session service go.
	session, err := Setup(Options{Mode: "hfp"}, w.Env)
	if err != nil || session {
		t.Fatalf("Setup hfp = %v, %v", session, err)
	}
	dir := w.ConfigDir
	if got := wpconf.ConfiguredHeadsets(dir); got != nil {
		t.Errorf("headsets in hfp = %v", got)
	}
	for _, p := range []string{wpconf.DevicesPath(dir), wpconf.KnownPath(dir), wpconf.SessionUnitPath(dir)} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s left in mode hfp", p)
		}
	}
	w.ranAll(t, "systemctl --user disable --now "+wpconf.SessionUnit)
}

func TestSetupRemove(t *testing.T) {
	w := newWorld(t, jabra)
	if _, err := Setup(Options{Mode: "ofono"}, w.Env); err != nil {
		t.Fatal(err)
	}
	w.out.Reset()
	if _, err := Setup(Options{Remove: true, Profile: "ignored"}, w.Env); err != nil {
		t.Fatal(err)
	}
	dir := w.ConfigDir
	if m := wpconf.ConfiguredMode(dir); m != "" {
		t.Errorf("mode after remove = %q", m)
	}
	if !wpconf.Off(dir) {
		t.Error("remove did not keep the session service from setting it up again")
	}
	if !strings.Contains(w.out.String(), "removed ") {
		t.Errorf("output:\n%s", w.out.String())
	}
	// Removing again finds nothing.
	w.out.Reset()
	if _, err := Setup(Options{Remove: true}, w.Env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.out.String(), "nothing to remove") {
		t.Errorf("second remove:\n%s", w.out.String())
	}
	// uninstall removes it so that installing again sets it up again.
	if _, err := Setup(Options{Remove: true, Uninstall: true}, w.Env); err != nil {
		t.Fatal(err)
	}
	if wpconf.Off(dir) {
		t.Error("uninstall left it switched off")
	}
	// Setting it up again switches it on.
	if _, err := Setup(Options{Remove: true}, w.Env); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(Options{Mode: "ofono"}, w.Env); err != nil {
		t.Fatal(err)
	}
	if wpconf.Off(dir) {
		t.Error("still off after setting it up again")
	}
}

func TestSetupFromSession(t *testing.T) {
	w := newWorld(t, jabra)
	session, err := Setup(Options{Mode: "ofono", FromSession: true}, w.Env)
	if err != nil || !session {
		t.Fatalf("Setup = %v, %v", session, err)
	}
	for _, c := range w.ran {
		if strings.Contains(c, wpconf.SessionUnit) {
			t.Errorf("the session service touched itself: %q", c)
		}
	}
	if strings.Contains(w.out.String(), "now restart the daemon") {
		t.Error("session service asked to restart the daemon")
	}
}

func TestSetupErrors(t *testing.T) {
	w := newWorld(t, jabra)
	if _, err := Setup(Options{Profile: "music"}, w.Env); err == nil {
		t.Error("-profile music accepted")
	}
	if wpconf.ConfiguredMode(w.ConfigDir) != "" {
		t.Error("configuration written despite a wrong -profile")
	}

	w = newWorld(t, jabra)
	w.fail["wireplumber --version"] = true
	if _, err := Setup(Options{Mode: "ofono"}, w.Env); err == nil || !strings.Contains(err.Error(), "wireplumber --version") {
		t.Errorf("without wireplumber: %v", err)
	}

	w = newWorld(t, jabra)
	w.fail["systemctl --user restart pipewire wireplumber"] = true
	if _, err := Setup(Options{Mode: "ofono"}, w.Env); err == nil || !strings.Contains(err.Error(), "restart") {
		t.Errorf("failed restart: %v", err)
	}

	w = newWorld(t, jabra)
	w.fail["systemctl --user enable "+wpconf.SessionUnit] = true
	if _, err := Setup(Options{Mode: "ofono"}, w.Env); err == nil || !strings.Contains(err.Error(), "enable") {
		t.Errorf("failed enable: %v", err)
	}
}

func TestSessionUnitNote(t *testing.T) {
	w := newWorld(t, jabra)
	w.SessionBinary, w.Self = install.Binary, "/home/me/bt-headset-hid"
	if _, err := Setup(Options{Mode: "ofono"}, w.Env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.out.String(), "NOTE: it runs "+install.Binary) {
		t.Errorf("no note about the other program:\n%s", w.out.String())
	}
}

func TestSessionBinary(t *testing.T) {
	none := func(string) bool { return false }
	only := func(p string) func(string) bool { return func(q string) bool { return p == q } }
	for _, c := range []struct {
		exists func(string) bool
		self   string
		want   string
	}{
		{only(install.Binary), "/tmp/x", install.Binary},
		{only(install.PackagedBinary), "/tmp/x", install.PackagedBinary},
		{none, "/tmp/x", "/tmp/x"},
		{none, "", install.Binary},
	} {
		if got := SessionBinary(c.exists, c.self); got != c.want {
			t.Errorf("SessionBinary(self %q) = %q, want %q", c.self, got, c.want)
		}
	}
}

func TestDroppedHeadsets(t *testing.T) {
	before := []string{"50:C2:75:0D:4E:03", "84:D3:52:8C:B9:D0"}
	got := DroppedHeadsets(before, []string{"84:d3:52:8c:b9:d0"})
	if len(got) != 1 || got[0] != "50:C2:75:0D:4E:03" {
		t.Errorf("DroppedHeadsets = %q", got)
	}
	if got := DroppedHeadsets(before, []string{"84:D3:52:8C:B9:D0", "50:c2:75:0d:4e:03"}); got != nil {
		t.Errorf("DroppedHeadsets with both = %q", got)
	}
}

func TestChooseHeadsets(t *testing.T) {
	all := []bluez.Headset{car, jabra, jbl}
	names := func(hs []bluez.Headset) string {
		var n []string
		for _, h := range hs {
			n = append(n, h.Name)
		}
		return strings.Join(n, ",")
	}
	for _, c := range []struct {
		hs          []bluez.Headset
		in          string
		interactive bool
		want        string
	}{
		{nil, "", true, ""},
		{[]bluez.Headset{jbl}, "", true, "JBL"},
		{all, "", false, "Jabra"},
		{[]bluez.Headset{car, jbl}, "", false, "Car,JBL"},
		{all, "\n", true, "Jabra"},
		{all, "", true, "Jabra"},
		{all, "all\n", true, "Car,Jabra,JBL"},
		{all, "3, 1\n", true, "JBL,Car"},
		{all, "4\nx\n2 2\n", true, "Jabra"},
		{all, "4", true, "Jabra"},
	} {
		if got := names(ChooseHeadsets(c.hs, strings.NewReader(c.in), c.interactive, io.Discard)); got != c.want {
			t.Errorf("ChooseHeadsets(%s, %q, %v) = %s, want %s", names(c.hs), c.in, c.interactive, got, c.want)
		}
	}
}
