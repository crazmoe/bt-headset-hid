package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	var o wpOptions
	fs.StringVar(&o.mode, "mode", "", `mode the daemon runs in: "ofono", "pipewire" or "hfp" (default: as before, else ofono)`)
	fs.BoolVar(&o.remove, "remove", false, "remove the configuration instead; it stays removed until you run this again")
	fs.BoolVar(&o.uninstall, "uninstall", false, `with -remove, used by "bt-headset-hid uninstall": installing again sets it up again`)
	fs.BoolVar(&o.noRestart, "no-restart", false, "do not restart WirePlumber and PipeWire")
	fs.StringVar(&o.profile, "profile", "", `-mode pipewire and ofono: "switch" plays music (A2DP) on the headsets and switches them to the phone profile for calls, pausing media players; "phone" keeps them in the phone profile (default: as before, else switch)`)
	fs.StringVar(&o.headset, "headset", "", `-mode pipewire and ofono: Bluetooth addresses of the headsets, comma-separated, for the phone profile and the "bt-headset-hid" speaker and microphone (default: as before, else the paired headset)`)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bt-headset-hid wireplumber [-mode ofono|pipewire|hfp] [-headset XX:XX:XX:XX:XX:XX[,...]] [-profile switch|phone] [-remove] [-no-restart]")
		fmt.Fprintln(fs.Output(), "Run as your normal user, not with sudo.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		return errors.New("run this as your normal user without sudo: the configuration belongs to your PipeWire session")
	}
	_, err := setUpWirePlumber(o)
	return err
}

// wpOptions are the options of "bt-headset-hid wireplumber".
type wpOptions struct {
	mode, profile, headset       string
	remove, uninstall, noRestart bool
	// fromSession is set when the session service sets up the user's
	// PipeWire itself; it leaves the service alone.
	fromSession bool
}

// setUpWirePlumber does what runWirePlumber describes and reports whether
// the mode needs the session service.
func setUpWirePlumber(o wpOptions) (bool, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return false, err
	}
	before := wpconf.ConfiguredMode(dir)
	if o.mode == "" {
		if o.mode = before; o.mode == "" {
			o.mode = "ofono"
		}
	}

	snapshot := wpconf.Snapshot(dir)
	session := false
	if o.remove {
		removed, err := wpconf.Remove(dir)
		if err == nil {
			var hs []string
			hs, err = wpconf.RemoveHeadset(dir)
			removed = append(removed, hs...)
		}
		if err == nil {
			var ok bool
			if ok, err = wpconf.RemoveKnown(dir); ok {
				removed = append(removed, wpconf.KnownPath(dir))
			}
		}
		for _, p := range removed {
			fmt.Println("removed", p)
		}
		if err != nil {
			return false, err
		}
		devs, err := wpconf.RemoveDevices(dir)
		if err != nil {
			return false, err
		}
		if devs {
			fmt.Println("removed", wpconf.DevicesPath(dir))
		}
		unit, err := removeSession(dir)
		if err != nil {
			return false, err
		}
		// The packaged session service starts at every login; this keeps it
		// from setting everything up again.
		if err := wpconf.SetOff(dir, !o.uninstall); err != nil {
			return false, err
		}
		if len(removed) == 0 && !devs && !unit {
			fmt.Println("nothing to remove")
		}
	} else {
		if err := wpconf.SetOff(dir, false); err != nil {
			return false, err
		}
		out, err := exec.Command("wireplumber", "--version").CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("wireplumber --version: %v", err)
		}
		v, err := wpconf.ParseVersion(string(out))
		if err != nil {
			return false, err
		}
		path, err := wpconf.Install(dir, o.mode, v)
		if err != nil {
			return false, err
		}
		fmt.Printf("WirePlumber %d.%d: wrote %s for -mode %s\n", v.Major, v.Minor, path, o.mode)
		if o.profile != "" && o.profile != "switch" && o.profile != "phone" {
			return false, fmt.Errorf("-profile %q: want switch or phone", o.profile)
		}
		session, err = setupHeadset(dir, o.mode, o.headset, o.profile, v)
		if err != nil {
			return false, err
		}
	}

	// The headset list alone needs no restart: the session service follows
	// it.
	if changed := wpconf.Snapshot(dir) != snapshot; changed && !o.noRestart {
		fmt.Println("restarting PipeWire and WirePlumber")
		if err := restartPipeWire(); err != nil {
			return false, err
		}
	} else if changed {
		fmt.Println("restart PipeWire and WirePlumber for the change: systemctl --user restart pipewire wireplumber")
	}
	if o.fromSession {
		return session, nil
	}
	if session {
		if err := startSession(dir, !o.noRestart); err != nil {
			return false, err
		}
	} else if !o.remove {
		if _, err := removeSession(dir); err != nil {
			return false, err
		}
	}
	// Only a new mode changes who provides HFP; the headsets alone do not
	// concern the daemon.
	if o.remove || o.mode != before {
		fmt.Println("now restart the daemon: sudo systemctl restart bt-headset-hid")
	}
	return session, nil
}

