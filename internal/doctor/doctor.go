// Package doctor checks a bt-headset-hid installation. Facts are gathered by
// the caller; Check turns them into findings, each with the command that
// fixes it, in English or German. It does no I/O, so it can be tested.
package doctor

import (
	"fmt"
	"slices"
	"strings"

	"github.com/crazmoe/bt-headset-hid/internal/install"
)

// Level says how bad a finding is.
type Level int

const (
	OK   Level = iota // works
	Info              // worth knowing, nothing to do
	Warn              // may not work
	Fail              // does not work
)

// Finding is one result of Check.
type Finding struct {
	Level Level
	Text  string
	Fix   string // command or step that fixes it, if any
}

// State is the state of a systemd unit or another program: "" if unknown.
type State string

const (
	Active   State = "active"
	Inactive State = "inactive"
)

// Headset is a paired headset.
type Headset struct {
	Address   string
	Name      string
	Connected bool
}

// Facts describe the system, as far as the caller could find out.
type Facts struct {
	Root bool // doctor runs as root, so the user's session is not seen

	// The system service: Mode is read from its unit, "" if not installed.
	Mode    string
	Service State
	// UnitProgram is the program the unit starts, symlinks resolved.
	UnitProgram string
	// Packaged: the program of the deb or rpm package is installed.
	Packaged bool
	// Programs are the programs bt-headset-hid in the order the shell finds
	// them on PATH, symlinks resolved, each once.
	Programs    []string
	Log         []string // lines of its log since it last started
	LogReadable bool     // the log could be read

	UHID       bool   // /dev/uhid exists
	UdevRule   bool   // the udev rule is installed
	DBusPolicy []byte // installed policy, nil if missing
	HIDDevice  bool   // a hidraw device of bt-headset-hid exists
	HIDAccess  bool   // the user can open it

	Bluetooth State // bluetooth.service
	Adapter   int   // -1 unknown, 0 none, 1 off, 2 on
	Paired    []Headset
	BlueZErr  error

	ModemManager, OFono, Hsphfpd State

	PipeWireVersion string // output of "pipewire --version"
	PipeWire        State  // user's pipewire.service
	WirePlumber     State  // user's wireplumber.service
	PulseAudio      State  // user's pulseaudio.service
	OFonoBackend    int    // -1 unknown, 0 PipeWire lacks it, 1 has it

	WPMode     string   // mode of the user's WirePlumber configuration, "" if none
	Configured []string // headsets set up in the user's configuration
	OldRules   bool     // WirePlumber rules for every headset from an older version
	Session    State    // user's bt-headset-hid-session.service
}

// Check returns the findings for f, in German if de.
func Check(f Facts, de bool) []Finding {
	c := checker{de: de}
	c.service(f)
	c.copies(f)
	if f.Mode == "" {
		return c.out
	}
	c.files(f)
	c.bluetooth(f)
	c.conflicts(f)
	c.log(f)
	if f.Root {
		c.add(Info, "Als root gestartet: die Einstellungen deines Benutzers wurden nicht geprüft",
			"Started as root: your user's settings were not checked", "bt-headset-hid doctor")
		return c.out
	}
	c.audio(f)
	return c.out
}

type checker struct {
	de  bool
	out []Finding
}

func (c *checker) add(l Level, de, en, fix string) {
	c.out = append(c.out, Finding{Level: l, Text: c.t(de, en), Fix: fix})
}

func (c *checker) t(de, en string) string {
	if c.de {
		return de
	}
	return en
}

func (c *checker) service(f Facts) {
	if f.Mode == "" {
		c.add(Fail, "bt-headset-hid ist nicht eingerichtet", "bt-headset-hid is not set up", "sudo bt-headset-hid install")
		return
	}
	switch f.Service {
	case Active:
		c.add(OK, "Dienst läuft, Modus "+f.Mode, "service running, mode "+f.Mode, "")
	case "":
		c.add(Warn, "Zustand des Dienstes unbekannt", "state of the service unknown", "systemctl status bt-headset-hid")
	default:
		c.add(Fail, "Dienst läuft nicht ("+string(f.Service)+")", "service not running ("+string(f.Service)+")",
			"sudo systemctl enable --now bt-headset-hid")
	}
}

