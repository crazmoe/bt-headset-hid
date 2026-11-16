package sysfacts

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/dbustest"
	"github.com/crazmoe/bt-headset-hid/internal/doctor"
	"github.com/crazmoe/bt-headset-hid/internal/install"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
)

// commands fakes Output and CombinedOutput: the key is the command line.
type commands map[string]string

func (c commands) run(name string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	out, ok := c[line]
	if !ok {
		return nil, errors.New("not found: " + line)
	}
	return []byte(out), nil
}

func write(t *testing.T, path, data string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), perm); err != nil {
		t.Fatal(err)
	}
}

type fakeObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

func (f fakeObjects) GetManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, *dbus.Error) {
	return f, nil
}

// fakeBlueZ starts a bus with org.bluez exporting objs and returns its
// address.
func fakeBlueZ(t *testing.T, objs fakeObjects) string {
	addr := dbustest.Bus(t)
	conn := dbustest.Connect(t, addr)
	if err := conn.Export(objs, "/", "org.freedesktop.DBus.ObjectManager"); err != nil {
		t.Fatal(err)
	}
	if r, err := conn.RequestName("org.bluez", 0); err != nil || r != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("RequestName: %v %v", r, err)
	}
	return addr
}

func adapter(powered bool) map[string]map[string]dbus.Variant {
	return map[string]map[string]dbus.Variant{"org.bluez.Adapter1": {"Powered": dbus.MakeVariant(powered)}}
}

func TestUnitState(t *testing.T) {
	s := System{Output: commands{
		"systemctl is-active bluetooth.service":       "active\n",
		"systemctl is-active ofono.service":           "unknown\n",
		"systemctl --user is-active pipewire.service": "failed\n",
	}.run}
	for _, c := range []struct {
		user bool
		unit string
		want doctor.State
	}{
		{false, "bluetooth.service", doctor.Active},
		{false, "ofono.service", doctor.Inactive},
		{true, "pipewire.service", doctor.State("failed")},
		{false, "missing.service", ""},
	} {
		if got := s.UnitState(c.user, c.unit); got != c.want {
			t.Errorf("UnitState(%v, %s) = %q, want %q", c.user, c.unit, got, c.want)
		}
	}
}

func TestFactsNotInstalled(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bin")
	write(t, filepath.Join(path, "bt-headset-hid"), "#!/bin/sh\n", 0o755)
	s := System{
		Root:   root,
		Euid:   1000,
		Getenv: func(k string) string { return map[string]string{"PATH": path}[k] },
		Output: func(string, ...string) ([]byte, error) {
			t.Error("Facts ran a command without the service installed")
			return nil, errors.New("no")
		},
	}
	f := s.Facts()
	if f.Mode != "" || f.Root || f.Packaged || f.Adapter != -1 || f.OFonoBackend != -1 {
		t.Errorf("Facts = %+v", f)
	}
	if len(f.Programs) != 1 {
		t.Errorf("Programs = %v", f.Programs)
	}
}

