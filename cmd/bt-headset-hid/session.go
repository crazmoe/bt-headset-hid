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
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/callswitch"
	"github.com/crazmoe/bt-headset-hid/internal/desktop"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
)

// endDelay is how long no call must be going on before the headsets switch
// back, so that a short gap in the call state does not switch them twice.
const endDelay = 2 * time.Second

// adoptDelay is how long BlueZ must be quiet before a newly paired headset
// is set up.
const adoptDelay = 3 * time.Second

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
	names := map[string]bool{}
	for _, a := range addrs {
		names[cardName(a)] = true
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
	if err := watchPairing(sys); err != nil {
		return err
	}

	sw := &callswitch.Switcher{
		Env:    desktop.System{Session: sess, Names: names, Skip: desktop.Softphones},
		Log:    logger.Printf,
		Settle: func() { time.Sleep(2 * time.Second) },
	}
	// With -profile phone the headsets stay in the phone profile, and this
	// only sets up headsets paired later.
	phone := wpconf.ConfiguredPhone(dir)
	switch {
	case len(addrs) == 0:
		logger.Print("no headset set up yet, waiting for one to be paired")
	case phone:
		logger.Printf("%s stay in the phone profile", strings.Join(addrs, ", "))
	default:
		logger.Printf("switching %s to the phone profile during calls", strings.Join(addrs, ", "))
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var end <-chan time.Time
	taken := "" // headset that took the call, if known
	check := func(final bool) {
		if phone {
			return
		}
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
	// Newly paired headsets are set up a moment after the last change in
	// BlueZ, once their profiles are known, and not during a call, since
	// that restarts WirePlumber.
	var adopt <-chan time.Time
	adoptPending := true
	check(false)
	for {
		if adoptPending && !sw.InCall() && adopt == nil {
			adopt = time.After(adoptDelay)
		}
		select {
		case <-ctx.Done():
			sw.Call(false, callswitch.Route{})
			return nil
		case sig, ok := <-sigs:
			if !ok {
				sw.Call(false, callswitch.Route{})
				return errors.New("system bus closed")
			}
			if pairingChanged(sig) {
				adoptPending, adopt = true, nil
				continue
			}
			check(false)
		case <-end:
			end = nil
			check(true)
		case <-adopt:
			adopt = nil
			if sw.InCall() {
				continue
			}
			adoptPending = false
			for _, a := range adoptHeadsets(sys, dir, logger) {
				names[cardName(a)] = true
			}
		}
	}
}

// cardName is PipeWire's device.name of the headset with Bluetooth address
// addr.
func cardName(addr string) string {
	return "bluez_card." + strings.ReplaceAll(strings.ToUpper(addr), ":", "_")
}

// watchPairing subscribes conn to BlueZ's signals about new and changed
// devices, which pairingChanged recognizes.
func watchPairing(conn *dbus.Conn) error {
	err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesAdded"),
		dbus.WithMatchSender("org.bluez"),
	)
	if err != nil {
		return err
	}
	return conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchSender("org.bluez"),
		dbus.WithMatchArg(0, "org.bluez.Device1"),
	)
}

// pairingChanged reports whether sig says a Bluetooth device appeared or was
// paired, or that its profiles changed.
func pairingChanged(sig *dbus.Signal) bool {
	switch sig.Name {
	case "org.freedesktop.DBus.ObjectManager.InterfacesAdded":
		if len(sig.Body) < 2 {
			return false
		}
		ifaces, _ := sig.Body[1].(map[string]map[string]dbus.Variant)
		_, ok := ifaces["org.bluez.Device1"]
		return ok
	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		if len(sig.Body) < 2 {
			return false
		}
		if iface, _ := sig.Body[0].(string); iface != "org.bluez.Device1" {
			return false
		}
		changed, _ := sig.Body[1].(map[string]dbus.Variant)
		_, paired := changed["Paired"]
		_, uuids := changed["UUIDs"]
		return paired || uuids
	}
	return false
}

// adoptHeadsets sets up the headsets paired since the headsets were last set
// up, restarts WirePlumber for them and returns them. Without a list of the
// headsets known before, from an older version, it only writes that list.
func adoptHeadsets(sys *dbus.Conn, dir string, logger *log.Logger) []string {
	hs, err := bluez.Headsets(sys)
	if err != nil {
		logger.Printf("paired headsets: %v", err)
		return nil
	}
	paired := addresses(hs)
	configured := wpconf.ConfiguredHeadsets(dir)
	known, ok := wpconf.Known(dir)
	add := wpconf.NewHeadsets(paired, configured, known, ok)
	if !ok {
		if err := wpconf.SetKnown(dir, append(paired, configured...)); err != nil {
			logger.Printf("known headsets: %v", err)
		}
		return nil
	}
	if len(add) == 0 {
		return nil
	}
	out, err := exec.Command("wireplumber", "--version").CombinedOutput()
	if err != nil {
		logger.Printf("set up %s: wireplumber --version: %v", strings.Join(add, ", "), err)
		return nil
	}
	v, err := wpconf.ParseVersion(string(out))
	if err != nil {
		logger.Printf("set up %s: %v", strings.Join(add, ", "), err)
		return nil
	}
	if _, err := wpconf.InstallHeadset(dir, append(configured, add...), v, wpconf.ConfiguredPhone(dir)); err != nil {
		logger.Printf("set up %s: %v", strings.Join(add, ", "), err)
		return nil
	}
	if err := wpconf.SetKnown(dir, append(known, add...)); err != nil {
		logger.Printf("known headsets: %v", err)
	}
	logger.Printf("newly paired headset %s set up; to undo: bt-headset-hid wireplumber -headset %s",
		strings.Join(add, ", "), strings.Join(configured, ","))
	if out, err := exec.Command("systemctl", "--user", "restart", "wireplumber").CombinedOutput(); err != nil {
		logger.Printf("systemctl --user restart wireplumber: %v: %s", err, out)
	}
	return add
}