// copies finds an older copy of the program, e.g. in /usr/local/bin from
// "install" before the package, that the shell or the service runs
// instead of the package's.
func (c *checker) copies(f Facts) {
	if !f.Packaged {
		return
	}
	var old []string
	for _, p := range f.Programs {
		if p == install.PackagedBinary {
			break
		}
		old = append(old, p)
	}
	unitOld := f.UnitProgram != "" && f.UnitProgram != install.PackagedBinary
	if unitOld && !slices.Contains(old, f.UnitProgram) {
		old = append(old, f.UnitProgram)
	}
	if len(old) == 0 {
		return
	}
	fix := "sudo rm " + strings.Join(old, " ") + " && hash -r"
	if unitOld {
		fix += " && sudo bt-headset-hid install"
	}
	list := strings.Join(old, ", ")
	if unitOld {
		c.add(Fail, "der Dienst startet eine ältere Kopie ("+list+") statt des Pakets",
			"the service starts an older copy ("+list+") instead of the package", fix)
		return
	}
	c.add(Fail, "eine ältere Kopie ("+list+") verdeckt das Paket, bt-headset-hid-Befehle nehmen sie",
		"an older copy ("+list+") hides the package, bt-headset-hid commands run it", fix)
}

func (c *checker) files(f Facts) {
	if !f.UHID {
		c.add(Fail, "Kernelmodul uhid fehlt (/dev/uhid)", "kernel module uhid missing (/dev/uhid)", "sudo modprobe uhid")
	}
	if !f.UdevRule {
		c.add(Fail, "udev-Regel fehlt, Softphones dürfen das Gerät nicht öffnen",
			"udev rule missing, softphones may not open the device", "sudo bt-headset-hid install -mode "+f.Mode)
	}
	if want := install.Policy(f.Mode); want != nil && string(f.DBusPolicy) != string(want) {
		c.add(Fail, "D-Bus-Policy fehlt oder passt nicht zum Modus "+f.Mode,
			"D-Bus policy missing or not for mode "+f.Mode, "sudo bt-headset-hid install -mode "+f.Mode)
	}
	if f.Service == Active {
		switch {
		case !f.HIDDevice:
			c.add(Fail, "kein HID-Gerät bt-headset-hid vorhanden", "no HID device bt-headset-hid",
				"journalctl -u bt-headset-hid -b")
		case !f.HIDAccess && !f.Root:
			c.add(Warn, "HID-Gerät vorhanden, aber du darfst es nicht öffnen (Softphones auch nicht)",
				"HID device present, but you may not open it (nor may softphones)",
				c.t("sudo udevadm trigger --subsystem-match=hidraw, dann ab- und wieder anmelden", "sudo udevadm trigger --subsystem-match=hidraw, then log out and in again"))
		default:
			c.add(OK, "HID-Gerät bt-headset-hid vorhanden", "HID device bt-headset-hid present", "")
		}
	}
}

func (c *checker) bluetooth(f Facts) {
	if f.Bluetooth != Active && f.Bluetooth != "" {
		c.add(Fail, "Bluetooth-Dienst läuft nicht", "Bluetooth service not running", "sudo systemctl enable --now bluetooth")
		return
	}
	switch f.Adapter {
	case 0:
		c.add(Fail, "kein Bluetooth-Adapter gefunden", "no Bluetooth adapter found", "rfkill list")
		return
	case 1:
		c.add(Fail, "Bluetooth ist ausgeschaltet", "Bluetooth is switched off", "bluetoothctl power on")
		return
	}
	if f.BlueZErr != nil {
		c.add(Warn, fmt.Sprintf("BlueZ nicht erreichbar: %v", f.BlueZErr), fmt.Sprintf("cannot reach BlueZ: %v", f.BlueZErr), "")
		return
	}
	if len(f.Paired) == 0 {
		c.add(Fail, "kein Headset gekoppelt", "no headset paired", c.t("in den Bluetooth-Einstellungen des Desktops koppeln", "pair it in the desktop's Bluetooth settings"))
		return
	}
	var conn []string
	for _, h := range f.Paired {
		if h.Connected {
			conn = append(conn, h.Name)
		}
	}
	if len(conn) == 0 {
		c.add(Warn, "kein Headset verbunden", "no headset connected", "bluetoothctl connect "+f.Paired[0].Address)
	} else {
		c.add(OK, "verbunden: "+strings.Join(conn, ", "), "connected: "+strings.Join(conn, ", "), "")
	}
}

