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

// HeadsetContent returns the WirePlumber rules for the headsets with the
// Bluetooth addresses addrs. Each starts in the phone profile (HFP) instead
// of A2DP; other Bluetooth devices keep A2DP. Without it WirePlumber puts a
// headset into the music profile between calls, and a press on the call
// button then often reaches the media player instead of answering.
//
// The rules also rename the call audio nodes of all these headsets to
// HeadsetOutput and HeadsetInput, the targets of the fixed devices. If more
// than one headset is connected, WirePlumber uses the first it finds.
func HeadsetContent(addrs []string, v Version) ([]byte, error) {
	upper, ids, err := headsetIDs(addrs)
	if err != nil {
		return nil, err
	}
	list := strings.Join(upper, ", ")
	var b strings.Builder
	if v.Lua() {
		fmt.Fprintf(&b, "-- bt-headset-hid: the headsets %s start in the phone profile (HFP), other devices keep A2DP.\n", list)
		b.WriteString("-- ~/.config/wireplumber/bluetooth.lua.d/52-bt-headset-hid-headset.lua, written by \"bt-headset-hid wireplumber\".\n")
		b.WriteString("-- Their call audio gets one name, so the \"bt-headset-hid\" speaker and microphone use the one connected.\n")
		for _, id := range ids {
			fmt.Fprintf(&b, `table.insert(bluez_monitor.rules, {
  matches = { { { "device.name", "equals", "bluez_card.%[1]s" } } },
  apply_properties = { ["device.profile"] = "headset-head-unit" },
})
table.insert(bluez_monitor.rules, {
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
	fmt.Fprintf(&b, "# bt-headset-hid: the headsets %s start in the phone profile (HFP), other devices keep A2DP.\n", list)
	b.WriteString("# ~/.config/wireplumber/wireplumber.conf.d/52-bt-headset-hid-headset.conf, written by \"bt-headset-hid wireplumber\".\n")
	b.WriteString("# Their call audio gets one name, so the \"bt-headset-hid\" speaker and microphone use the one connected.\n")
	b.WriteString("monitor.bluez.rules = [\n")
	for _, id := range ids {
		fmt.Fprintf(&b, `  {
    matches = [ { device.name = "bluez_card.%[1]s" } ]
    actions = { update-props = { device.profile = "headset-head-unit" } }
  }
  {
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

// InstallHeadset writes the rules for the headsets addrs, removes the file of
// the other format and returns the path written.
func InstallHeadset(dir string, addrs []string, v Version) (string, error) {
	data, err := HeadsetContent(addrs, v)
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
	cardRE = regexp.MustCompile(`"bluez_card\.((?:[0-9A-F]{2}_){5}[0-9A-F]{2})"`)
	// oldDevicesRE finds the headset in fixed devices written before the
	// headset rules covered several headsets.
	oldDevicesRE = regexp.MustCompile(`"bluez_output\.((?:[0-9A-F]{2}_){5}[0-9A-F]{2})\.1"`)
)

// ConfiguredHeadsets returns the addresses of the headsets set up before, in
// either format, or nil if there are none.
func ConfiguredHeadsets(dir string) []string {
	a, b := HeadsetPaths(dir, Version{0, 5})
	var addrs []string
	seen := map[string]bool{}
	add := func(re *regexp.Regexp, p string) {
		data, err := os.ReadFile(p)
		if err != nil {
			return
		}
		for _, m := range re.FindAllSubmatch(data, -1) {
			addr := strings.ReplaceAll(string(m[1]), "_", ":")
			if !seen[addr] {
				seen[addr] = true
				addrs = append(addrs, addr)
			}
		}
	}
	add(cardRE, a)
	add(cardRE, b)
	if len(addrs) == 0 {
		add(oldDevicesRE, DevicesPath(dir))
	}
	return addrs
}
