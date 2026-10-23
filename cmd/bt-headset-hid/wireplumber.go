package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/install"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
)

// runWirePlumber implements "bt-headset-hid wireplumber": it installs or
// removes the WirePlumber configuration of the current user for a mode, in
// modes pipewire and ofono also the fixed "bt-headset-hid" speaker and microphone, and
// restarts the user's PipeWire session.
func runWirePlumber(args []string) error {
	fs := flag.NewFlagSet("wireplumber", flag.ContinueOnError)
	mode := fs.String("mode", "", `mode the daemon runs in: "pipewire", "hfp" or "ofono" (default: as before, else pipewire)`)
	remove := fs.Bool("remove", false, "remove the configuration instead")
	noRestart := fs.Bool("no-restart", false, "do not restart WirePlumber and PipeWire")
	profile := fs.String("profile", "", `-mode pipewire and ofono: "switch" plays music (A2DP) on the headsets and switches them to the phone profile for calls, pausing media players; "phone" keeps them in the phone profile (default: as before, else switch)`)
	headset := fs.String("headset", "", `-mode pipewire and ofono: Bluetooth addresses of the headsets, comma-separated, for the phone profile and the "bt-headset-hid" speaker and microphone (default: as before, else the paired headset)`)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bt-headset-hid wireplumber [-mode pipewire|hfp|ofono] [-headset XX:XX:XX:XX:XX:XX[,...]] [-profile switch|phone] [-remove] [-no-restart]")
		fmt.Fprintln(fs.Output(), "Run as your normal user, not with sudo.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		return errors.New("run this as your normal user without sudo: the configuration belongs to your PipeWire session")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	if *mode == "" {
		if *mode = wpconf.ConfiguredMode(dir); *mode == "" {
			*mode = "pipewire"
		}
	}

	session := false
	if *remove {
		removed, err := wpconf.Remove(dir)
		if err == nil {
			var hs []string
			hs, err = wpconf.RemoveHeadset(dir)
			removed = append(removed, hs...)
		}
		for _, p := range removed {
			fmt.Println("removed", p)
		}
		if err != nil {
			return err
		}
		devs, err := wpconf.RemoveDevices(dir)
		if err != nil {
			return err
		}
		if devs {
			fmt.Println("removed", wpconf.DevicesPath(dir))
		}
		unit, err := removeSession(dir)
		if err != nil {
			return err
		}
		if len(removed) == 0 && !devs && !unit {
			fmt.Println("nothing to remove")
		}
	} else {
		out, err := exec.Command("wireplumber", "--version").CombinedOutput()
		if err != nil {
			return fmt.Errorf("wireplumber --version: %v", err)
		}
		v, err := wpconf.ParseVersion(string(out))
		if err != nil {
			return err
		}
		path, err := wpconf.Install(dir, *mode, v)
		if err != nil {
			return err
		}
		fmt.Printf("WirePlumber %d.%d: wrote %s for -mode %s\n", v.Major, v.Minor, path, *mode)
		if *profile != "" && *profile != "switch" && *profile != "phone" {
			return fmt.Errorf("-profile %q: want switch or phone", *profile)
		}
		session, err = setupHeadset(dir, *mode, *headset, *profile, v)
		if err != nil {
			return err
		}
	}

	if !*noRestart {
		fmt.Println("restarting PipeWire and WirePlumber")
		if out, err := exec.Command("systemctl", "--user", "restart", "pipewire", "wireplumber").CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl --user restart: %v: %s", err, out)
		}
		// Not every system has pipewire-pulse; try-restart ignores it then.
		exec.Command("systemctl", "--user", "try-restart", "pipewire-pulse").Run()
	}
	if session {
		if err := startSession(dir, !*noRestart); err != nil {
			return err
		}
	} else if !*remove {
		if _, err := removeSession(dir); err != nil {
			return err
		}
	}
	fmt.Println("now restart the daemon: sudo systemctl restart bt-headset-hid")
	return nil
}