func TestFacts(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, install.Unit), string(install.UnitFile(install.PackagedBinary, "ofono")), 0o644)
	write(t, filepath.Join(root, install.PackagedBinary), "", 0o755)
	write(t, filepath.Join(root, install.UdevRule), "rule\n", 0o644)
	write(t, filepath.Join(root, install.DBusPolicy), "<busconfig/>\n", 0o644)
	write(t, filepath.Join(root, "/dev/uhid"), "", 0o600)
	write(t, filepath.Join(root, "/sys/class/hidraw/hidraw0/device/uevent"), "HID_NAME=Keyboard\n", 0o644)
	write(t, filepath.Join(root, "/sys/class/hidraw/hidraw3/device/uevent"), "HID_ID=0005:0000:0000\nHID_NAME=bt-headset-hid\n", 0o644)
	write(t, filepath.Join(root, "/dev/hidraw3"), "", 0o666)
	write(t, filepath.Join(root, "/usr/lib/x86_64-linux-gnu/spa-0.2/bluez5/libspa-bluez5.so"), "...HandsfreeAudioManager...", 0o644)
	config := t.TempDir()
	if err := wpconf.SetHeadsets(config, []string{"50:C2:75:0D:4E:03"}, false); err != nil {
		t.Fatal(err)
	}

	bus := fakeBlueZ(t, fakeObjects{
		"/org/bluez/hci0": adapter(true),
		"/org/bluez/hci0/dev_50_C2_75_0D_4E_03": {"org.bluez.Device1": {
			"Address": dbus.MakeVariant("50:C2:75:0D:4E:03"), "Alias": dbus.MakeVariant("Jabra"),
			"Paired": dbus.MakeVariant(true), "Connected": dbus.MakeVariant(true),
			"UUIDs": dbus.MakeVariant([]string{"0000111e-0000-1000-8000-00805f9b34fb"}),
		}},
	})
	cmds := commands{
		"systemctl is-active bt-headset-hid.service":                          "active\n",
		"systemctl is-active bluetooth.service":                               "active\n",
		"systemctl is-active ModemManager.service":                            "inactive\n",
		"systemctl is-active ofono.service":                                   "unknown\n",
		"systemctl is-active hsphfpd.service":                                 "inactive\n",
		"systemctl --user is-active pipewire.service":                         "active\n",
		"systemctl --user is-active wireplumber.service":                      "active\n",
		"systemctl --user is-active pulseaudio.service":                       "inactive\n",
		"systemctl --user is-active " + wpconf.SessionUnit:                    "active\n",
		"journalctl -u bt-headset-hid.service -b -n 300 -o cat --no-pager -q": "old\nStarted bt-headset-hid.service\nmode ofono\n",
		"pipewire --version":                                                  "pipewire\nCompiled with libpipewire 1.2.7\n",
	}
	s := System{
		Root:           root,
		Euid:           1000,
		Getenv:         func(k string) string { return map[string]string{"PATH": filepath.Join(root, "/usr/bin")}[k] },
		Output:         cmds.run,
		CombinedOutput: cmds.run,
		SystemBus:      func() (*dbus.Conn, error) { return dbus.Connect(bus) },
		ConfigDir:      func() (string, error) { return config, nil },
	}
	f := s.Facts()
	want := doctor.Facts{
		Mode:            "ofono",
		Service:         doctor.Active,
		UnitProgram:     install.PackagedBinary,
		Packaged:        true,
		Programs:        []string{filepath.Join(root, install.PackagedBinary)},
		Log:             []string{"mode ofono"},
		LogReadable:     true,
		UHID:            true,
		UdevRule:        true,
		DBusPolicy:      []byte("<busconfig/>\n"),
		HIDDevice:       true,
		HIDAccess:       true,
		Bluetooth:       doctor.Active,
		Adapter:         2,
		Paired:          []doctor.Headset{{Address: "50:C2:75:0D:4E:03", Name: "Jabra", Connected: true}},
		ModemManager:    doctor.Inactive,
		OFono:           doctor.Inactive,
		Hsphfpd:         doctor.Inactive,
		PipeWireVersion: "pipewire\nCompiled with libpipewire 1.2.7\n",
		PipeWire:        doctor.Active,
		WirePlumber:     doctor.Active,
		PulseAudio:      doctor.Inactive,
		OFonoBackend:    1,
		WPMode:          wpconf.ConfiguredMode(config),
		Configured:      []string{"50:C2:75:0D:4E:03"},
		Session:         doctor.Active,
	}
	// The temporary directory may be behind a symlink.
	if len(f.Programs) == 1 {
		if r, err := filepath.EvalSymlinks(want.Programs[0]); err == nil {
			want.Programs[0] = r
		}
	}
	if !reflect.DeepEqual(f, want) {
		t.Errorf("Facts =\n%+v\nwant\n%+v", f, want)
	}

	// As root, the user's session is not looked at.
	s.Euid = 0
	s.ConfigDir = func() (string, error) {
		t.Error("ConfigDir asked as root")
		return "", errors.New("no")
	}
	if f := s.Facts(); !f.Root || f.PipeWire != "" || f.Configured != nil {
		t.Errorf("Facts as root = %+v", f)
	}

	// Without the system bus.
	s.SystemBus = func() (*dbus.Conn, error) { return nil, errors.New("no bus") }
	if f := s.Facts(); f.BlueZErr == nil || f.Adapter != -1 {
		t.Errorf("Facts without bus: BlueZErr %v, Adapter %d", f.BlueZErr, f.Adapter)
	}
}

func TestAdapterState(t *testing.T) {
	for _, c := range []struct {
		objs fakeObjects
		want int
	}{
		{fakeObjects{}, 0},
		{fakeObjects{"/org/bluez/hci0": adapter(false)}, 1},
		{fakeObjects{"/org/bluez/hci0": adapter(false), "/org/bluez/hci1": adapter(true)}, 2},
	} {
		conn := dbustest.Connect(t, fakeBlueZ(t, c.objs))
		if got := AdapterState(conn); got != c.want {
			t.Errorf("AdapterState(%v) = %d, want %d", c.objs, got, c.want)
		}
	}
	// Without BlueZ on the bus.
	if got := AdapterState(dbustest.Connect(t, dbustest.Bus(t))); got != -1 {
		t.Errorf("AdapterState without BlueZ = %d", got)
	}
}

func TestOFonoBackend(t *testing.T) {
	root := t.TempDir()
	s := System{Root: root}
	if got := s.OFonoBackend(); got != -1 {
		t.Errorf("without plugin: %d", got)
	}
	plugin := filepath.Join(root, "/usr/lib64/spa-0.2/bluez5/libspa-bluez5.so")
	write(t, plugin, "native backend only", 0o644)
	if got := s.OFonoBackend(); got != 0 {
		t.Errorf("without oFono backend: %d", got)
	}
	write(t, plugin, "...HandsfreeAudioManager...", 0o644)
	if got := s.OFonoBackend(); got != 1 {
		t.Errorf("with oFono backend: %d", got)
	}
}

func TestPipeWireVersion(t *testing.T) {
	s := System{CombinedOutput: commands{}.run}
	if v := s.PipeWireVersion(); v != "" {
		t.Errorf("without pipewire: %q", v)
	}
}

func TestPrograms(t *testing.T) {
	dir := t.TempDir()
	local, usr, bin, empty := filepath.Join(dir, "local"), filepath.Join(dir, "usr"), filepath.Join(dir, "bin"), filepath.Join(dir, "empty")
	for _, d := range []string{local, usr, empty} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// bin is a symlink to usr, as /bin to /usr/bin.
	if err := os.Symlink(usr, bin); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{local, usr} {
		write(t, filepath.Join(d, "bt-headset-hid"), "#!/bin/sh\n", 0o755)
	}
	// Not executable: the shell skips it.
	write(t, filepath.Join(empty, "bt-headset-hid"), "", 0o644)
	usrReal, _ := filepath.EvalSymlinks(usr)
	localReal, _ := filepath.EvalSymlinks(local)
	got := Programs(strings.Join([]string{empty, local, usr, bin}, ":"))
	want := []string{filepath.Join(localReal, "bt-headset-hid"), filepath.Join(usrReal, "bt-headset-hid")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Programs = %v, want %v", got, want)
	}
}
