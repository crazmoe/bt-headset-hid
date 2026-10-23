// Package tray is what the tray icon of "bt-headset-hid session" shows: its
// icon, tooltip and menu, built from the state of the setup. It does no
// I/O, so it can be tested; internal/desktop puts it on the session bus.
package tray

import (
	"fmt"
	"slices"
	"strings"

	"github.com/crazmoe/bt-headset-hid/internal/doctor"
)

// Headset is a paired headset.
type Headset struct {
	Address   string
	Name      string
	Connected bool
	Selected  bool // set up for calls
}

// State is what the tray icon shows.
type State struct {
	Version  string
	Mode     string // ofono, pipewire or hfp
	Headsets []Headset
	InCall   bool
	Problems []doctor.Finding // the failures of the last check
	German   bool
}

// Action is what clicking a menu item does.
type Action int

const (
	None          Action = iota
	ToggleHeadset        // Arg: the headset's address
	ShowProblem          // Arg: the index in State.Problems
	CheckNow
	Help
)

// Check is the checkmark of a menu item.
type Check int

const (
	NoCheck Check = iota
	Unchecked
	Checked
)

// Item is a menu item. ID 0 is the menu itself, so items start at 1.
type Item struct {
	ID        int32
	Label     string
	Enabled   bool
	Separator bool
	Check     Check
	Action    Action
	Arg       string
}

// View is the icon, title, tooltip and menu for a state.
type View struct {
	Icon      string // icon name from the icon theme
	Title     string
	Tooltip   string
	Attention bool // a problem needs the user
	Menu      []Item
}

func t(de bool, german, english string) string {
	if de {
		return german
	}
	return english
}

// name is the headset's name, or its address.
func (h Headset) name() string {
	if h.Name != "" {
		return h.Name
	}
	return h.Address
}

// Show builds the view of s.
func Show(s State) View {
	de := s.German
	v := View{Icon: "audio-headset", Title: "bt-headset-hid"}
	var connected []string
	for _, h := range s.Headsets {
		if h.Connected && (h.Selected || s.Mode == "hfp") {
			connected = append(connected, h.name())
		}
	}
	switch {
	case len(s.Problems) > 0:
		v.Icon, v.Attention = "dialog-warning", true
		if len(s.Problems) == 1 {
			v.Tooltip = s.Problems[0].Text
		} else {
			v.Tooltip = fmt.Sprintf(t(de, "%d Probleme, siehe Menü", "%d problems, see the menu"), len(s.Problems))
		}
	case s.InCall:
		v.Icon = "call-start"
		v.Tooltip = t(de, "Anruf", "Call")
	case len(connected) > 0:
		v.Tooltip = strings.Join(connected, ", ") + t(de, " verbunden", " connected")
	default:
		v.Tooltip = t(de, "kein Headset verbunden", "no headset connected")
	}

	add := func(it Item) {
		it.ID = int32(len(v.Menu) + 1)
		v.Menu = append(v.Menu, it)
	}
	add(Item{Label: fmt.Sprintf("bt-headset-hid %s · %s %s", s.Version, t(de, "Modus", "mode"), s.Mode)})
	add(Item{Separator: true})
	switch {
	case len(s.Headsets) == 0:
		add(Item{Label: t(de, "Kein Headset gekoppelt", "No headset paired")})
	case s.Mode == "hfp":
		add(Item{Label: t(de, "Headsets:", "Headsets:")})
		for _, h := range s.Headsets {
			add(Item{Label: h.name() + state(de, h.Connected)})
		}
	default:
		add(Item{Label: t(de, "Headsets für Anrufe:", "Headsets for calls:")})
		for _, h := range s.Headsets {
			c := Unchecked
			if h.Selected {
				c = Checked
			}
			add(Item{Label: h.name() + state(de, h.Connected), Enabled: true, Check: c, Action: ToggleHeadset, Arg: h.Address})
		}
	}
	add(Item{Separator: true})
	if len(s.Problems) == 0 {
		add(Item{Label: t(de, "Einrichtung in Ordnung", "Setup is fine")})
	}
	for i, p := range s.Problems {
		add(Item{Label: "⚠ " + p.Text, Enabled: true, Action: ShowProblem, Arg: fmt.Sprint(i)})
	}
	add(Item{Label: t(de, "Jetzt prüfen", "Check now"), Enabled: true, Action: CheckNow})
	add(Item{Label: t(de, "Hilfe", "Help"), Enabled: true, Action: Help})
	return v
}

func state(de, connected bool) string {
	if connected {
		return t(de, " (verbunden)", " (connected)")
	}
	return ""
}

// Find returns the item with id in menu.
func Find(menu []Item, id int32) (Item, bool) {
	for _, it := range menu {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// Toggle returns selected with addr taken out if it is in it, otherwise
// added.
func Toggle(selected []string, addr string) []string {
	var out []string
	found := false
	for _, a := range selected {
		if strings.EqualFold(a, addr) {
			found = true
			continue
		}
		out = append(out, a)
	}
	if !found {
		out = append(out, addr)
	}
	return out
}

// Headsets marks the headsets in paired that are in selected.
func Headsets(paired []Headset, selected []string) []Headset {
	out := slices.Clone(paired)
	for i := range out {
		out[i].Selected = slices.ContainsFunc(selected, func(a string) bool { return strings.EqualFold(a, out[i].Address) })
	}
	return out
}

// HelpURL is the page the Help item opens.
func HelpURL(de bool) string {
	return t(de, "https://github.com/crazmoe/bt-headset-hid/blob/main/README.de.md", "https://github.com/crazmoe/bt-headset-hid#readme")
}

// Failures returns the findings in fs that are failures, the problems the
// tray icon shows.
func Failures(fs []doctor.Finding) []doctor.Finding {
	var out []doctor.Finding
	for _, f := range fs {
		if f.Level == doctor.Fail {
			out = append(out, f)
		}
	}
	return out
}
