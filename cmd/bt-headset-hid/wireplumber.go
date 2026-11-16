package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/crazmoe/bt-headset-hid/internal/wpsetup"
)

// runWirePlumber implements "bt-headset-hid wireplumber": it installs or
// removes the WirePlumber configuration of the current user for a mode, in
// modes pipewire and ofono also the fixed "bt-headset-hid" speaker and
// microphone, and restarts the user's PipeWire session.
func runWirePlumber(args []string) error {
	fs := flag.NewFlagSet("wireplumber", flag.ContinueOnError)
	var o wpsetup.Options
	fs.StringVar(&o.Mode, "mode", "", `mode the daemon runs in: "ofono", "pipewire" or "hfp" (default: as before, else ofono)`)
	fs.BoolVar(&o.Remove, "remove", false, "remove the configuration instead; it stays removed until you run this again")
	fs.BoolVar(&o.Uninstall, "uninstall", false, `with -remove, used by "bt-headset-hid uninstall": installing again sets it up again`)
	fs.BoolVar(&o.NoRestart, "no-restart", false, "do not restart WirePlumber and PipeWire")
	fs.StringVar(&o.Profile, "profile", "", `-mode pipewire and ofono: "switch" plays music (A2DP) on the headsets and switches them to the phone profile for calls, pausing media players; "phone" keeps them in the phone profile (default: as before, else switch)`)
	fs.StringVar(&o.Headset, "headset", "", `-mode pipewire and ofono: Bluetooth addresses of the headsets, comma-separated, for the phone profile and the "bt-headset-hid" speaker and microphone (default: as before, else the paired headset)`)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bt-headset-hid wireplumber [-mode ofono|pipewire|hfp] [-headset XX:XX:XX:XX:XX:XX[,...]] [-profile switch|phone] [-remove] [-no-restart]")
		fmt.Fprintln(fs.Output(), "Run as your normal user, not with sudo.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		return wpsetup.ErrRoot
	}
	e, err := wpsetup.RealEnv()
	if err != nil {
		return err
	}
	_, err = wpsetup.Setup(o, e)
	return err
}
