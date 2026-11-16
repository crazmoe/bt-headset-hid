// Package sysfacts gathers the facts that package doctor judges: services,
// files, devices, BlueZ and PipeWire. It does the I/O that doctor leaves
// out; every source can be replaced, so it can be tested.
package sysfacts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/doctor"
	"github.com/crazmoe/bt-headset-hid/internal/install"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
)

// System is where the facts come from.
type System struct {
	// Root is put in front of every file path; "" for the real system.
	Root string
	Euid int
	// Getenv reads the environment (PATH).
	Getenv func(string) string
	// Output returns the standard output of a command, CombinedOutput also
	// its errors.
	Output, CombinedOutput func(name string, args ...string) ([]byte, error)
	SystemBus              func() (*dbus.Conn, error)
	// ConfigDir is the user's configuration directory (~/.config).
	ConfigDir func() (string, error)
}

// Real is the system this program runs on.
func Real() System {
	return System{
		Euid:   os.Geteuid(),
		Getenv: os.Getenv,
		Output: func(name string, args ...string) ([]byte, error) {
			return exec.Command(name, args...).Output()
		},
		CombinedOutput: func(name string, args ...string) ([]byte, error) {
			return exec.Command(name, args...).CombinedOutput()
		},
		SystemBus: func() (*dbus.Conn, error) { return dbus.ConnectSystemBus() },
		ConfigDir: os.UserConfigDir,
	}
}

func (s System) path(p string) string { return filepath.Join(s.Root, p) }

func (s System) exists(p string) bool {
	_, err := os.Stat(s.path(p))
	return err == nil
}

// UnitState asks systemctl for the state of unit; user selects the user's
// units. Units that do not exist are "inactive"; "" means systemctl could
// not tell.
func (s System) UnitState(user bool, unit string) doctor.State {
	args := []string{"is-active", unit}
	if user {
		args = append([]string{"--user"}, args...)
	}
	out, err := s.Output("systemctl", args...)
	st := strings.TrimSpace(string(out))
	if st == "" && err != nil {
		return ""
	}
	if st == "unknown" {
		return doctor.Inactive
	}
	return doctor.State(st)
}

// PipeWireVersion is the output of "pipewire --version", or "".
func (s System) PipeWireVersion() string {
	out, err := s.CombinedOutput("pipewire", "--version")
	if err != nil {
		return ""
	}
	return string(out)
}

// Facts gathers what "bt-headset-hid doctor" checks.
func (s System) Facts() doctor.Facts {
	f := doctor.Facts{Root: s.Euid == 0, Adapter: -1, OFonoBackend: -1}
	if unit, err := os.ReadFile(s.path(install.Unit)); err == nil {
		f.Mode = doctor.ModeFromUnit(unit)
		if p := doctor.ProgramFromUnit(unit); p != "" {
			f.UnitProgram = resolve(p)
		}
	}
	f.Packaged = s.exists(install.PackagedBinary)
	f.Programs = Programs(s.Getenv("PATH"))
	if f.Mode == "" {
		return f
	}
	f.Service = s.UnitState(false, install.Service)
	if out, err := s.Output("journalctl", "-u", install.Service, "-b", "-n", "300", "-o", "cat", "--no-pager", "-q"); err == nil && len(out) > 0 {
		f.LogReadable = true
		f.Log = doctor.LastStart(strings.Split(strings.TrimSpace(string(out)), "\n"))
	}
	f.UHID = s.exists("/dev/uhid")
	f.UdevRule = s.exists(install.UdevRule)
	f.DBusPolicy, _ = os.ReadFile(s.path(install.DBusPolicy))
	f.HIDDevice, f.HIDAccess = s.hidDevice("bt-headset-hid")

	f.Bluetooth = s.UnitState(false, "bluetooth.service")
	f.ModemManager = s.UnitState(false, "ModemManager.service")
	f.OFono = s.UnitState(false, "ofono.service")
	f.Hsphfpd = s.UnitState(false, "hsphfpd.service")
	if conn, err := s.SystemBus(); err != nil {
		f.BlueZErr = err
	} else {
		defer conn.Close()
		f.Adapter = AdapterState(conn)
		hs, err := bluez.Headsets(conn)
		f.BlueZErr = err
		for _, h := range hs {
			f.Paired = append(f.Paired, doctor.Headset{Address: h.Address, Name: h.Name, Connected: h.Connected})
		}
	}
	f.PipeWireVersion = s.PipeWireVersion()
	f.OFonoBackend = s.OFonoBackend()
	if f.Root {
		return f
	}
	f.PipeWire = s.UnitState(true, "pipewire.service")
	f.WirePlumber = s.UnitState(true, "wireplumber.service")
	f.PulseAudio = s.UnitState(true, "pulseaudio.service")
	f.Session = s.UnitState(true, wpconf.SessionUnit)
	if dir, err := s.ConfigDir(); err == nil {
		f.WPMode = wpconf.ConfiguredMode(dir)
		f.Configured = wpconf.ConfiguredHeadsets(dir)
		f.OldRules = wpconf.OldRules(dir)
	}
	return f
}

// hidDevice reports whether the HID device name exists and whether this
// user may open it.
func (s System) hidDevice(name string) (exists, access bool) {
	uevents, _ := filepath.Glob(s.path("/sys/class/hidraw/*/device/uevent"))
	for _, p := range uevents {
		data, err := os.ReadFile(p)
		if err != nil || !doctor.HIDName(data, name) {
			continue
		}
		exists = true
		dev := s.path("/dev/" + filepath.Base(filepath.Dir(filepath.Dir(p))))
		if h, err := os.OpenFile(dev, os.O_RDWR, 0); err == nil {
			h.Close()
			access = true
		}
	}
	return exists, access
}

// OFonoBackend reports whether PipeWire's Bluetooth plugin has the oFono
// backend: 1 yes, 0 no, -1 if the plugin was not found.
func (s System) OFonoBackend() int {
	var plugins []string
	for _, g := range []string{"/usr/lib/*/spa-0.2/bluez5/libspa-bluez5.so", "/usr/lib*/spa-0.2/bluez5/libspa-bluez5.so"} {
		m, _ := filepath.Glob(s.path(g))
		plugins = append(plugins, m...)
	}
	state := -1
	for _, p := range plugins {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if bytes.Contains(data, []byte("HandsfreeAudioManager")) {
			return 1
		}
		state = 0
	}
	return state
}

// resolve returns p with symlinks resolved, or p if that fails.
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// Programs returns the programs bt-headset-hid on path, in the order the
// shell finds them, symlinks resolved, each once.
func Programs(path string) []string {
	if path == "" {
		path = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	var out []string
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, "bt-headset-hid")
		if fi, err := os.Stat(p); err != nil || fi.IsDir() || fi.Mode()&0o111 == 0 {
			continue
		}
		if r := resolve(p); !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// AdapterState returns 0 without a Bluetooth adapter, 1 if all are off, 2
// if one is on, -1 if unknown.
func AdapterState(conn *dbus.Conn) int {
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := conn.Object("org.bluez", "/").Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objs); err != nil {
		return -1
	}
	state := 0
	for _, ifaces := range objs {
		a, ok := ifaces["org.bluez.Adapter1"]
		if !ok {
			continue
		}
		if on, _ := a["Powered"].Value().(bool); on {
			return 2
		}
		state = 1
	}
	return state
}