func (c *checker) conflicts(f Facts) {
	switch f.Mode {
	case "pipewire":
		if f.ModemManager == Active {
			c.add(Fail, "ModemManager läuft und belegt denselben D-Bus-Namen",
				"ModemManager is running and owns the same D-Bus name", "sudo bt-headset-hid install -mode pipewire")
		}
		if f.PipeWireVersion != "" {
			if err := install.CheckPipeWire(f.PipeWireVersion, ""); err != nil {
				c.add(Fail, "PipeWire zu alt für -mode pipewire: "+err.Error(), "PipeWire too old for -mode pipewire: "+err.Error(),
					"sudo bt-headset-hid install -mode ofono")
			}
		}
	case "ofono", "hfp":
		if f.OFono == Active {
			c.add(Fail, "oFono läuft und belegt das Headset", "oFono is running and takes the headset",
				"sudo systemctl disable --now ofono")
		}
		if f.Hsphfpd == Active {
			c.add(Fail, "hsphfpd läuft und belegt das Headset", "hsphfpd is running and takes the headset",
				"sudo systemctl disable --now hsphfpd")
		}
	}
}

// logHints maps lines of the daemon's log to findings.
var logHints = []struct {
	match, de, en, fix string
}{
	{"is already owned, is ModemManager running", "ModemManager belegt den D-Bus-Namen", "ModemManager owns the D-Bus name",
		"sudo bt-headset-hid install -mode ofono"},
	{"is already owned, is oFono running", "oFono belegt den D-Bus-Namen", "oFono owns the D-Bus name",
		"sudo systemctl disable --now ofono && sudo systemctl restart bt-headset-hid"},
	{"already provides the HFP/HSP Audio Gateway", "PipeWire belegt das Headset noch (WirePlumber-Einstellung nicht aktiv)",
		"PipeWire still takes the headset (WirePlumber setting not active)",
		"bt-headset-hid wireplumber && sudo systemctl restart bt-headset-hid"},
	{"open /dev/uhid", "/dev/uhid lässt sich nicht öffnen", "cannot open /dev/uhid", "sudo modprobe uhid && sudo systemctl restart bt-headset-hid"},
	{"no wideband audio link", "Headset oder Adapter schafft keine HD-Sprache, es wird normale Qualität genutzt",
		"headset or adapter cannot do HD voice, normal quality is used", ""},
}

func (c *checker) log(f Facts) {
	if !f.LogReadable {
		if f.Service == Active {
			c.add(Info, "Protokoll des Dienstes nicht lesbar, mit sudo mehr Details",
				"cannot read the service's log, more details with sudo", "sudo bt-headset-hid doctor")
		}
		return
	}
	seen := map[string]bool{}
	agent := false
	for _, l := range f.Log {
		if strings.Contains(l, "audio agent") && strings.Contains(l, " registered") {
			agent = true
		}
		for _, h := range logHints {
			if strings.Contains(l, h.match) && !seen[h.match] {
				seen[h.match] = true
				lvl := Fail
				if h.fix == "" {
					lvl = Info
				}
				c.add(lvl, h.de, h.en, h.fix)
			}
		}
	}
	if f.Mode == "ofono" && f.Service == Active && !agent && !seen["already provides the HFP/HSP Audio Gateway"] {
		c.add(Warn, "PipeWire holt den Gesprächston nicht ab (kein Audio-Agent angemeldet)",
			"PipeWire does not take the call audio (no audio agent registered)",
			"bt-headset-hid wireplumber -mode ofono && sudo systemctl restart bt-headset-hid")
	}
}

