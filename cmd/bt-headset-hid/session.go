package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/callswitch"
	"github.com/crazmoe/bt-headset-hid/internal/desktop"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
)

// endDelay is how long no call must be going on before the headsets switch
// back, so that a short gap in the call state does not switch them twice.
const endDelay = 2 * time.Second

// runSession implements "bt-headset-hid session", which runs in the user's
// session in modes pipewire and ofono: during a call it pauses the media players and
// switches the headsets set up with "bt-headset-hid wireplumber" from music
// (A2DP) to the phone profile, and back afterwards.
func runSession(args []string) error {
	fs := flag.NewFlagSet("session", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bt-headset-hid session")
		fmt.Fprintln(fs.Output(), `Runs in your session (systemd user service "bt-headset-hid-session", set up by "bt-headset-hid wireplumber").`)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	logger := log.New(os.Stderr, "", 0)
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	addrs := wpconf.ConfiguredHeadsets(dir)
	if len(addrs) == 0 {
		return errors.New(`no headsets set up, run "bt-headset-hid wireplumber" first`)
	}
	names := map[string]bool{}
	for _, a := range addrs {
		names["bluez_card."+strings.ReplaceAll(a, ":", "_")] = true
	}
	sys, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	defer sys.Close()
	sess, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer sess.Close()
	sigs := make(chan *dbus.Signal, 16)
	sys.Signal(sigs)
	if err := desktop.Watch(sys); err != nil {
		return err
	}

	sw := &callswitch.Switcher{
		Env:    desktop.System{Session: sess, Names: names, Skip: desktop.Softphones},
		Log:    logger.Printf,
		Settle: func() { time.Sleep(2 * time.Second) },
	}
	logger.Printf("switching %s to the phone profile during calls", strings.Join(addrs, ", "))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var end <-chan time.Time
	taken := "" // headset that took the call, if known
	check := func(final bool) {
		in, route, err := desktop.InCall(sys)
		if err != nil {
			logger.Printf("call state: %v", err)
			return
		}
		switch {
		case in:
			end = nil
			if !sw.InCall() {
				logger.Print("call")
			}
			if route.Device != "" && route.Device != taken {
				logger.Printf("call taken on %s", route.Device)
			}
			taken = route.Device
			sw.Call(true, route)
		case !sw.InCall():
		case final:
			logger.Print("call ended")
			sw.Call(false, callswitch.Route{})
		case end == nil:
			end = time.After(endDelay)
		}
	}
	check(false)
	for {
		select {
		case <-ctx.Done():
			sw.Call(false, callswitch.Route{})
			return nil
		case _, ok := <-sigs:
			if !ok {
				sw.Call(false, callswitch.Route{})
				return errors.New("system bus closed")
			}
			check(false)
		case <-end:
			end = nil
			check(true)
		}
	}
}