// setupHeadset sets up the headsets in modes pipewire and ofono: the fixed
// "bt-headset-hid" speaker and microphone use the one connected, and with
// profile "phone" they always stay in the phone profile, otherwise they play
// music and "bt-headset-hid session" switches them for calls. Other Bluetooth
// devices keep A2DP. Mode hfp, where PipeWire has no call audio, removes it
// all. It reports whether the session service is needed; in modes pipewire
// and ofono it always is, since it also sets up headsets paired later.
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
		if ok, err := wpconf.RemoveKnown(dir); err != nil {
			return false, err
		} else if ok {
			removed = append(removed, wpconf.KnownPath(dir))
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
	paired, pairedErr := pairedHeadsets()
	name := strings.Join(addrs, ", ")
	dropped := droppedHeadsets(wpconf.ConfiguredHeadsets(dir), addrs)
	if len(addrs) > 0 && len(dropped) > 0 {
		fmt.Printf("no longer set up: %s; to keep them too: -headset %s\n",
			strings.Join(dropped, ", "), strings.Join(append(addrs, dropped...), ","))
	}
	if len(addrs) == 0 {
		if configured := wpconf.ConfiguredHeadsets(dir); len(configured) > 0 {
			known, ok := wpconf.Known(dir)
			added := wpconf.NewHeadsets(addresses(paired), configured, known, ok)
			addrs = append(configured, added...)
			name = strings.Join(addrs, ", ") + " (as before"
			if len(added) > 0 {
				name += ", and the newly paired " + strings.Join(added, ", ")
			}
			name += "; others: -headset XX:XX:XX:XX:XX:XX,YY:YY:YY:YY:YY:YY)"
		} else if pairedErr != nil {
			fmt.Printf("could not ask BlueZ for the paired headsets: %v\n", pairedErr)
		} else {
			hs := chooseHeadsets(paired, os.Stdin, isTerminal(os.Stdin), os.Stdout)
			var names []string
			for _, h := range hs {
				addrs = append(addrs, h.Address)
				names = append(names, fmt.Sprintf("%s (%s)", h.Name, h.Address))
			}
			name = strings.Join(names, ", ")
		}
	}
	// Headsets paired now and not chosen, and ones dropped, stay as they
	// are, even when paired again; ones paired later for the first time are
	// set up by "bt-headset-hid session".
	known, _ := wpconf.Known(dir)
	known = append(known, addresses(paired)...)
	if len(addrs) > 0 {
		known = append(known, dropped...)
	}
	if err := wpconf.SetKnown(dir, append(known, addrs...)); err != nil {
		return false, err
	}
	path, err := wpconf.InstallHeadset(dir, addrs, v, profile == "phone")
	if err != nil {
		return false, err
	}
	switch {
	case len(addrs) == 0:
		fmt.Println("no headset paired yet: pair it as usual, it is set up by itself once paired")
	case profile == "phone":
		fmt.Printf("wrote %s: %s stay in the phone profile, other Bluetooth devices keep music (A2DP)\n", path, name)
	default:
		fmt.Printf("wrote %s: %s\n", path, name)
		fmt.Println("  they play music (A2DP) and switch to the phone profile for calls; always the phone profile: -profile phone")
	}
	path, err = wpconf.InstallDevices(dir)
	if err != nil {
		return false, err
	}
	fmt.Printf("wrote %s: speaker and microphone \"bt-headset-hid\" for the connected headset\n", path)
	fmt.Println("  choose \"bt-headset-hid\" in the softphone for the phone device, the speaker and the microphone")
	return true, nil
}

