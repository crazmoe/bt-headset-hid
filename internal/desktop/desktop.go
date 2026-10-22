// Package desktop is the I/O of "bt-headset-hid session" in the user's
// session: headsets through pw-dump and wpctl, media players through MPRIS on
// the session bus, and the call state of the bt-headset-hid modem on the
// system bus.
package desktop

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/callswitch"
	"github.com/crazmoe/bt-headset-hid/internal/modem"
)

// System is the callswitch.Env of a user session.
type System struct {
	Session *dbus.Conn
	// Names are the device.name values of the headsets to switch.
	Names map[string]bool
	// Skip are parts of MPRIS names, in lower case, of players never to
	// pause, such as softphones.
	Skip []string
}

// Softphones are parts of the MPRIS names of softphones. Some report their
// ringtone as playing, and pausing them would silence the call.
var Softphones = []string{"starclx", "baresip", "linphone", "zoiper", "microsip", "jami", "skype", "teams", "zoom", "jitsi"}

func (s System) skip(name string) bool {
	// playerctld only mirrors the other players.
	if name == mprisPrefix+"playerctld" {
		return true
	}
	lower := strings.ToLower(strings.TrimPrefix(name, mprisPrefix))
	for _, k := range s.Skip {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

func (s System) Cards() ([]callswitch.Card, error) {
	out, err := exec.Command("pw-dump").Output()
	if err != nil {
		return nil, fmt.Errorf("pw-dump: %w", err)
	}
	return callswitch.ParseCards(out, s.Names)
}

func (s System) Default(kind string) (string, error) {
	out, err := exec.Command("pw-dump").Output()
	if err != nil {
		return "", fmt.Errorf("pw-dump: %w", err)
	}
	return callswitch.ParseDefault(out, kind)
}

func (s System) SetDefault(kind, name string) error {
	args := []string{"-n", "default", "-d", "0", kind}
	if name != "" {
		v, err := json.Marshal(struct {
			Name string `json:"name"`
		}{name})
		if err != nil {
			return err
		}
		args = []string{"-n", "default", "0", kind, string(v), "Spa:String:JSON"}
	}
	out, err := exec.Command("pw-metadata", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pw-metadata: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s System) SetProfile(c callswitch.Card, profile string) error {
	i, ok := c.Profiles[profile]
	if !ok {
		return fmt.Errorf("no profile %s", profile)
	}
	out, err := exec.Command("wpctl", "set-profile", strconv.Itoa(int(c.ID)), strconv.Itoa(i)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("wpctl set-profile: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

const (
	mprisPrefix = "org.mpris.MediaPlayer2."
	mprisPath   = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	mprisPlayer = "org.mpris.MediaPlayer2.Player"
)

func (s System) Playing() ([]string, error) {
	var names []string
	if err := s.Session.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return nil, err
	}
	var playing []string
	for _, n := range names {
		if !strings.HasPrefix(n, mprisPrefix) || s.skip(n) {
			continue
		}
		v, err := s.Session.Object(n, mprisPath).GetProperty(mprisPlayer + ".PlaybackStatus")
		if err != nil {
			continue
		}
		if st, _ := v.Value().(string); st == "Playing" {
			playing = append(playing, n)
		}
	}
	return playing, nil
}

func (s System) Pause(player string) error {
	return s.Session.Object(player, mprisPath).Call(mprisPlayer+".Pause", 0).Err
}

func (s System) Play(player string) error {
	return s.Session.Object(player, mprisPath).Call(mprisPlayer+".Play", 0).Err
}

// InCall reports whether the ModemManager modem on the system bus conn, which
// is the bt-headset-hid daemon in mode pipewire, has a call that rings, is
// being dialled or is going on. Without the modem there is no call.
func InCall(conn *dbus.Conn) (bool, error) {
	// Without NoAutoStart the bus would try to start ModemManager, and
	// systemd answers that it has no such unit.
	get := func(path dbus.ObjectPath, iface, prop string) (dbus.Variant, error) {
		var v dbus.Variant
		err := conn.Object(modem.Service, path).
			Call("org.freedesktop.DBus.Properties.Get", dbus.FlagNoAutoStart, iface, prop).Store(&v)
		return v, err
	}
	v, err := get(modem.ModemPath, modem.Service+".Modem.Voice", "Calls")
	if err != nil {
		if e, ok := err.(dbus.Error); ok && (e.Name == "org.freedesktop.DBus.Error.ServiceUnknown" || e.Name == "org.freedesktop.DBus.Error.NameHasNoOwner") {
			return false, nil
		}
		return false, err
	}
	paths, _ := v.Value().([]dbus.ObjectPath)
	for _, p := range paths {
		st, err := get(p, modem.Service+".Call", "State")
		if err != nil {
			continue // the call ended meanwhile
		}
		if n, _ := st.Value().(int32); callswitch.Busy(modem.CallState(n)) {
			return true, nil
		}
	}
	return false, nil
}

// Watch subscribes conn to the signals of the modem and of its name, so a
// caller reading conn's signal channel learns when to check InCall again.
func Watch(conn *dbus.Conn) error {
	if err := conn.AddMatchSignal(dbus.WithMatchSender(modem.Service)); err != nil {
		return err
	}
	return conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, modem.Service),
	)
}
