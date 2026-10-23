// Command bt-headset-hid bridges Bluetooth headset buttons to a virtual HID telephony device.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/crazmoe/bt-headset-hid/internal/daemon"
	"github.com/crazmoe/bt-headset-hid/internal/install"
)

// version is overridden at release build time with -ldflags "-X main.version=...".
var version = "0.4.0"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "wireplumber" || os.Args[1] == "session" || os.Args[1] == "install" || os.Args[1] == "uninstall" || os.Args[1] == "doctor") {
		var err error
		switch os.Args[1] {
		case "wireplumber":
			err = runWirePlumber(os.Args[2:])
		case "session":
			err = runSession(os.Args[2:])
		case "doctor":
			err = runDoctor(os.Args[2:])
		default:
			err = runInstall(os.Args[2:], os.Args[1] == "uninstall")
		}
		switch {
		case err == flag.ErrHelp:
		case err == errProblems:
			os.Exit(1)
		case err != nil:
			fmt.Fprintf(os.Stderr, "bt-headset-hid %s: %v\n", os.Args[1], err)
			os.Exit(1)
		}
		return
	}
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: bt-headset-hid [flags]")
		fmt.Fprintln(flag.CommandLine.Output(), "       sudo bt-headset-hid install [-mode ofono|pipewire|hfp]   (set up everything)")
		fmt.Fprintln(flag.CommandLine.Output(), "       sudo bt-headset-hid uninstall")
		fmt.Fprintln(flag.CommandLine.Output(), "       bt-headset-hid doctor   (check the setup and say how to fix what is wrong)")
		fmt.Fprintln(flag.CommandLine.Output(), "       bt-headset-hid wireplumber [-mode ofono|pipewire|hfp] [-remove]   (only PipeWire, as your user)")
		fmt.Fprintln(flag.CommandLine.Output(), "       bt-headset-hid session   (user service of modes pipewire and ofono: phone profile during calls)")
		flag.PrintDefaults()
	}
	mode := flag.String("mode", string(daemon.ModeOFono),
		`"ofono": all buttons and call audio, ModemManager stays free; "pipewire": PipeWire keeps HFP and the audio, answer/hang up only; "hfp": all buttons, no call audio`)
	name := flag.String("name", "bt-headset-hid", "HID device name")
	vendor := flag.Uint("vendor", daemon.DefaultVendor, "HID vendor ID")
	product := flag.Uint("product", daemon.DefaultProduct, "HID product ID")
	grabMediaKeys := flag.Bool("grab-media-keys", false,
		`-mode pipewire and ofono: use the headset's play/pause key (AVRCP) as the call button instead of leaving it to the media player`)
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("bt-headset-hid", version)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := log.New(os.Stderr, "", 0)
	if daemon.Mode(*mode) == daemon.ModePipeWire {
		out, _ := exec.Command("pipewire", "--version").CombinedOutput()
		if err := install.CheckPipeWire(string(out), ""); errors.Is(err, install.ErrPipeWireTooOld) {
			logger.Printf("WARNING: %v; or use -mode hfp", err)
		}
	}
	err := daemon.Run(ctx, daemon.Config{
		Mode:          daemon.Mode(*mode),
		Name:          *name,
		Vendor:        uint32(*vendor),
		Product:       uint32(*product),
		GrabMediaKeys: *grabMediaKeys,
		Logf:          logger.Printf,
	})
	if err != nil {
		logger.Fatal(err)
	}
}
