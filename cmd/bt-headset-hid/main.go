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
