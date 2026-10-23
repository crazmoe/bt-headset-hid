package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/doctor"
	"github.com/crazmoe/bt-headset-hid/internal/install"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
)

// errProblems makes "bt-headset-hid doctor" exit with 1 after it printed
// the problems.
var errProblems = fmt.Errorf("problems found")

// runDoctor implements "bt-headset-hid doctor": it checks the installation
// and prints what is wrong and how to fix it.
func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bt-headset-hid doctor")
		fmt.Fprintln(fs.Output(), "Checks the setup and says how to fix what is wrong. Run as your normal user.")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	de := german(os.Getenv)
	fmt.Println("bt-headset-hid", version, "doctor")
	findings := doctor.Check(gatherFacts(), de)
	for _, f := range findings {
		mark := map[doctor.Level]string{doctor.OK: "✓", doctor.Info: "i", doctor.Warn: "!", doctor.Fail: "✗"}[f.Level]
		fmt.Printf("  %s %s\n", mark, f.Text)
		if f.Fix != "" && f.Level != doctor.OK {
			if de {
				fmt.Printf("      Lösung: %s\n", f.Fix)
			} else {
				fmt.Printf("      fix: %s\n", f.Fix)
			}
		}
	}
	if doctor.Failed(findings) {
		if de {
			fmt.Println("Probleme gefunden. Nach dem Beheben erneut prüfen: bt-headset-hid doctor")
		} else {
			fmt.Println("Problems found. After fixing them, check again: bt-headset-hid doctor")
		}
		return errProblems
	}
	if de {
		fmt.Println("Alles in Ordnung.")
	} else {
		fmt.Println("All fine.")
	}
	return nil
}

// german reports whether the user's language is German.
func german(getenv func(string) string) bool {
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if l := getenv(v); l != "" {
			return strings.HasPrefix(l, "de")
		}
	}
	return false
}

// unitState asks systemctl for the state of unit; user selects the user's
// units. Units that do not exist are "inactive".
func unitState(user bool, unit string) doctor.State {
	args := []string{"is-active", unit}
	if user {
		args = append([]string{"--user"}, args...)
	}
	out, err := exec.Command("systemctl", args...).Output()
	s := strings.TrimSpace(string(out))
	if s == "" && err != nil {
		return ""
	}
	if s == "unknown" {
		return doctor.Inactive
	}
	return doctor.State(s)
}

func gatherFacts() doctor.Facts {
	f := doctor.Facts{Root: os.Geteuid() == 0, Adapter: -1, OFonoBackend: -1}
	if unit, err := os.ReadFile(install.Unit); err == nil {
		f.Mode = doctor.ModeFromUnit(unit)
	}
	if f.Mode == "" {
		return f
	}
	f.Service = unitState(false, install.Service)
	if out, err := exec.Command("journalctl", "-u", install.Service, "-b", "-n", "300", "-o", "cat", "--no-pager", "-q").Output(); err == nil && len(out) > 0 {
		f.LogReadable = true
		f.Log = doctor.LastStart(strings.Split(strings.TrimSpace(string(out)), "\n"))
	}
	_, err := os.Stat("/dev/uhid")
	f.UHID = err == nil
	_, err = os.Stat(install.UdevRule)
	f.UdevRule = err == nil
	f.DBusPolicy, _ = os.ReadFile(install.DBusPolicy)
	ueventFiles, _ := filepath.Glob("/sys/class/hidraw/*/device/uevent")
	for _, p := range ueventFiles {
		if data, err := os.ReadFile(p); err == nil && doctor.HIDName(data, "bt-headset-hid") {
			f.HIDDevice = true
			dev := "/dev/" + filepath.Base(filepath.Dir(filepath.Dir(p)))
			if h, err := os.OpenFile(dev, os.O_RDWR, 0); err == nil {
				h.Close()
				f.HIDAccess = true
			}
		}
	}

	f.Bluetooth = unitState(false, "bluetooth.service")
	f.ModemManager = unitState(false, "ModemManager.service")
	f.OFono = unitState(false, "ofono.service")
	f.Hsphfpd = unitState(false, "hsphfpd.service")
	if conn, err := dbus.ConnectSystemBus(); err != nil {
		f.BlueZErr = err
	} else {
		defer conn.Close()
		f.Adapter = adapterState(conn)
		hs, err := bluez.Headsets(conn)
		f.BlueZErr = err
		for _, h := range hs {
			f.Paired = append(f.Paired, doctor.Headset{Address: h.Address, Name: h.Name, Connected: h.Connected})
		}
	}
	if out, err := exec.Command("pipewire", "--version").CombinedOutput(); err == nil {
		f.PipeWireVersion = string(out)
	}
	f.OFonoBackend = ofonoBackend()
	if f.Root {
		return f
	}
	f.PipeWire = unitState(true, "pipewire.service")
	f.WirePlumber = unitState(true, "wireplumber.service")
	f.PulseAudio = unitState(true, "pulseaudio.service")
	f.Session = unitState(true, wpconf.SessionUnit)
	if dir, err := os.UserConfigDir(); err == nil {
		f.WPMode = wpconf.ConfiguredMode(dir)
		f.Configured = wpconf.ConfiguredHeadsets(dir)
		f.OldRules = wpconf.OldRules(dir)
	}
	return f
}

// adapterState returns 0 without a Bluetooth adapter, 1 if all are off, 2
// if one is on, -1 if unknown.
func adapterState(conn *dbus.Conn) int {
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

// ofonoBackend reports whether PipeWire's Bluetooth plugin has the oFono
// backend: 1 yes, 0 no, -1 if the plugin was not found.
func ofonoBackend() int {
	var plugins []string
	for _, g := range []string{"/usr/lib/*/spa-0.2/bluez5/libspa-bluez5.so", "/usr/lib*/spa-0.2/bluez5/libspa-bluez5.so"} {
		m, _ := filepath.Glob(g)
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
