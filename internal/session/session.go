// Package session is "bt-headset-hid session", the user service: it shows
// the tray icon and checks the setup now and then; in modes pipewire and
// ofono, during a call it also pauses the media players and switches the
// headsets from music (A2DP) to the phone profile, and back afterwards. It
// sets up headsets paired later and follows the call audio to the headset
// that is connected.
package session

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/callswitch"
	"github.com/crazmoe/bt-headset-hid/internal/desktop"
	"github.com/crazmoe/bt-headset-hid/internal/doctor"
	"github.com/crazmoe/bt-headset-hid/internal/i18n"
	"github.com/crazmoe/bt-headset-hid/internal/install"
	"github.com/crazmoe/bt-headset-hid/internal/tray"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
	"github.com/crazmoe/bt-headset-hid/internal/wpsetup"
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

// trayDelay is how long things must be quiet before the tray icon is
// updated.
const trayDelay = 200 * time.Millisecond

// Run runs the session service until ctx is done; version is shown in the
// tray menu.
func Run(ctx context.Context, version string, logger *log.Logger) error {
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
	de := i18n.German(os.Getenv)
	notify := func(m i18n.Message) {
		summary, body := m.Pick(de)
		if err := desktop.Notify(sess, summary, body); err != nil {
			logger.Printf("notification: %v", err)
		}
	}
	unit, _ := os.ReadFile(install.Unit)
	mode := setUpUser(unit, dir, logger, notify, func(o wpsetup.Options) error {
		e, err := wpsetup.RealEnv()
		if err == nil {
			_, err = wpsetup.Setup(o, e)
		}
		return err
	})
	if mode == "" {
		return nil
	}
	// In mode hfp there is no call audio to handle, only the tray icon and
	// the checks.
	audio := mode != "hfp"
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
	if audio {
		if err := desktop.Watch(sys); err != nil {
			return err
		}
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
	case !audio:
	case len(addrs) == 0:
		logger.Print("no headset set up yet, waiting for one to be paired")
	case phone:
		logger.Printf("%s stay in the phone profile", strings.Join(addrs, ", "))
	default:
		logger.Printf("switching %s to the phone profile during calls", strings.Join(addrs, ", "))
	}
	// The speaker and microphone "bt-headset-hid" follow the headset
	// connected, checked a moment after PipeWire's objects changed.
	pwChanged := make(chan struct{}, 1)
	if audio {
		go desktop.Monitor(ctx, pwChanged, logger.Printf)
	}
	var routeAt <-chan time.Time
	routeSoon := func() {
		if routeAt == nil {
			routeAt = time.After(routeDelay)
		}
	}

	var end <-chan time.Time
	taken := "" // headset that took the call, if known
	check := func(final bool) {
		if !audio {
			return
		}
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
	adoptPending := audio
	// Problems are checked now and then, each shown once.
	healthAt := time.After(healthDelay)
	health := make(chan []doctor.Finding, 1)
	var problems map[string]bool
	var failures []doctor.Finding
	checkingNow := false // "Check now" in the tray menu: say the result

	// The tray icon shows the headsets, calls and problems; its menu
	// chooses the headsets for calls.
	clicks := make(chan int32, 8)
	var view tray.View
	var trayAt <-chan time.Time
	traySoon := func() {
		if trayAt == nil {
			trayAt = time.After(trayDelay)
		}
	}
	icon, err := desktop.NewTray(sess, func(id int32) {
		select {
		case clicks <- id:
		default:
		}
	})
	if err != nil {
		logger.Printf("tray icon: %v", err)
	}
	var warnedLow map[string]bool // headsets warned about a low battery
	showTray := func() {
		if icon == nil {
			return
		}
		var paired []tray.Headset
		hs, err := bluez.Headsets(sys)
		if err != nil {
			logger.Printf("paired headsets: %v", err)
		}
		// BlueZ shows the battery level if it took it from the daemon;
		// otherwise the daemon publishes it itself (mode ofono).
		var own map[string]int
		if mode == "ofono" {
			own = desktop.Batteries(sys)
		}
		for _, h := range hs {
			th := tray.Headset{Address: h.Address, Name: h.Name, Connected: h.Connected}
			if p, ok := own[strings.ToUpper(h.Address)]; ok && h.Battery < 0 {
				h.Battery = p
			}
			if h.Battery >= 0 && h.Connected {
				th.Battery, th.BatteryKnown = h.Battery, true
			}
			paired = append(paired, th)
		}
		headsets := tray.Headsets(paired, addrs)
		var low []tray.Headset
		low, warnedLow = tray.LowBatteries(headsets, warnedLow)
		for _, h := range low {
			name := h.Name
			if name == "" {
				name = h.Address
			}
			notify(i18n.New(
				"Akku fast leer", fmt.Sprintf("%s: noch %d %%", name, h.Battery),
				"Battery low", fmt.Sprintf("%s: %d %% left", name, h.Battery),
			))
		}
		view = tray.Show(tray.State{
			Version:  version,
			Mode:     mode,
			Headsets: headsets,
			InCall:   sw.InCall() || taken != "",
			Problems: failures,
			German:   de,
		})
		icon.Show(view)
	}
	showTray()
	check(false)
	for {
		if adoptPending && adopt == nil {
			adopt = time.After(adoptDelay)
		}
		select {
		case <-trayAt:
			trayAt = nil
			showTray()
		case id := <-clicks:
			it, ok := tray.Find(view.Menu, id)
			if !ok {
				continue
			}
			switch it.Action {
			case tray.ToggleHeadset:
				if err := toggleHeadset(dir, it.Arg, logger); err == nil {
					setAddrs(wpconf.ConfiguredHeadsets(dir))
					routeSoon()
				}
				traySoon()
			case tray.ShowProblem:
				var i int
				if _, err := fmt.Sscan(it.Arg, &i); err == nil && i < len(failures) {
					notify(problemMessage(failures[i]))
				}
			case tray.CheckNow:
				if !checkingNow {
					checkingNow = true
					healthAt = nil
					go checkHealth(health, de)
				}
			case tray.Help:
				openURL(tray.HelpURL(de), logger)
			}
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
			traySoon()
			if dev, ok := unpaired(sig); ok && audio {
				// A device of an adapter that went away is not unpaired.
				if _, err := bluez.LocalAddress(sys, dev); err == nil {
					if dropHeadset(dir, bluez.AddressFromPath(dev), logger) != "" {
						setAddrs(wpconf.ConfiguredHeadsets(dir))
						routeSoon()
					}
				}
				continue
			}
			if pairingChanged(sig) && audio {
				adoptPending, adopt = true, nil
				continue
			}
			check(false)
		case <-end:
			end = nil
			check(true)
			traySoon()
		case <-adopt:
			adopt = nil
			adoptPending = false
			hs, err := bluez.Headsets(sys)
			if err != nil {
				logger.Printf("paired headsets: %v", err)
				continue
			}
			if added := adoptHeadsets(hs, dir, logger); len(added) > 0 {
				setAddrs(wpconf.ConfiguredHeadsets(dir))
				routeSoon()
				traySoon()
				notify(i18n.New(
					"Headset eingerichtet", strings.Join(added, ", ")+" ist bereit für Anrufe über „bt-headset-hid“.",
					"Headset set up", strings.Join(added, ", ")+` is ready for calls through "bt-headset-hid".`,
				))
			}
		case <-healthAt:
			healthAt = nil
			go checkHealth(health, de)
		case fs := <-health:
			healthAt = time.After(healthInterval)
			var fresh []doctor.Finding
			fresh, problems = doctor.NewFailures(problems, fs)
			failures = tray.Failures(fs)
			traySoon()
			for _, f := range fresh {
				logger.Printf("problem: %s; fix: %s", f.Text, f.Fix)
				notify(problemMessage(f))
			}
			if checkingNow {
				checkingNow = false
				switch {
				case len(failures) == 0:
					notify(i18n.New("bt-headset-hid: alles in Ordnung", "", "bt-headset-hid: all fine", ""))
				case len(fresh) == 0:
					// Shown before; show them again, since the user asked.
					for _, f := range failures {
						notify(problemMessage(f))
					}
				}
			}
		}
	}
}
