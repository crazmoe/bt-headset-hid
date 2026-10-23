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

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/callswitch"
	"github.com/crazmoe/bt-headset-hid/internal/desktop"
	"github.com/crazmoe/bt-headset-hid/internal/doctor"
	"github.com/crazmoe/bt-headset-hid/internal/install"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
)

// endDelay is how long no call must be going on before the headsets switch
// back, so that a short gap in the call state does not switch them twice.
const endDelay = 2 * time.Second

// adoptDelay is how long BlueZ must be quiet before a newly paired headset
// is set up.
const adoptDelay = 3 * time.Second

// healthDelay is how long after the start the setup is first checked, and
// healthInterval how often after that; each new problem is shown as a
// desktop notification.
const (
	healthDelay    = 30 * time.Second
	healthInterval = 30 * time.Minute
)

// routeDelay is how long PipeWire's objects must be quiet before the speaker
// and microphone "bt-headset-hid" are pointed at the headset again.
const routeDelay = 300 * time.Millisecond

// runSession implements "bt-headset-hid session", which runs in the user's
// session in modes pipewire and ofono: during a call it pauses the media players and
// switches the headsets set up with "bt-headset-hid wireplumber" from music
// (A2DP) to the phone profile, and back afterwards.
func runSession(args []string) error {
	fs := flag.NewFlagSet("session", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bt-headset-hid session")
		fmt.Fprintln(fs.Output(), `Runs in your session (systemd user service "bt-headset-hid-session", set up by the package or "bt-headset-hid wireplumber"). It sets up your PipeWire if needed.`)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	logger := log.New(os.Stderr, "", 0)
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	if wpconf.Off(dir) {
		logger.Print(`removed with "bt-headset-hid wireplumber -remove"; to set it up again: bt-headset-hid wireplumber`)
		return nil
	}
	sess, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer sess.Close()
	notify := func(m message) {
		summary, body := m.pick(german(os.Getenv))
		if err := desktop.Notify(sess, summary, body); err != nil {
			logger.Printf("notification: %v", err)
		}
	}
	if more, err := setUpUser(dir, logger, notify); !more || err != nil {
		return err
	}
	addrs := wpconf.ConfiguredHeadsets(dir)
	names := map[string]bool{}
	setAddrs := func(as []string) {
		addrs = as
		for n := range names {
			delete(names, n)
		}
		for _, a := range as {
			names[cardName(a)] = true
		}
	}
	setAddrs(addrs)
	sys, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	defer sys.Close()
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
	// does not switch them for calls.
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

	// The speaker and microphone "bt-headset-hid" follow the headset
	// connected, checked a moment after PipeWire's objects changed.
	pwChanged := make(chan struct{}, 1)
	go desktop.Monitor(ctx, pwChanged, logger.Printf)
	var routeAt <-chan time.Time
	routeSoon := func() {
		if routeAt == nil {
			routeAt = time.After(routeDelay)
		}
	}

	var end <-chan time.Time
	taken := "" // headset that took the call, if known
	check := func(final bool) {
		in, route, err := desktop.InCall(sys)
		if err != nil {
			logger.Printf("call state: %v", err)
			return
		}
		if !in {
			route = callswitch.Route{}
		}
		if route.Device != taken {
			if route.Device != "" {
				logger.Printf("call taken on %s", route.Device)
			}
			taken = route.Device
			routeSoon()
		}
		if phone {
			return
		}
		switch {
		case in:
			end = nil
			if !sw.InCall() {
				logger.Print("call")
			}
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
	// BlueZ, once their profiles are known.
	var adopt <-chan time.Time
	adoptPending := true
	// Problems are checked now and then, each shown once.
	healthAt := time.After(healthDelay)
	health := make(chan []doctor.Finding, 1)
	var problems map[string]bool
	check(false)
	for {
		if adoptPending && adopt == nil {
			adopt = time.After(adoptDelay)
		}
		select {
		case <-pwChanged:
			routeSoon()
		case <-routeAt:
			routeAt = nil
			routeAudio(addrs, taken, phone, sw.Env, logger)
		case <-ctx.Done():
			sw.Call(false, callswitch.Route{})
			return nil
		case sig, ok := <-sigs:
			if !ok {
				sw.Call(false, callswitch.Route{})
				return errors.New("system bus closed")
			}
			if dev, ok := unpaired(sig); ok {
				// A device of an adapter that went away is not unpaired.
				if _, err := bluez.LocalAddress(sys, dev); err == nil {
					if dropHeadset(dir, bluez.AddressFromPath(dev), logger) != "" {
						setAddrs(wpconf.ConfiguredHeadsets(dir))
						routeSoon()
					}
				}
				continue
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
			adoptPending = false
			if added := adoptHeadsets(sys, dir, logger); len(added) > 0 {
				setAddrs(wpconf.ConfiguredHeadsets(dir))
				routeSoon()
				notify(message{
					"Headset eingerichtet", strings.Join(added, ", ") + " ist bereit für Anrufe über „bt-headset-hid“.",
					"Headset set up", strings.Join(added, ", ") + ` is ready for calls through "bt-headset-hid".`,
				})
			}
		case <-healthAt:
			healthAt = nil
			go checkHealth(health)
		case fs := <-health:
			healthAt = time.After(healthInterval)
			var fresh []doctor.Finding
			fresh, problems = doctor.NewFailures(problems, fs)
			for _, f := range fresh {
				logger.Printf("problem: %s; fix: %s", f.Text, f.Fix)
				m := message{"bt-headset-hid: " + f.Text, "Details: bt-headset-hid doctor", "bt-headset-hid: " + f.Text, "Details: bt-headset-hid doctor"}
				if f.Fix != "" {
					m.bodyDE = "Lösung: " + f.Fix + "\n" + m.bodyDE
					m.bodyEN = "Fix: " + f.Fix + "\n" + m.bodyEN
				}
				notify(m)
			}
		}
	}
}

// message is a desktop notification in German and English.
type message struct {
	summaryDE, bodyDE string
	summaryEN, bodyEN string
}

func (m message) pick(de bool) (summary, body string) {
	if de {
		return m.summaryDE, m.bodyDE
	}
	return m.summaryEN, m.bodyEN
}

// setUpUser sets up the user's PipeWire for the mode the daemon runs in,
// if it is not yet, as after installing the package. It reports whether
// the session service has more to do: not in mode hfp, nor without the
// daemon.
func setUpUser(dir string, logger *log.Logger, notify func(message)) (bool, error) {
	unit, err := os.ReadFile(install.Unit)
	mode := ""
	if err == nil {
		mode = doctor.ModeFromUnit(unit)
	}
	if mode == "" {
		logger.Print("bt-headset-hid is not installed; to install it: sudo bt-headset-hid install")
		return false, nil
	}
	if wpconf.ConfiguredMode(dir) != mode {
		logger.Printf("setting up PipeWire for mode %s", mode)
		if _, err := setUpWirePlumber(wpOptions{mode: mode, fromSession: true}); err != nil {
			notify(message{
				"bt-headset-hid: Einrichtung fehlgeschlagen", "Im Terminal ausführen: bt-headset-hid doctor",
				"bt-headset-hid: setup failed", "Run in a terminal: bt-headset-hid doctor",
			})
			// Starting again would fail again; the user fixes it first.
			logger.Printf("set up PipeWire: %v", err)
			return false, nil
		}
		if mode == "hfp" {
			notify(message{
				"bt-headset-hid ist eingerichtet", "Wähle im Softphone „bt-headset-hid“ als Telefongerät.",
				"bt-headset-hid is set up", `Choose "bt-headset-hid" as the phone device in the softphone.`,
			})
		} else {
			notify(message{
				"bt-headset-hid ist eingerichtet", "Wähle im Softphone „bt-headset-hid“ als Telefongerät, Lautsprecher und Mikrofon.",
				"bt-headset-hid is set up", `Choose "bt-headset-hid" as the phone device, speaker and microphone in the softphone.`,
			})
		}
	}
	return mode != "hfp", nil
}

// checkHealth runs the checks of "bt-headset-hid doctor" and sends the
// findings on found.
func checkHealth(found chan<- []doctor.Finding) {
	found <- doctor.Check(gatherFacts(), german(os.Getenv))
}

// cardName is PipeWire's device.name of the headset with Bluetooth address
// addr.
func cardName(addr string) string {
	return "bluez_card." + strings.ReplaceAll(strings.ToUpper(addr), ":", "_")
}

// watchPairing subscribes conn to BlueZ's signals about new, changed and
// removed devices, which pairingChanged and unpaired recognize.
func watchPairing(conn *dbus.Conn) error {
	err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesAdded"),
		dbus.WithMatchSender("org.bluez"),
	)
	if err != nil {
		return err
	}
	err = conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesRemoved"),
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

// adoptHeadsets adds the headsets paired since the headsets were last set up
// to the headset list and returns their names. Without a list of the headsets known
// before, from an older version, it only writes that list.
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
	if err := wpconf.SetHeadsets(dir, append(configured, add...), wpconf.ConfiguredPhone(dir)); err != nil {
		logger.Printf("set up %s: %v", strings.Join(add, ", "), err)
		return nil
	}
	if err := wpconf.SetKnown(dir, append(known, add...)); err != nil {
		logger.Printf("known headsets: %v", err)
	}
	logger.Printf("newly paired headset %s set up; to undo: bt-headset-hid wireplumber -headset %s",
		strings.Join(add, ", "), strings.Join(configured, ","))
	var names []string
	for _, a := range add {
		names = append(names, headsetName(hs, a))
	}
	return names
}

// headsetName returns the name of the headset addr in hs, with the
// address, or the address alone.
func headsetName(hs []bluez.Headset, addr string) string {
	for _, h := range hs {
		if strings.EqualFold(h.Address, addr) && h.Name != "" {
			return h.Name + " (" + h.Address + ")"
		}
	}
	return addr
}

// unpaired returns the device path if sig says BlueZ removed a device, as it
// does when the device is unpaired (or its adapter goes away).
func unpaired(sig *dbus.Signal) (dbus.ObjectPath, bool) {
	if sig.Name != "org.freedesktop.DBus.ObjectManager.InterfacesRemoved" || len(sig.Body) < 2 {
		return "", false
	}
	path, _ := sig.Body[0].(dbus.ObjectPath)
	ifaces, _ := sig.Body[1].([]string)
	for _, i := range ifaces {
		if i == "org.bluez.Device1" && bluez.AddressFromPath(path) != "" {
			return path, true
		}
	}
	return "", false
}

// dropHeadset takes the unpaired headset addr off the headset list and
// returns it, or "" if it was not set up. It stays on the known list, so
// pairing it again does not set it up again.
func dropHeadset(dir, addr string, logger *log.Logger) string {
	configured := wpconf.ConfiguredHeadsets(dir)
	keep := droppedHeadsets(configured, []string{addr})
	if len(keep) == len(configured) {
		return ""
	}
	known, _ := wpconf.Known(dir)
	if err := wpconf.SetKnown(dir, append(known, addr)); err != nil {
		logger.Printf("known headsets: %v", err)
	}
	if err := wpconf.SetHeadsets(dir, keep, wpconf.ConfiguredPhone(dir)); err != nil {
		logger.Printf("unpaired headset %s: %v", addr, err)
		return ""
	}
	logger.Printf("unpaired headset %s no longer set up", addr)
	return addr
}

// routeAudio points the speaker and microphone "bt-headset-hid" at the call
// audio of the headset in addrs that is connected, preferring device, the
// one that took the call, and with phone keeps the connected headsets in the
// phone profile.
func routeAudio(addrs []string, device string, phone bool, env callswitch.Env, logger *log.Logger) {
	if phone {
		cards, err := env.Cards()
		if err != nil {
			logger.Printf("headsets: %v", err)
		}
		for _, c := range cards {
			p := c.PhoneProfile()
			if callswitch.IsPhone(c.Profile) || p == "" {
				continue
			}
			if err := env.SetProfile(c, p); err != nil {
				logger.Printf("%s: profile %s: %v", c.Name, p, err)
				continue
			}
			logger.Printf("%s: %s -> %s", c.Name, c.Profile, p)
		}
	}
	r, err := desktop.Routing()
	if err != nil {
		logger.Printf("routing: %v", err)
		return
	}
	ch := callswitch.RouteFor(r, addrs, device)
	for _, c := range ch {
		if err := desktop.SetTarget(c); err != nil {
			logger.Printf("routing: %v", err)
			return
		}
	}
	if len(ch) > 0 {
		logger.Printf("bt-headset-hid: %s", callswitch.Describe(ch, r))
	}
}
