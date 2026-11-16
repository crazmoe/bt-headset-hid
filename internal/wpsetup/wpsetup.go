// Package wpsetup sets up the user's PipeWire for a mode: it writes or
// removes the WirePlumber configuration of package wpconf, sets up the
// headsets and the user service "bt-headset-hid session", and restarts
// PipeWire. "bt-headset-hid wireplumber" and the session service use it.
// Commands, BlueZ and the terminal come through Env, so it can be tested.
package wpsetup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/install"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
)

// Options are the options of "bt-headset-hid wireplumber".
type Options struct {
	Mode, Profile, Headset       string
	Remove, Uninstall, NoRestart bool
	// FromSession is set when the session service sets up the user's
	// PipeWire itself; it leaves the service alone.
	FromSession bool
}

// Env is the world Setup works in.
type Env struct {
	ConfigDir string // the user's ~/.config
	Out       io.Writer
	// In is where the question which headsets to set up is answered, if
	// Interactive.
	In          io.Reader
	Interactive bool
	// Run runs a command and returns its output and errors.
	Run func(name string, args ...string) ([]byte, error)
	// Paired asks BlueZ for the paired headsets.
	Paired func() ([]bluez.Headset, error)
	// Packaged: the package brings the session service for every user.
	Packaged bool
	// SessionBinary is the program the session service runs, Self this
	// program, both with symlinks resolved.
	SessionBinary, Self string
}

// RealEnv is the environment of the user running this program.
func RealEnv() (Env, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return Env{}, err
	}
	self, err := os.Executable()
	if err == nil {
		if p, err := filepath.EvalSymlinks(self); err == nil {
			self = p
		}
	}
	_, err = os.Stat(install.PackagedSessionUnit)
	return Env{
		ConfigDir:   dir,
		Out:         os.Stdout,
		In:          os.Stdin,
		Interactive: isTerminal(os.Stdin),
		Run: func(name string, args ...string) ([]byte, error) {
			return exec.Command(name, args...).CombinedOutput()
		},
		Paired: func() ([]bluez.Headset, error) {
			conn, err := dbus.ConnectSystemBus()
			if err != nil {
				return nil, err
			}
			defer conn.Close()
			return bluez.Headsets(conn)
		},
		Packaged:      err == nil,
		SessionBinary: SessionBinary(exists, self),
		Self:          self,
	}, nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// isTerminal reports whether f is a terminal, where a question can be asked.
func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// SessionBinary is the program the session service runs: the installed one
// if there is one, else self.
func SessionBinary(exists func(string) bool, self string) string {
	for _, p := range []string{install.Binary, install.PackagedBinary} {
		if exists(p) {
			return p
		}
	}
	if self == "" {
		return install.Binary
	}
	return self
}

// Setup installs or, with o.Remove, removes the WirePlumber configuration of
// the user for a mode, in modes pipewire and ofono also the fixed
// "bt-headset-hid" speaker and microphone, and restarts the user's PipeWire.
// It reports whether the mode needs the session service.
func Setup(o Options, e Env) (bool, error) {
	dir := e.ConfigDir
	before := wpconf.ConfiguredMode(dir)
	if o.Mode == "" {
		if o.Mode = before; o.Mode == "" {
			o.Mode = "ofono"
		}
	}
	if !o.Remove && o.Profile != "" && o.Profile != "switch" && o.Profile != "phone" {
		return false, fmt.Errorf("-profile %q: want switch or phone", o.Profile)
	}

	snapshot := wpconf.Snapshot(dir)
	session := false
	if o.Remove {
		if err := remove(o, e); err != nil {
			return false, err
		}
	} else {
		if err := wpconf.SetOff(dir, false); err != nil {
			return false, err
		}
		out, err := e.Run("wireplumber", "--version")
		if err != nil {
			return false, fmt.Errorf("wireplumber --version: %v", err)
		}
		v, err := wpconf.ParseVersion(string(out))
		if err != nil {
			return false, err
		}
		path, err := wpconf.Install(dir, o.Mode, v)
		if err != nil {
			return false, err
		}
		fmt.Fprintf(e.Out, "WirePlumber %d.%d: wrote %s for -mode %s\n", v.Major, v.Minor, path, o.Mode)
		session, err = setupHeadset(e, o.Mode, o.Headset, o.Profile, v)
		if err != nil {
			return false, err
		}
	}

	// The headset list alone needs no restart: the session service follows
	// it.
	if changed := wpconf.Snapshot(dir) != snapshot; changed && !o.NoRestart {
		fmt.Fprintln(e.Out, "restarting PipeWire and WirePlumber")
		if err := restartPipeWire(e); err != nil {
			return false, err
		}
	} else if changed {
		fmt.Fprintln(e.Out, "restart PipeWire and WirePlumber for the change: systemctl --user restart pipewire wireplumber")
	}
	if o.FromSession {
		return session, nil
	}
	if session {
		if err := startSession(e, !o.NoRestart); err != nil {
			return false, err
		}
	} else if !o.Remove {
		if _, err := removeSession(e); err != nil {
			return false, err
		}
	}
	// Only a new mode changes who provides HFP; the headsets alone do not
	// concern the daemon.
	if o.Remove || o.Mode != before {
		fmt.Fprintln(e.Out, "now restart the daemon: sudo systemctl restart bt-headset-hid")
	}
	return session, nil
}

