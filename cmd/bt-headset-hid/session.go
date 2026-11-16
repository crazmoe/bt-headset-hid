package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/crazmoe/bt-headset-hid/internal/session"
)

// runSession implements "bt-headset-hid session", the user service.
func runSession(args []string) error {
	fs := flag.NewFlagSet("session", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bt-headset-hid session")
		fmt.Fprintln(fs.Output(), `Runs in your session (systemd user service "bt-headset-hid-session", set up by the package or "bt-headset-hid wireplumber"). It sets up your PipeWire if needed.`)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return session.Run(ctx, version, log.New(os.Stderr, "", 0))
}