// startSession writes and enables the user service "bt-headset-hid session"
// and, with restart, (re)starts it. With the package, which brings the
// service for every user, it only enables and (re)starts that.
func startSession(dir string, restart bool) error {
	if _, err := os.Stat(install.PackagedSessionUnit); err == nil {
		// A copy from an older version would hide the packaged service.
		if ok, err := wpconf.RemoveSessionUnit(dir); err != nil {
			return err
		} else if ok {
			fmt.Println("removed", wpconf.SessionUnitPath(dir), "(the package brings the service)")
		}
		fmt.Println("session service: it switches the headsets to the phone profile for calls, pausing media players, and sets up headsets paired later")
	} else {
		path, err := wpconf.InstallSessionUnit(dir, sessionBinary())
		if err != nil {
			return err
		}
		fmt.Printf("wrote %s: it switches the headsets to the phone profile for calls, pausing media players, and sets up headsets paired later\n", path)
		if self, err := os.Executable(); err == nil {
			if p, err := filepath.EvalSymlinks(self); err == nil {
				self = p
			}
			if bin := sessionBinary(); bin != self {
				fmt.Printf("  NOTE: it runs %s, not this program; to run this version, install it: sudo %s install\n", bin, self)
			}
		}
	}
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

// removeSession stops the user service, removes it unless the package
// brings it, and reports whether it existed.
func removeSession(dir string) (bool, error) {
	_, err := os.Stat(install.PackagedSessionUnit)
	packaged := err == nil
	if _, err := os.Stat(wpconf.SessionUnitPath(dir)); err != nil && !packaged {
		return false, nil
	}
	exec.Command("systemctl", "--user", "disable", "--now", wpconf.SessionUnit).Run()
	ok, err := wpconf.RemoveSessionUnit(dir)
	if ok {
		fmt.Println("removed", wpconf.SessionUnitPath(dir))
		exec.Command("systemctl", "--user", "daemon-reload").Run()
	}
	return ok || packaged, err
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

// pairedHeadsets asks BlueZ for the paired headsets.
func pairedHeadsets() ([]bluez.Headset, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return bluez.Headsets(conn)
}

func addresses(hs []bluez.Headset) []string {
	var addrs []string
	for _, h := range hs {
		addrs = append(addrs, h.Address)
	}
	return addrs
}

// chooseHeadsets picks the headsets to set up from the paired ones hs. With
// several and interactive, it asks on in and out; the default is the
// connected ones, or all if none is.
func chooseHeadsets(hs []bluez.Headset, in io.Reader, interactive bool, out io.Writer) []bluez.Headset {
	if len(hs) <= 1 {
		return hs
	}
	var def []bluez.Headset
	var defNums []string
	for i, h := range hs {
		if h.Connected {
			def = append(def, h)
			defNums = append(defNums, strconv.Itoa(i+1))
		}
	}
	if len(def) == 0 {
		def, defNums = hs, []string{"all"}
	}
	if !interactive {
		fmt.Fprintf(out, "several headsets are paired, setting up %s; to pick others: -headset XX:XX:XX:XX:XX:XX,...\n",
			strings.Join(addresses(def), ", "))
		return def
	}
	fmt.Fprintln(out, "Several headsets are paired. Which ones do you use for calls?")
	for i, h := range hs {
		state := ""
		if h.Connected {
			state = ", connected"
		}
		fmt.Fprintf(out, "  %d) %s (%s%s)\n", i+1, h.Name, h.Address, state)
	}
	r := bufio.NewReader(in)
	for {
		fmt.Fprintf(out, "Numbers separated by commas, \"all\" for all [%s]: ", strings.Join(defNums, ","))
		line, err := r.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer == "" {
			if err != nil {
				fmt.Fprintln(out)
			}
			return def
		}
		if answer == "all" || answer == "a" {
			return hs
		}
		if picked, ok := pickNumbers(answer, hs); ok {
			return picked
		}
		if err != nil {
			fmt.Fprintln(out)
			return def
		}
	}
}

// pickNumbers returns the headsets numbered (from 1) in answer, such as
// "1,3" or "1 3".
func pickNumbers(answer string, hs []bluez.Headset) ([]bluez.Headset, bool) {
	var picked []bluez.Headset
	seen := map[int]bool{}
	for _, f := range strings.FieldsFunc(answer, func(r rune) bool { return r == ',' || r == ' ' }) {
		n, err := strconv.Atoi(f)
		if err != nil || n < 1 || n > len(hs) {
			return nil, false
		}
		if !seen[n] {
			seen[n] = true
			picked = append(picked, hs[n-1])
		}
	}
	return picked, len(picked) > 0
}

// isTerminal reports whether f is a terminal, where a question can be asked.
func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// restartPipeWire restarts the user's PipeWire together with WirePlumber:
// restarting WirePlumber alone can leave PipeWire 1.0 with WirePlumber 0.4
// without any links, so no program has sound.
func restartPipeWire() error {
	if out, err := exec.Command("systemctl", "--user", "restart", "pipewire", "wireplumber").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl --user restart: %v: %s", err, out)
	}
	// Not every system has pipewire-pulse; try-restart ignores it then.
	exec.Command("systemctl", "--user", "try-restart", "pipewire-pulse").Run()
	return nil
}