// remove removes the whole configuration. Unless o.Uninstall, it stays
// removed: the packaged session service, which starts at every login, does
// not set it up again.
func remove(o Options, e Env) error {
	dir := e.ConfigDir
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
		fmt.Fprintln(e.Out, "removed", p)
	}
	if err != nil {
		return err
	}
	devs, err := wpconf.RemoveDevices(dir)
	if err != nil {
		return err
	}
	if devs {
		fmt.Fprintln(e.Out, "removed", wpconf.DevicesPath(dir))
	}
	unit, err := removeSession(e)
	if err != nil {
		return err
	}
	if err := wpconf.SetOff(dir, !o.Uninstall); err != nil {
		return err
	}
	if len(removed) == 0 && !devs && !unit {
		fmt.Fprintln(e.Out, "nothing to remove")
	}
	return nil
}

// setupHeadset sets up the headsets in modes pipewire and ofono: the fixed
// "bt-headset-hid" speaker and microphone use the one connected, and with
// profile "phone" they always stay in the phone profile, otherwise they play
// music and "bt-headset-hid session" switches them for calls. Other Bluetooth
// devices keep A2DP. Mode hfp, where PipeWire has no call audio, removes it
// all. It reports whether the session service is needed; in modes pipewire
// and ofono it always is, since it also sets up headsets paired later.
func setupHeadset(e Env, mode, addr, profile string, v wpconf.Version) (bool, error) {
	dir := e.ConfigDir
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
			fmt.Fprintln(e.Out, "removed", p)
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
	paired, pairedErr := e.Paired()
	name := strings.Join(addrs, ", ")
	dropped := DroppedHeadsets(wpconf.ConfiguredHeadsets(dir), addrs)
	if len(addrs) > 0 && len(dropped) > 0 {
		fmt.Fprintf(e.Out, "no longer set up: %s; to keep them too: -headset %s\n",
			strings.Join(dropped, ", "), strings.Join(append(addrs, dropped...), ","))
	}
	if len(addrs) == 0 {
		if configured := wpconf.ConfiguredHeadsets(dir); len(configured) > 0 {
			known, ok := wpconf.Known(dir)
			added := wpconf.NewHeadsets(Addresses(paired), configured, known, ok)
			addrs = append(configured, added...)
			name = strings.Join(addrs, ", ") + " (as before"
			if len(added) > 0 {
				name += ", and the newly paired " + strings.Join(added, ", ")
			}
			name += "; others: -headset XX:XX:XX:XX:XX:XX,YY:YY:YY:YY:YY:YY)"
		} else if pairedErr != nil {
			fmt.Fprintf(e.Out, "could not ask BlueZ for the paired headsets: %v\n", pairedErr)
		} else {
			hs := ChooseHeadsets(paired, e.In, e.Interactive, e.Out)
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
	known = append(known, Addresses(paired)...)
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
		fmt.Fprintln(e.Out, "no headset paired yet: pair it as usual, it is set up by itself once paired")
	case profile == "phone":
		fmt.Fprintf(e.Out, "wrote %s: %s stay in the phone profile, other Bluetooth devices keep music (A2DP)\n", path, name)
	default:
		fmt.Fprintf(e.Out, "wrote %s: %s\n", path, name)
		fmt.Fprintln(e.Out, "  they play music (A2DP) and switch to the phone profile for calls; always the phone profile: -profile phone")
	}
	path, err = wpconf.InstallDevices(dir)
	if err != nil {
		return false, err
	}
	fmt.Fprintf(e.Out, "wrote %s: speaker and microphone \"bt-headset-hid\" for the connected headset\n", path)
	fmt.Fprintln(e.Out, "  choose \"bt-headset-hid\" in the softphone for the phone device, the speaker and the microphone")
	return true, nil
}

// startSession writes and enables the user service "bt-headset-hid session"
// and, with restart, (re)starts it. With the package, which brings the
// service for every user, it only enables and (re)starts that.
func startSession(e Env, restart bool) error {
	dir := e.ConfigDir
	if e.Packaged {
		// A copy from an older version would hide the packaged service.
		if ok, err := wpconf.RemoveSessionUnit(dir); err != nil {
			return err
		} else if ok {
			fmt.Fprintln(e.Out, "removed", wpconf.SessionUnitPath(dir), "(the package brings the service)")
		}
		fmt.Fprintln(e.Out, "session service: it switches the headsets to the phone profile for calls, pausing media players, and sets up headsets paired later")
	} else {
		path, err := wpconf.InstallSessionUnit(dir, e.SessionBinary)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.Out, "wrote %s: it switches the headsets to the phone profile for calls, pausing media players, and sets up headsets paired later\n", path)
		if e.Self != "" && e.SessionBinary != e.Self {
			fmt.Fprintf(e.Out, "  NOTE: it runs %s, not this program; to run this version, install it: sudo %s install\n", e.SessionBinary, e.Self)
		}
	}
	cmds := [][]string{{"daemon-reload"}, {"enable", wpconf.SessionUnit}}
	if restart {
		cmds = append(cmds, []string{"restart", wpconf.SessionUnit})
	}
	for _, c := range cmds {
		if out, err := e.Run("systemctl", append([]string{"--user"}, c...)...); err != nil {
			return fmt.Errorf("systemctl --user %s: %v: %s", strings.Join(c, " "), err, out)
		}
	}
	return nil
}

