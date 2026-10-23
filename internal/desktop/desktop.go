// Package desktop is the I/O of "bt-headset-hid session" in the user's
// session: headsets through pw-dump and wpctl, media players through MPRIS on
// the session bus, and the call state of the bt-headset-hid modem on the
// system bus.
package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/callswitch"
	"github.com/crazmoe/bt-headset-hid/internal/hfp"
	"github.com/crazmoe/bt-headset-hid/internal/modem"
	"github.com/crazmoe/bt-headset-hid/internal/ofono"
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

func (s System) Output() (string, error) {
	out, err := exec.Command("pw-dump").Output()
	if err != nil {
		return "", fmt.Errorf("pw-dump: %w", err)
	}
	return callswitch.ParseOutput(out)
}

func (s System) SetOutput(name string) error {
	v, err := json.Marshal(struct {
		Name string `json:"name"`
	}{name})
	if err != nil {
		return err
	}
	out, err := exec.Command("pw-metadata", "-n", "default", "0", "default.configured.audio.sink", string(v), "Spa:String:JSON").CombinedOutput()
	if err != nil {
		return fmt.Errorf("pw-metadata: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Routing reads the streams of the fixed devices and the call audio of the
// headsets from PipeWire.
func Routing() (callswitch.Routing, error) {
	out, err := exec.Command("pw-dump").Output()
	if err != nil {
		return callswitch.Routing{}, fmt.Errorf("pw-dump: %w", err)
	}
	return callswitch.ParseRouting(out)
}

// SetTarget points a stream at a node (by its object.serial), or back at the
// default device if c.Value is "".
func SetTarget(c callswitch.Change) error {
	args := []string{"-n", "default", strconv.Itoa(int(c.Subject)), callswitch.TargetKey, c.Value, "Spa:Id"}
	if c.Value == "" {
		args = []string{"-n", "default", "-d", strconv.Itoa(int(c.Subject)), callswitch.TargetKey}
	}
	out, err := exec.Command("pw-metadata", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pw-metadata: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Monitor runs "pw-dump --monitor" and sends on changed, without blocking,
// whenever PipeWire's objects change, until ctx ends. It starts it again
// when it ends, as it does when PipeWire restarts.
func Monitor(ctx context.Context, changed chan<- struct{}, logf func(string, ...any)) {
	for {
		cmd := exec.CommandContext(ctx, "pw-dump", "--monitor", "--no-colors")
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			logf("pw-dump --monitor: %v", err)
		} else {
			buf := make([]byte, 64<<10)
			for {
				if _, err := out.Read(buf); err != nil {
					break
				}
				select {
				case changed <- struct{}{}:
				default:
				}
			}
			cmd.Wait()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
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

// InCall reports whether the bt-headset-hid daemon on the system bus conn
// sees a call that rings, is being dialled or is going on, and which
// headsets it needs. In mode ofono the daemon publishes the call state and
// the headset that took the call under the oFono name; in mode pipewire it is
// the ModemManager modem, which does not know the headset. Without either
// there is no call.
func InCall(conn *dbus.Conn) (bool, callswitch.Route, error) {
	// Without NoAutoStart the bus would try to start oFono or ModemManager,
	// and systemd answers that it has no such unit.
	noOwner := func(err error) bool {
		e, ok := err.(dbus.Error)
		return ok && (e.Name == "org.freedesktop.DBus.Error.ServiceUnknown" || e.Name == "org.freedesktop.DBus.Error.NameHasNoOwner")
	}
	var status map[string]dbus.Variant
	err := conn.Object(ofono.Service, ofono.StatusPath).
		Call("org.freedesktop.DBus.Properties.GetAll", dbus.FlagNoAutoStart, ofono.StatusIface).Store(&status)
	if st, ok := status["CallState"].Value().(string); err == nil && ok {
		dev, _ := status["CallDevice"].Value().(string)
		// While it only rings, wait for the headset that answers.
		r := callswitch.Route{Device: dev, Wait: st == hfp.Incoming.String() && dev == ""}
		return st != hfp.Idle.String(), r, nil
	}
	get := func(path dbus.ObjectPath, iface, prop string) (dbus.Variant, error) {
		var v dbus.Variant
		err := conn.Object(modem.Service, path).
			Call("org.freedesktop.DBus.Properties.Get", dbus.FlagNoAutoStart, iface, prop).Store(&v)
		return v, err
	}
	v, err := get(modem.ModemPath, modem.Service+".Modem.Voice", "Calls")
	if err != nil {
		if noOwner(err) {
			return false, callswitch.Route{}, nil
		}
		return false, callswitch.Route{}, err
	}
	paths, _ := v.Value().([]dbus.ObjectPath)
	for _, p := range paths {
		st, err := get(p, modem.Service+".Call", "State")
		if err != nil {
			continue // the call ended meanwhile
		}
		if n, _ := st.Value().(int32); callswitch.Busy(modem.CallState(n)) {
			return true, callswitch.Route{}, nil
		}
	}
	return false, callswitch.Route{}, nil
}

// Watch subscribes conn to the signals of the daemon (as oFono or as the
// modem) and of its names, so a caller reading conn's signal channel learns
// when to check InCall again.
func Watch(conn *dbus.Conn) error {
	for _, name := range []string{ofono.Service, modem.Service} {
		if err := conn.AddMatchSignal(dbus.WithMatchSender(name)); err != nil {
			return err
		}
		err := conn.AddMatchSignal(
			dbus.WithMatchInterface("org.freedesktop.DBus"),
			dbus.WithMatchMember("NameOwnerChanged"),
			dbus.WithMatchArg(0, name),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// Notify shows a desktop notification from bt-headset-hid over the session
// bus conn.
func Notify(conn *dbus.Conn, summary, body string) error {
	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	return obj.Call("org.freedesktop.Notifications.Notify", 0,
		"bt-headset-hid", uint32(0), "audio-headset", summary, body,
		[]string{}, map[string]dbus.Variant{}, int32(-1)).Err
}