// setupHeadset sets up the headsets in modes pipewire and ofono: the fixed
// "bt-headset-hid" speaker and microphone use the one connected, and with
// profile "phone" they always stay in the phone profile, otherwise they play
// music and "bt-headset-hid session" switches them for calls. Other Bluetooth
// devices keep A2DP. Mode hfp, where PipeWire has no call audio, removes it
// all. It reports whether the session service is needed.
func setupHeadset(dir, mode, addr, profile string, v wpconf.Version) (bool, error) {
	if mode == "hfp" {
		removed, err := wpconf.RemoveHeadset(dir)
		if err != nil {
			return false, err
		}
		ok, err := wpconf.RemoveDevices(dir)
		if err != nil {
			return false, err
		}
		if ok {
			removed = append(removed, wpconf.DevicesPath(dir))
		}
		for _, p := range removed {
			fmt.Println("removed", p)
		}
		return false, nil
	}
	if profile == "" {
		profile = "switch"
		if wpconf.ConfiguredPhone(dir) {
			profile = "phone"
		}
	}
	var addrs []string
	for _, a := range strings.Split(addr, ",") {
		if a = strings.TrimSpace(a); a != "" {
			addrs = append(addrs, a)
		}
	}
	name := strings.Join(addrs, ", ")
	if dropped := droppedHeadsets(wpconf.ConfiguredHeadsets(dir), addrs); len(addrs) > 0 && len(dropped) > 0 {
		fmt.Printf("no longer set up: %s; to keep them too: -headset %s\n",
			strings.Join(dropped, ", "), strings.Join(append(addrs, dropped...), ","))
	}
	if len(addrs) == 0 {
		addrs = wpconf.ConfiguredHeadsets(dir)
		name = strings.Join(addrs, ", ") + " (as before; others: -headset XX:XX:XX:XX:XX:XX,YY:YY:YY:YY:YY:YY)"
	}
	if len(addrs) == 0 {
		h, err := findHeadset()
		if err != nil {
			fmt.Printf("no phone profile and no speaker and microphone \"bt-headset-hid\": %v\n", err)
			fmt.Println("  pair the headset, then run this again, or pass -headset XX:XX:XX:XX:XX:XX")
			return false, nil
		}
		addrs, name = []string{h.Address}, fmt.Sprintf("%s (%s)", h.Name, h.Address)
	}
	path, err := wpconf.InstallHeadset(dir, addrs, v, profile == "phone")
	if err != nil {
		return false, err
	}
	if profile == "phone" {
		fmt.Printf("wrote %s: %s stay in the phone profile, other Bluetooth devices keep music (A2DP)\n", path, name)
	} else {
		fmt.Printf("wrote %s: %s\n", path, name)
		fmt.Println("  they play music (A2DP) and switch to the phone profile for calls; always the phone profile: -profile phone")
	}
	path, err = wpconf.InstallDevices(dir)
	if err != nil {
		return false, err
	}
	fmt.Printf("wrote %s: speaker and microphone \"bt-headset-hid\" for the connected headset\n", path)
	fmt.Println("  choose \"bt-headset-hid\" in the softphone for the phone device, the speaker and the microphone")
	return profile != "phone", nil
}

// startSession writes and enables the user service "bt-headset-hid session"
// and, with restart, (re)starts it.
func startSession(dir string, restart bool) error {
	path, err := wpconf.InstallSessionUnit(dir, sessionBinary())
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s: during calls it switches the headsets to the phone profile and pauses media players\n", path)
	cmds := [][]string{{"daemon-reload"}, {"enable", wpconf.SessionUnit}}
	if restart {
		cmds = append(cmds, []string{"restart", wpconf.SessionUnit})
	}
	for _, c := range cmds {
		if out, err := exec.Command("systemctl", append([]string{"--user"}, c...)...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl --user %s: %v: %s", strings.Join(c, " "), err, out)
		}
	}
	return nil
}

// removeSession stops and removes the user service and reports whether it
// existed.
func removeSession(dir string) (bool, error) {
	if _, err := os.Stat(wpconf.SessionUnitPath(dir)); err != nil {
		return false, nil
	}
	exec.Command("systemctl", "--user", "disable", "--now", wpconf.SessionUnit).Run()
	ok, err := wpconf.RemoveSessionUnit(dir)
	if ok {
		fmt.Println("removed", wpconf.SessionUnitPath(dir))
		exec.Command("systemctl", "--user", "daemon-reload").Run()
	}
	return ok, err
}

// sessionBinary is the program the session service runs: the installed one
// if there is one, else this one.
func sessionBinary() string {
	for _, p := range []string{install.Binary, install.PackagedBinary} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	self, err := os.Executable()
	if err != nil {
		return install.Binary
	}
	if p, err := filepath.EvalSymlinks(self); err == nil {
		self = p
	}
	return self
}

// droppedHeadsets returns the headsets of before that are not in addrs.
func droppedHeadsets(before, addrs []string) []string {
	var dropped []string
	for _, b := range before {
		found := false
		for _, a := range addrs {
			if strings.EqualFold(a, b) {
				found = true
			}
		}
		if !found {
			dropped = append(dropped, b)
		}
	}
	return dropped
}

// findHeadset asks BlueZ for the paired headset.
func findHeadset() (bluez.Headset, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return bluez.Headset{}, err
	}
	defer conn.Close()
	hs, err := bluez.Headsets(conn)
	if err != nil {
		return bluez.Headset{}, fmt.Errorf("BlueZ: %w", err)
	}
	if h, ok := bluez.PickHeadset(hs); ok {
		return h, nil
	}
	if len(hs) == 0 {
		return bluez.Headset{}, errors.New("no paired headset found")
	}
	var all []string
	msg := "several headsets, pick them (comma-separated) or connect only the one to use:"
	for _, h := range hs {
		msg += fmt.Sprintf("\n    -headset %s   %s", h.Address, h.Name)
		all = append(all, h.Address)
	}
	msg += fmt.Sprintf("\n    -headset %s   all of them", strings.Join(all, ","))
	return bluez.Headset{}, errors.New(msg)
}
