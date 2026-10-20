// Command bt-headset-hid bridges Bluetooth headset buttons to a virtual HID telephony device.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/crazmoe/bt-headset-hid/internal/daemon"
)

const version = "0.1.0"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "wireplumber" || os.Args[1] == "install" || os.Args[1] == "uninstall") {
		var err error
		switch os.Args[1] {
		case "wireplumber":
			err = runWirePlumber(os.Args[2:])
		default:
			err = runInstall(os.Args[2:], os.Args[1] == "uninstall")
		}
		switch {
		case err == flag.ErrHelp:
		case err != nil:
			fmt.Fprintf(os.Stderr, "bt-headset-hid %s: %v\n", os.Args[1], err)
			os.Exit(1)
		}
		return
	}
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: bt-headset-hid [flags]")
		fmt.Fprintln(flag.CommandLine.Output(), "       sudo bt-headset-hid install [-mode pipewire|hfp]   (set up everything)")
		fmt.Fprintln(flag.CommandLine.Output(), "       sudo bt-headset-hid uninstall")
		fmt.Fprintln(flag.CommandLine.Output(), "       bt-headset-hid wireplumber [-mode pipewire|hfp] [-remove]   (only PipeWire, as your user)")
		flag.PrintDefaults()
	}
	mode := flag.String("mode", string(daemon.ModePipeWire),
		`"pipewire": PipeWire keeps HFP and the audio, answer/hang up only; "hfp": all buttons, no call audio`)
	name := flag.String("name", "bt-headset-hid", "HID device name")
	vendor := flag.Uint("vendor", daemon.DefaultVendor, "HID vendor ID")
	product := flag.Uint("product", daemon.DefaultProduct, "HID product ID")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("bt-headset-hid", version)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := log.New(os.Stderr, "", 0)
	err := daemon.Run(ctx, daemon.Config{
		Mode:    daemon.Mode(*mode),
		Name:    *name,
		Vendor:  uint32(*vendor),
		Product: uint32(*product),
		Logf:    logger.Printf,
	})
	if err != nil {
		logger.Fatal(err)
	}
}
