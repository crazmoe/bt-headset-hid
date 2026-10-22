package wpconf

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// HeadsetPaths returns the WirePlumber file that keeps one headset in the
// phone profile for v and the file of the other configuration format.
func HeadsetPaths(dir string, v Version) (target, other string) {
	lua := filepath.Join(dir, "wireplumber", "bluetooth.lua.d", "52-bt-headset-hid-headset.lua")
	conf := filepath.Join(dir, "wireplumber", "wireplumber.conf.d", "52-bt-headset-hid-headset.conf")
	if v.Lua() {
		return lua, conf
	}
	return conf, lua
}

// HeadsetContent returns the WirePlumber rule that starts the headset with
// Bluetooth address addr in the phone profile (HFP) instead of A2DP. Other
// Bluetooth devices keep A2DP. Without it WirePlumber puts the headset into
// the music profile between calls, and a press on the call button then often
// reaches the media player instead of answering.
func HeadsetContent(addr string, v Version) ([]byte, error) {
	addr = strings.ToUpper(addr)
	if !addressRE.MatchString(addr) {
		return nil, errNotAddress(addr)
	}
	card := "bluez_card." + strings.ReplaceAll(addr, ":", "_")
	if v.Lua() {
		return []byte(`-- bt-headset-hid: the headset ` + addr + ` starts in the phone profile (HFP), other devices keep A2DP.
-- ~/.config/wireplumber/bluetooth.lua.d/52-bt-headset-hid-headset.lua, written by "bt-headset-hid wireplumber".
table.insert(bluez_monitor.rules, {
  matches = { { { "device.name", "equals", "` + card + `" } } },
  apply_properties = { ["device.profile"] = "headset-head-unit" },
})
`), nil
	}
	return []byte(`# bt-headset-hid: the headset ` + addr + ` starts in the phone profile (HFP), other devices keep A2DP.
# ~/.config/wireplumber/wireplumber.conf.d/52-bt-headset-hid-headset.conf, written by "bt-headset-hid wireplumber".
monitor.bluez.rules = [
  {
    matches = [ { device.name = "` + card + `" } ]
    actions = { update-props = { device.profile = "headset-head-unit" } }
  }
]
`), nil
}

// InstallHeadset writes the phone profile rule for the headset addr, removes
// the file of the other format and returns the path written.
func InstallHeadset(dir, addr string, v Version) (string, error) {
	data, err := HeadsetContent(addr, v)
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

// RemoveHeadset deletes the phone profile rule in both formats and returns
// the paths that existed.
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
