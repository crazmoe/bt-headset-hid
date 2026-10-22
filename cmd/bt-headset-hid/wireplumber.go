package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
)

// runWirePlumber implements "bt-headset-hid wireplumber": it installs or
// removes the WirePlumber configuration of the current user for a mode, in
// mode pipewire also the fixed "bt-headset-hid" speaker and microphone, and
// restarts the user's PipeWire session.
func runWirePlumber(args []string) error {
	fs := flag.NewFlagSet("wireplumber", flag.ContinueOnError)
	mode := fs.String("mode", "pipewire", `mode the daemon runs in: "pipewire" or "hfp"`)
	remove := fs.Bool("remove", false, "remove the configuration instead")
	noRestart := fs.Bool("no-restart", false, "do not restart WirePlumber and PipeWire")
	headset := fs.String("headset", "", `-mode pipewire: Bluetooth address of the headset for the "bt-headset-hid" speaker and microphone (default: the paired headset)`)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bt-headset-hid wireplumber [-mode pipewire|hfp] [-headset XX:XX:XX:XX:XX:XX] [-remove] [-no-restart]")
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
		devs, err := wpconf.RemoveDevices(dir)
		if err != nil {
			return err
		}
		if devs {
			fmt.Println("removed", wpconf.DevicesPath(dir))
		}
		if len(removed) == 0 && !devs {
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
		if err := setupDevices(dir, *mode, *headset); err != nil {
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
	fmt.Println("now restart the daemon: sudo systemctl restart bt-headset-hid")
	return nil
}

// setupDevices writes the fixed "bt-headset-hid" speaker and microphone for
// the headset in mode pipewire and removes them in mode hfp, where PipeWire
// has no call audio.
func setupDevices(dir, mode, addr string) error {
	if mode != "pipewire" {
		if ok, err := wpconf.RemoveDevices(dir); err != nil || !ok {
			return err
		}
		fmt.Println("removed", wpconf.DevicesPath(dir))
		return nil
	}
	name := addr
	if addr == "" {
		h, err := findHeadset()
		if err != nil {
			fmt.Printf("no speaker and microphone \"bt-headset-hid\": %v\n", err)
			fmt.Println("  pair the headset, then run this again, or pass -headset XX:XX:XX:XX:XX:XX")
			return nil
		}
		addr, name = h.Address, fmt.Sprintf("%s (%s)", h.Name, h.Address)
	}
	path, err := wpconf.InstallDevices(dir, addr)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s: speaker and microphone \"bt-headset-hid\" for %s\n", path, name)
	fmt.Println("  choose \"bt-headset-hid\" in the softphone for the phone device, the speaker and the microphone")
	return nil
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
	msg := "several headsets, connect only the one to use or pick one:"
	for _, h := range hs {
		msg += fmt.Sprintf("\n    -headset %s   %s", h.Address, h.Name)
	}
	return bluez.Headset{}, errors.New(msg)
}