// removeSession stops the user service, removes it unless the package
// brings it, and reports whether it existed.
func removeSession(e Env) (bool, error) {
	dir := e.ConfigDir
	if _, err := os.Stat(wpconf.SessionUnitPath(dir)); err != nil && !e.Packaged {
		return false, nil
	}
	e.Run("systemctl", "--user", "disable", "--now", wpconf.SessionUnit)
	ok, err := wpconf.RemoveSessionUnit(dir)
	if ok {
		fmt.Fprintln(e.Out, "removed", wpconf.SessionUnitPath(dir))
		e.Run("systemctl", "--user", "daemon-reload")
	}
	return ok || e.Packaged, err
}

// restartPipeWire restarts the user's PipeWire together with WirePlumber:
// restarting WirePlumber alone can leave PipeWire 1.0 with WirePlumber 0.4
// without any links, so no program has sound.
func restartPipeWire(e Env) error {
	if out, err := e.Run("systemctl", "--user", "restart", "pipewire", "wireplumber"); err != nil {
		return fmt.Errorf("systemctl --user restart: %v: %s", err, out)
	}
	// Not every system has pipewire-pulse; try-restart ignores it then.
	e.Run("systemctl", "--user", "try-restart", "pipewire-pulse")
	return nil
}

// ErrRoot is returned for "bt-headset-hid wireplumber" run as root.
var ErrRoot = errors.New("run this as your normal user without sudo: the configuration belongs to your PipeWire session")
