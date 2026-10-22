// Package callswitch switches headsets that otherwise play music (A2DP) to
// the phone profile (HFP) for a call and back afterwards, and pauses media
// players during the call. The I/O lives behind Env, so the logic is testable
// without PipeWire or D-Bus.
package callswitch

import (
	"strings"

	"github.com/crazmoe/bt-headset-hid/internal/modem"
)

// PhoneProfile is the name PipeWire gives the HFP profile of a headset;
// variants such as "headset-head-unit-cvsd" start with it.
const PhoneProfile = "headset-head-unit"

// Card is a connected Bluetooth headset as PipeWire sees it.
type Card struct {
	ID       uint32         // PipeWire object id
	Name     string         // device.name, e.g. bluez_card.50_C2_75_0D_4E_03
	Profile  string         // active profile
	Profiles map[string]int // available profiles by name, with their index
	// Default: the default output (sink) of the system is a node of this card.
	Default bool
}

// forCall returns the headsets to switch for a call: the one that is the
// default output if there is one, so with two connected the call goes where
// the music plays, otherwise all of them.
func forCall(cards []Card) []Card {
	var def []Card
	for _, c := range cards {
		if c.Default {
			def = append(def, c)
		}
	}
	if len(def) > 0 {
		return def
	}
	return cards
}

// IsPhone reports whether profile is a phone profile.
func IsPhone(profile string) bool { return strings.HasPrefix(profile, PhoneProfile) }

// phoneProfile picks the phone profile of c, or "" if it has none.
func (c Card) phoneProfile() string {
	if _, ok := c.Profiles[PhoneProfile]; ok {
		return PhoneProfile
	}
	best := ""
	for name := range c.Profiles {
		if IsPhone(name) && (best == "" || name < best) {
			best = name
		}
	}
	return best
}

// Env is what the Switcher acts on.
type Env interface {
	// Cards returns the connected headsets to switch.
	Cards() ([]Card, error)
	SetProfile(c Card, profile string) error
	// Playing returns the media players that are playing now.
	Playing() ([]string, error)
	Pause(player string) error
	Play(player string) error
	// Default returns the node name of the default device the user chose
	// for kind (SinkKey or SourceKey), SetDefault chooses it; "" unsets it.
	Default(kind string) (string, error)
	SetDefault(kind, name string) error
}

// Kinds of default devices: the keys of their choice in the "default"
// metadata of PipeWire.
const (
	SinkKey   = "default.configured.audio.sink"
	SourceKey = "default.configured.audio.source"
)

// Switcher follows the call state. It is not safe for concurrent use.
type Switcher struct {
	Env Env
	Log func(format string, args ...any)
	// CallSink and CallSource, if set, become the default devices while a
	// headset is switched for a call: the "bt-headset-hid" speaker and
	// microphone, which lead to that headset. Programs that lost their
	// device, such as a softphone whose stream still points at the phone
	// output of an earlier call, then land there too.
	CallSink, CallSource string
	// Settle, if set, runs after headsets went back to their profile and
	// before the default devices are restored and players resume, so that
	// the music finds the headset again.
	Settle func()

	inCall   bool
	saved    map[string]string // card name -> profile before the call
	paused   []string
	defaults map[string]string // kind -> default before the call, if set for the call
}

func (s *Switcher) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
	}
}

// InCall reports the state of the last Call.
func (s *Switcher) InCall() bool { return s.inCall }

// Call starts (in) or ends a call. At the start it pauses the media players
// that play and switches the headsets to the phone profile; at the end it
// switches back the headsets it switched and resumes the players it paused.
func (s *Switcher) Call(in bool) {
	if in == s.inCall {
		return
	}
	s.inCall = in
	if in {
		s.start()
	} else {
		s.end()
	}
}

func (s *Switcher) start() {
	players, err := s.Env.Playing()
	if err != nil {
		s.logf("media players: %v", err)
	}
	for _, p := range players {
		if err := s.Env.Pause(p); err != nil {
			s.logf("pause %s: %v", p, err)
			continue
		}
		s.logf("paused %s", p)
		s.paused = append(s.paused, p)
	}
	cards, err := s.Env.Cards()
	if err != nil {
		s.logf("headsets: %v", err)
	}
	s.saved = map[string]string{}
	for _, c := range forCall(cards) {
		if IsPhone(c.Profile) {
			continue
		}
		phone := c.phoneProfile()
		if phone == "" {
			s.logf("%s has no phone profile", c.Name)
			continue
		}
		if err := s.Env.SetProfile(c, phone); err != nil {
			s.logf("%s: profile %s: %v", c.Name, phone, err)
			continue
		}
		s.logf("%s: %s -> %s", c.Name, c.Profile, phone)
		s.saved[c.Name] = c.Profile
	}
	if len(s.saved) > 0 {
		s.setDefaults()
	}
}

// setDefaults makes CallSink and CallSource the default devices and keeps
// the ones before. The music output of a headset goes away with its
// profile, and the desktop would otherwise pick another one, such as a
// second headset.
func (s *Switcher) setDefaults() {
	s.defaults = map[string]string{}
	for _, d := range [][2]string{{SinkKey, s.CallSink}, {SourceKey, s.CallSource}} {
		kind, name := d[0], d[1]
		if name == "" {
			continue
		}
		old, err := s.Env.Default(kind)
		if err != nil {
			s.logf("%s: %v", kind, err)
			continue
		}
		if err := s.Env.SetDefault(kind, name); err != nil {
			s.logf("%s %s: %v", kind, name, err)
			continue
		}
		s.defaults[kind] = old
	}
}

func (s *Switcher) end() {
	restored := false
	if len(s.saved) > 0 {
		cards, err := s.Env.Cards()
		if err != nil {
			s.logf("headsets: %v", err)
		}
		for _, c := range cards {
			old, ok := s.saved[c.Name]
			if !ok || c.Profile == old {
				continue
			}
			if _, ok := c.Profiles[old]; !ok {
				continue
			}
			if err := s.Env.SetProfile(c, old); err != nil {
				s.logf("%s: profile %s: %v", c.Name, old, err)
				continue
			}
			s.logf("%s: %s -> %s", c.Name, c.Profile, old)
			restored = true
		}
	}
	s.saved = nil
	if restored && s.Settle != nil && (len(s.paused) > 0 || len(s.defaults) > 0) {
		s.Settle()
	}
	for _, kind := range []string{SinkKey, SourceKey} {
		old, ok := s.defaults[kind]
		if !ok {
			continue
		}
		if err := s.Env.SetDefault(kind, old); err != nil {
			s.logf("%s %s: %v", kind, old, err)
		}
	}
	s.defaults = nil
	for _, p := range s.paused {
		if err := s.Env.Play(p); err != nil {
			s.logf("resume %s: %v", p, err)
			continue
		}
		s.logf("resumed %s", p)
	}
	s.paused = nil
}

// Busy reports whether a call in state s needs the phone profile.
func Busy(s modem.CallState) bool {
	return s >= modem.CallDialing && s <= modem.CallWaiting
}
