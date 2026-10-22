package wpconf

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The call audio nodes of every bt-headset-hid headset get these names, so
// the fixed "bt-headset-hid" speaker and microphone follow whichever of them
// is connected.
const (
	HeadsetOutput = "bluez_output.bt-headset-hid.1"
	HeadsetInput  = "bluez_input.bt-headset-hid.0"
)

// HeadsetPaths returns the WirePlumber file that keeps the headsets in the
// phone profile for v and the file of the other configuration format.
func HeadsetPaths(dir string, v Version) (target, other string) {
	lua := filepath.Join(dir, "wireplumber", "bluetooth.lua.d", "52-bt-headset-hid-headset.lua")
	conf := filepath.Join(dir, "wireplumber", "wireplumber.conf.d", "52-bt-headset-hid-headset.conf")
	if v.Lua() {
		return lua, conf
	}
	return conf, lua
}

// headsetIDs checks the Bluetooth addresses and returns them in upper case
// and with underscores, as PipeWire uses them in names.
func headsetIDs(addrs []string) (upper, ids []string, err error) {
	if len(addrs) == 0 {
		return nil, nil, errors.New("wpconf: no headset")
	}
	seen := map[string]bool{}
	for _, a := range addrs {
		a = strings.ToUpper(strings.TrimSpace(a))
		if !addressRE.MatchString(a) {
			return nil, nil, errNotAddress(a)
		}
		if seen[a] {
			continue
		}
		seen[a] = true
		upper = append(upper, a)
		ids = append(ids, strings.ReplaceAll(a, ":", "_"))
	}
	return upper, ids, nil
}

// PhoneMarker marks headset rules that keep the headsets in the phone
// profile all the time instead of switching for calls.
const PhoneMarker = "bt-headset-hid-profile: phone"

// HeadsetContent returns the WirePlumber rules for the headsets with the
// Bluetooth addresses addrs.
//
// The rules rename the call audio nodes of all these headsets to
// HeadsetOutput and HeadsetInput, the targets of the fixed devices. If more
// than one of them is in the phone profile, WirePlumber uses the first it
// finds.
//
// With phone, the headsets also start in the phone profile (HFP) instead of
// A2DP and stay there, which needs no "bt-headset-hid session". Other
// Bluetooth devices keep A2DP either way.
func HeadsetContent(addrs []string, v Version, phone bool) ([]byte, error) {
	upper, ids, err := headsetIDs(addrs)
	if err != nil {
		return nil, err
	}
	list := strings.Join(upper, ", ")
	what := "switch to the phone profile (HFP) for calls (bt-headset-hid session)"
	if phone {
		what = "start in the phone profile (HFP) and stay there; " + PhoneMarker
	}
	var b strings.Builder
	if v.Lua() {
		fmt.Fprintf(&b, "-- bt-headset-hid: the headsets %s %s.\n", list, what)
		b.WriteString("-- ~/.config/wireplumber/bluetooth.lua.d/52-bt-headset-hid-headset.lua, written by \"bt-headset-hid wireplumber\".\n")
		b.WriteString("-- Their call audio gets one name, so the \"bt-headset-hid\" speaker and microphone use the one connected.\n")
		for _, id := range ids {
			if phone {
				fmt.Fprintf(&b, `table.insert(bluez_monitor.rules, {
  matches = { { { "device.name", "equals", "bluez_card.%s" } } },
  apply_properties = { ["device.profile"] = "headset-head-unit" },
})
`, id)
			}
			fmt.Fprintf(&b, `table.insert(bluez_monitor.rules, {
  matches = { { { "node.name", "equals", "bluez_output.%[1]s.1" }, { "factory.name", "equals", "api.bluez5.sco.sink" } } },
  apply_properties = { ["node.name"] = "%[2]s" },
})
table.insert(bluez_monitor.rules, {
  matches = { { { "node.name", "equals", "bluez_input.%[1]s.0" }, { "factory.name", "equals", "api.bluez5.sco.source" } } },
  apply_properties = { ["node.name"] = "%[3]s" },
})
`, id, HeadsetOutput, HeadsetInput)
		}
		return []byte(b.String()), nil
	}
	fmt.Fprintf(&b, "# bt-headset-hid: the headsets %s %s.\n", list, what)
	b.WriteString("# ~/.config/wireplumber/wireplumber.conf.d/52-bt-headset-hid-headset.conf, written by \"bt-headset-hid wireplumber\".\n")
	b.WriteString("# Their call audio gets one name, so the \"bt-headset-hid\" speaker and microphone use the one connected.\n")
	b.WriteString("monitor.bluez.rules = [\n")
	for _, id := range ids {
		if phone {
			fmt.Fprintf(&b, `  {
    matches = [ { device.name = "bluez_card.%s" } ]
    actions = { update-props = { device.profile = "headset-head-unit" } }
  }
`, id)
		}
		fmt.Fprintf(&b, `  {
    matches = [ { node.name = "bluez_output.%[1]s.1", factory.name = "api.bluez5.sco.sink" } ]
    actions = { update-props = { node.name = "%[2]s" } }
  }
  {
    matches = [ { node.name = "bluez_input.%[1]s.0", factory.name = "api.bluez5.sco.source" } ]
    actions = { update-props = { node.name = "%[3]s" } }
  }
`, id, HeadsetOutput, HeadsetInput)
	}
	b.WriteString("]\n")
	return []byte(b.String()), nil
}

// InstallHeadset writes the rules for the headsets addrs (see HeadsetContent), removes the file of
// the other format and returns the path written.
func InstallHeadset(dir string, addrs []string, v Version, phone bool) (string, error) {
	data, err := HeadsetContent(addrs, v, phone)
	if err != nil {
		return "", err
	}
	target, other := HeadsetPaths(dir, v)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Remove(other); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return target, nil
}

// RemoveHeadset deletes the headset rules in both formats and returns the
// paths that existed.
func RemoveHeadset(dir string) ([]string, error) {
	a, b := HeadsetPaths(dir, Version{0, 5})
	var removed []string
	for _, p := range []string{a, b} {
		err := os.Remove(p)
		switch {
		case err == nil:
			removed = append(removed, p)
		case !errors.Is(err, fs.ErrNotExist):
			return removed, err
		}
	}
	return removed, nil
}

var (
	headsetRE = regexp.MustCompile(`"bluez_output\.((?:[0-9A-F]{2}_){5}[0-9A-F]{2})\.1"`)
)

// ConfiguredPhone reports whether the headset rules keep the headsets in the
// phone profile all the time.
func ConfiguredPhone(dir string) bool {
	a, b := HeadsetPaths(dir, Version{0, 5})
	for _, p := range []string{a, b} {
		if data, err := os.ReadFile(p); err == nil && strings.Contains(string(data), PhoneMarker) {
			return true
		}
	}
	return false
}

// ConfiguredHeadsets returns the addresses of the headsets set up before, in
// either format, or nil if there are none.
func ConfiguredHeadsets(dir string) []string {
	a, b := HeadsetPaths(dir, Version{0, 5})
	var addrs []string
	seen := map[string]bool{}
	add := func(p string) {
		data, err := os.ReadFile(p)
		if err != nil {
			return
		}
		for _, m := range headsetRE.FindAllSubmatch(data, -1) {
			addr := strings.ReplaceAll(string(m[1]), "_", ":")
			if !seen[addr] {
				seen[addr] = true
				addrs = append(addrs, addr)
			}
		}
	}
	add(a)
	add(b)
	if len(addrs) == 0 {
		// Fixed devices written before the headset rules covered several
		// headsets name the one headset the same way.
		add(DevicesPath(dir))
	}
	return addrs
}
