package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/crazmoe/bt-headset-hid/internal/doctor"
	"github.com/crazmoe/bt-headset-hid/internal/install"
)

// runInstall implements "bt-headset-hid install" and "uninstall".
func runInstall(args []string, uninstall bool) error {
	name := "install"
	if uninstall {
		name = "uninstall"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	mode := ""
	systemOnly := false
	if !uninstall {
		fs.StringVar(&mode, "mode", "", `"ofono": all buttons and call audio, ModemManager stays free; "pipewire": PipeWire keeps HFP and the audio; "hfp": all buttons, no call audio (default: as before, else ofono if it works here)`)
		fs.BoolVar(&systemOnly, "system-only", false, `only the system part; the session service sets up each user's PipeWire at login (used by the package)`)
	}
	fs.Usage = func() {
		if uninstall {
			fmt.Fprintln(fs.Output(), "usage: sudo bt-headset-hid uninstall")
		} else {
			fmt.Fprintln(fs.Output(), "usage: sudo bt-headset-hid install [-mode ofono|pipewire|hfp] [-system-only]")
		}
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return errors.New("run with sudo")
	}
	if !uninstall && mode == "" {
		mode = chooseMode()
	}
	if mode == "pipewire" {
		out, _ := exec.Command("pipewire", "--version").CombinedOutput()
		switch err := install.CheckPipeWire(string(out), ""); {
		case errors.Is(err, install.ErrPipeWireTooOld):
			return fmt.Errorf("%v.\nUse \"sudo bt-headset-hid install -mode ofono\" (or hfp), update PipeWire or run the fix (see docs/ADVANCED.md)", err)
		case err != nil:
			fmt.Println("WARNING:", err, "- -mode pipewire needs PipeWire 1.2 or later")
		}
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return err
	}
	o := install.Options{Mode: mode, Self: self}
	if user, uid, ok := install.SudoUser(os.Getenv); ok && !systemOnly {
		o.User, o.UID = user, uid
	} else if !systemOnly {
		step := "wireplumber -mode " + mode
		if uninstall {
			step = "wireplumber -remove"
		}
		fmt.Println("note: not started via sudo from a desktop user; skipping the WirePlumber step,")
		fmt.Println("      run \"bt-headset-hid " + step + "\" as that user yourself")
	}

	var steps []install.Step
	if uninstall {
		steps = install.PlanUninstall(o)
	} else if steps, err = install.Plan(o); err != nil {
		return err
	}
	if err := install.Apply(steps, o, "", install.Exec{}, func(s string) { fmt.Println("==>", s) }); err != nil {
		return err
	}

	if uninstall {
		fmt.Println("bt-headset-hid removed. If you use ModemManager and mode pipewire stopped it: sudo systemctl enable --now ModemManager")
		return nil
	}
	fmt.Println("bt-headset-hid installed in mode", mode+".")
	if o.User == "" {
		fmt.Println("Each desktop user's PipeWire is set up at their next login (or now, as that user: bt-headset-hid wireplumber).")
	}
	fmt.Println("Reconnect the headset, then check the setup as your normal user:")
	fmt.Println("  bt-headset-hid doctor")
	return nil
}

// chooseMode keeps the mode of an earlier installation, or picks one that
// works here, preferring ofono.
func chooseMode() string {
	if unit, err := os.ReadFile(install.Unit); err == nil {
		if m := doctor.ModeFromUnit(unit); m != "" {
			fmt.Println("mode", m, "as before; to change it: -mode ofono, pipewire or hfp")
			return m
		}
	}
	out, _ := exec.Command("pipewire", "--version").CombinedOutput()
	mode, why := install.PickMode(install.System{
		OFono:        unitState(false, "ofono.service") == doctor.Active,
		OFonoBackend: ofonoBackend(),
		PipeWireOK:   install.CheckPipeWire(string(out), "") == nil,
		ModemManager: unitState(false, "ModemManager.service") == doctor.Active,
	})
	switch mode {
	case "ofono":
		fmt.Println("mode ofono: all buttons and call audio through the headset")
	case "pipewire":
		fmt.Printf("mode pipewire, since %s: call audio through the headset, but only answer and hang up\n", why)
	default:
		fmt.Printf("mode hfp, since %s: all buttons, but no call audio through the headset\n", why)
	}
	return mode
}