func (c *checker) audio(f Facts) {
	if f.PulseAudio == Active {
		c.add(Fail, "PulseAudio läuft statt PipeWire", "PulseAudio runs instead of PipeWire",
			c.t("PipeWire mit pipewire-pulse installieren", "install PipeWire with pipewire-pulse"))
	}
	if f.PipeWire != Active && f.PipeWire != "" {
		c.add(Fail, "PipeWire läuft nicht", "PipeWire is not running", "systemctl --user enable --now pipewire wireplumber")
	} else if f.WirePlumber != Active && f.WirePlumber != "" {
		c.add(Fail, "WirePlumber läuft nicht", "WirePlumber is not running", "systemctl --user enable --now wireplumber")
	}
	switch f.WPMode {
	case f.Mode:
		c.add(OK, "WirePlumber eingerichtet für Modus "+f.Mode, "WirePlumber set up for mode "+f.Mode, "")
	case "":
		c.add(Fail, "WirePlumber ist für deinen Benutzer nicht eingerichtet", "WirePlumber is not set up for your user",
			"bt-headset-hid wireplumber -mode "+f.Mode)
	default:
		c.add(Fail, "WirePlumber ist für Modus "+f.WPMode+" eingerichtet, der Dienst läuft im Modus "+f.Mode,
			"WirePlumber is set up for mode "+f.WPMode+", the service runs in mode "+f.Mode,
			"bt-headset-hid wireplumber -mode "+f.Mode)
	}
	if f.Mode == "ofono" && f.OFonoBackend == 0 {
		c.add(Fail, "dein PipeWire ist ohne oFono-Unterstützung gebaut, kein Gesprächston",
			"your PipeWire is built without oFono support, no call audio", "sudo bt-headset-hid install -mode hfp")
	}
	if f.Mode == "hfp" {
		return
	}
	if len(f.Configured) == 0 {
		c.add(Warn, "kein Headset für Gesprächston eingerichtet", "no headset set up for call audio", "bt-headset-hid wireplumber")
	} else {
		paired := map[string]bool{}
		for _, h := range f.Paired {
			paired[strings.ToUpper(h.Address)] = true
		}
		var gone []string
		for _, a := range f.Configured {
			if f.BlueZErr == nil && !paired[strings.ToUpper(a)] {
				gone = append(gone, a)
			}
		}
		if len(gone) > 0 {
			fix := ""
			if keep := without(f.Configured, gone); len(keep) > 0 {
				fix = "bt-headset-hid wireplumber -headset " + strings.Join(keep, ",")
			}
			c.add(Info, "eingerichtet, aber nicht mehr gekoppelt: "+strings.Join(gone, ", "),
				"set up but no longer paired: "+strings.Join(gone, ", "), fix)
		}
		c.add(OK, "Headsets für Gesprächston: "+strings.Join(f.Configured, ", "),
			"headsets for call audio: "+strings.Join(f.Configured, ", "), "")
	}
	if f.OldRules {
		c.add(Warn, "WirePlumber-Regeln pro Headset aus einer älteren Version, neue Headsets brauchen damit einen Neustart",
			"WirePlumber rules for every headset from an older version, new headsets need a restart with them",
			"bt-headset-hid wireplumber")
	}
	if f.Session != Active && f.Session != "" {
		c.add(Fail, "Benutzerdienst bt-headset-hid-session läuft nicht", "user service bt-headset-hid-session is not running",
			"bt-headset-hid wireplumber")
	}
}

// Failed reports whether a finding is a failure.
func Failed(fs []Finding) bool {
	for _, f := range fs {
		if f.Level == Fail {
			return true
		}
	}
	return false
}

// NewFailures returns the failures in fs whose text is not in before, and
// the texts of all failures in fs, for the next call. The session service
// notifies about each problem once, not again on every check.
func NewFailures(before map[string]bool, fs []Finding) ([]Finding, map[string]bool) {
	var fresh []Finding
	now := map[string]bool{}
	for _, f := range fs {
		if f.Level != Fail {
			continue
		}
		now[f.Text] = true
		if !before[f.Text] {
			fresh = append(fresh, f)
		}
	}
	return fresh, now
}

// ModeFromUnit returns the mode the systemd unit of install.UnitFile runs
// the daemon in; without -mode it is the default, ofono.
func ModeFromUnit(unit []byte) string {
	for _, l := range strings.Split(string(unit), "\n") {
		cmd, ok := strings.CutPrefix(strings.TrimSpace(l), "ExecStart=")
		if !ok {
			continue
		}
		f := strings.Fields(cmd)
		for i, a := range f {
			if m, ok := strings.CutPrefix(a, "-mode="); ok {
				return m
			}
			if (a == "-mode" || a == "--mode") && i+1 < len(f) {
				return f[i+1]
			}
		}
		return "ofono"
	}
	return ""
}

// ProgramFromUnit returns the program the systemd unit starts, "" if it
// has no ExecStart.
func ProgramFromUnit(unit []byte) string {
	for _, l := range strings.Split(string(unit), "\n") {
		cmd, ok := strings.CutPrefix(strings.TrimSpace(l), "ExecStart=")
		if !ok {
			continue
		}
		if f := strings.Fields(cmd); len(f) > 0 {
			return strings.TrimLeft(f[0], "-@:+!")
		}
	}
	return ""
}

// HIDName reports whether a hidraw uevent file belongs to a device named
// name.
func HIDName(uevent []byte, name string) bool {
	for _, l := range strings.Split(string(uevent), "\n") {
		if l == "HID_NAME="+name {
			return true
		}
	}
	return false
}

// LastStart returns the lines of the daemon's log after it last started.
func LastStart(lines []string) []string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "Started bt-headset-hid") || strings.Contains(lines[i], "Starting bt-headset-hid") {
			return lines[i+1:]
		}
	}
	return lines
}

// without returns the addresses of all that are not in drop.
func without(all, drop []string) []string {
	var keep []string
	for _, a := range all {
		found := false
		for _, d := range drop {
			if strings.EqualFold(a, d) {
				found = true
			}
		}
		if !found {
			keep = append(keep, a)
		}
	}
	return keep
}
