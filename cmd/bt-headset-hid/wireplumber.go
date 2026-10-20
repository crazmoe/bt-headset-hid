package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
)

// runWirePlumber implements "bt-headset-hid wireplumber": it installs or
// removes the WirePlumber configuration of the current user for a mode and
// restarts the user's PipeWire session.
func runWirePlumber(args []string) error {
	fs := flag.NewFlagSet("wireplumber", flag.ContinueOnError)
	mode := fs.String("mode", "pipewire", `mode the daemon runs in: "pipewire" or "hfp"`)
	remove := fs.Bool("remove", false, "remove the configuration instead")
	noRestart := fs.Bool("no-restart", false, "do not restart WirePlumber and PipeWire")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bt-headset-hid wireplumber [-mode pipewire|hfp] [-remove] [-no-restart]")
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

	if *remove {
		removed, err := wpconf.Remove(dir)
		for _, p := range removed {
			fmt.Println("removed", p)
		}
		if err != nil {
			return err
		}
		if len(removed) == 0 {
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
	}

	if !*noRestart {
		fmt.Println("restarting PipeWire and WirePlumber")
		if out, err := exec.Command("systemctl", "--user", "restart", "pipewire", "wireplumber").CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl --user restart: %v: %s", err, out)
		}
		// Not every system has pipewire-pulse; try-restart ignores it then.
		exec.Command("systemctl", "--user", "try-restart", "pipewire-pulse").Run()
	}
	fmt.Println("now restart the daemon: sudo systemctl restart bt-headset-hid")
	return nil
}
