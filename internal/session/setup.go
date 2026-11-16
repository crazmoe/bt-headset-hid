package session

import (
	"log"
	"os/exec"

	"github.com/crazmoe/bt-headset-hid/internal/doctor"
	"github.com/crazmoe/bt-headset-hid/internal/i18n"
	"github.com/crazmoe/bt-headset-hid/internal/sysfacts"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
	"github.com/crazmoe/bt-headset-hid/internal/wpsetup"
)

// setUpUser sets up the user's PipeWire with setup for the mode the daemon
// runs in, read from its unit, if it is not yet, as after installing the
// package. It returns the mode, or "" if the session service has nothing to
// do: without the daemon, or when the setup failed.
func setUpUser(unit []byte, dir string, logger *log.Logger, notify func(i18n.Message), setup func(wpsetup.Options) error) string {
	mode := doctor.ModeFromUnit(unit)
	if mode == "" {
		logger.Print("bt-headset-hid is not installed; to install it: sudo bt-headset-hid install")
		return ""
	}
	if wpconf.ConfiguredMode(dir) != mode {
		logger.Printf("setting up PipeWire for mode %s", mode)
		if err := setup(wpsetup.Options{Mode: mode, FromSession: true}); err != nil {
			notify(i18n.New(
				"bt-headset-hid: Einrichtung fehlgeschlagen", "Im Terminal ausführen: bt-headset-hid doctor",
				"bt-headset-hid: setup failed", "Run in a terminal: bt-headset-hid doctor",
			))
			// Starting again would fail again; the user fixes it first.
			logger.Printf("set up PipeWire: %v", err)
			return ""
		}
		if mode == "hfp" {
			notify(i18n.New(
				"bt-headset-hid ist eingerichtet", "Wähle im Softphone „bt-headset-hid“ als Telefongerät.",
				"bt-headset-hid is set up", `Choose "bt-headset-hid" as the phone device in the softphone.`,
			))
		} else {
			notify(i18n.New(
				"bt-headset-hid ist eingerichtet", "Wähle im Softphone „bt-headset-hid“ als Telefongerät, Lautsprecher und Mikrofon.",
				"bt-headset-hid is set up", `Choose "bt-headset-hid" as the phone device, speaker and microphone in the softphone.`,
			))
		}
	}
	return mode
}

// problemMessage is the notification about the problem f.
func problemMessage(f doctor.Finding) i18n.Message {
	m := i18n.New("bt-headset-hid: "+f.Text, "Details: bt-headset-hid doctor", "bt-headset-hid: "+f.Text, "Details: bt-headset-hid doctor")
	if f.Fix != "" {
		m.BodyDE = "Lösung: " + f.Fix + "\n" + m.BodyDE
		m.BodyEN = "Fix: " + f.Fix + "\n" + m.BodyEN
	}
	return m
}

// openURL opens url in the browser.
func openURL(url string, logger *log.Logger) {
	cmd := exec.Command("xdg-open", url)
	if err := cmd.Start(); err != nil {
		logger.Printf("open %s: %v", url, err)
		return
	}
	go cmd.Wait()
}

// checkHealth runs the checks of "bt-headset-hid doctor" and sends the
// findings on found.
func checkHealth(found chan<- []doctor.Finding, de bool) {
	found <- doctor.Check(sysfacts.Real().Facts(), de)
}
