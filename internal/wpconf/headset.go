package wpconf

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// HeadsetsPath is the list of the headsets set up for call audio, below the
// configuration directory dir. "bt-headset-hid session" reads it and points
// the "bt-headset-hid" speaker and microphone at the one connected, so
// changing it needs no restart of WirePlumber.
func HeadsetsPath(dir string) string {
	return filepath.Join(dir, "bt-headset-hid", "headsets")
}

// HeadsetPaths returns the WirePlumber file of the headsets for v and the
// file of the other configuration format. Older versions wrote rules for
// every headset there; now the 0.5 file only turns off WirePlumber's own
// switch to the phone profile, and 0.4 needs none.
func HeadsetPaths(dir string, v Version) (target, other string) {
	lua := filepath.Join(dir, "wireplumber", "bluetooth.lua.d", "52-bt-headset-hid-headset.lua")
	conf := filepath.Join(dir, "wireplumber", "wireplumber.conf.d", "52-bt-headset-hid-headset.conf")
	if v.Lua() {
		return lua, conf
	}
	return conf, lua
}

// headsetIDs checks the Bluetooth addresses and returns them in upper case,
// without duplicates.
func headsetIDs(addrs []string) ([]string, error) {
	seen := map[string]bool{}
	var upper []string
	for _, a := range addrs {
		a = strings.ToUpper(strings.TrimSpace(a))
		if !addressRE.MatchString(a) {
			return nil, errNotAddress(a)
		}
		if !seen[a] {
			seen[a] = true
			upper = append(upper, a)
		}
	}
	return upper, nil
}

// PhoneMarker marks a headset list whose headsets stay in the phone profile
// all the time instead of switching for calls.
const PhoneMarker = "bt-headset-hid-profile: phone"

// HeadsetsContent returns the headset list for addrs.
func HeadsetsContent(addrs []string, phone bool) ([]byte, error) {
	upper, err := headsetIDs(addrs)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("# Headsets for call audio through the \"bt-headset-hid\" speaker and microphone; written by \"bt-headset-hid\".\n")
	b.WriteString("# Change them with \"bt-headset-hid wireplumber -headset XX:XX:XX:XX:XX:XX,...\".\n")
	if phone {
		b.WriteString("# " + PhoneMarker + "\n")
	}
	for _, a := range upper {
		b.WriteString(a + "\n")
	}
	return []byte(b.String()), nil
}

// SetHeadsets writes the headset list, which "bt-headset-hid session" follows
// without a restart of WirePlumber.
func SetHeadsets(dir string, addrs []string, phone bool) error {
	data, err := HeadsetsContent(addrs, phone)
	if err != nil {
		return err
	}
	p := HeadsetsPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// noAutoswitchConf turns off WirePlumber 0.5's own switch to the phone
// profile, see InstallHeadset.
const noAutoswitchConf = `# bt-headset-hid: "bt-headset-hid session" switches the headsets to the phone profile for calls;
# WirePlumber's own switch would put every Bluetooth headset into the phone profile when a call starts.
# ~/.config/wireplumber/wireplumber.conf.d/52-bt-headset-hid-headset.conf, written by "bt-headset-hid wireplumber".
wireplumber.settings = {
  bluetooth.autoswitch-to-headset-profile = false
}
`

// noAutoswitchPath is the WirePlumber 0.4 policy file that turns off
// WirePlumber's own switch to the phone profile.
func noAutoswitchPath(dir string) string {
	return filepath.Join(dir, "wireplumber", "policy.lua.d", "52-bt-headset-hid-session.lua")
}

const noAutoswitchLua = `-- bt-headset-hid: "bt-headset-hid session" switches the profile for calls; WirePlumber's own
-- switch would put every Bluetooth headset into the phone profile when a call starts.
-- ~/.config/wireplumber/policy.lua.d/52-bt-headset-hid-session.lua, written by "bt-headset-hid wireplumber".
bluetooth_policy.policy["media-role.use-headset-profile"] = false
`

// InstallHeadset writes the headset list for addrs (see SetHeadsets) and the
// WirePlumber setting for v: unless phone, WirePlumber's own switch to the
// phone profile, which would switch every Bluetooth headset when a call
// starts, is turned off, since "bt-headset-hid session" switches the
// headsets. It removes the per-headset rules of older versions and returns
// the path of the list.
func InstallHeadset(dir string, addrs []string, v Version, phone bool) (string, error) {
	if err := SetHeadsets(dir, addrs, phone); err != nil {
		return "", err
	}
	target, other := HeadsetPaths(dir, v)
	na := noAutoswitchPath(dir)
	remove := []string{other}
	switch {
	case phone:
		remove = append(remove, target, na)
	case v.Lua():
		remove = append(remove, target)
		if err := writeFile(na, []byte(noAutoswitchLua)); err != nil {
			return "", err
		}
	default:
		remove = append(remove, na)
		if err := writeFile(target, []byte(noAutoswitchConf)); err != nil {
			return "", err
		}
	}
	for _, p := range remove {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	return HeadsetsPath(dir), nil
}

func writeFile(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// RemoveHeadset deletes the headset list and the WirePlumber files of
// InstallHeadset, also those of older versions, and returns the paths that
// existed.
func RemoveHeadset(dir string) ([]string, error) {
	a, b := HeadsetPaths(dir, Version{0, 5})
	var removed []string
	for _, p := range []string{HeadsetsPath(dir), a, b, noAutoswitchPath(dir)} {
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

// OldRules reports whether WirePlumber rules for every headset from an
// older version are still there; they need a restart of PipeWire to go.
func OldRules(dir string) bool {
	a, b := HeadsetPaths(dir, Version{0, 5})
	for _, p := range []string{a, b} {
		if data, err := os.ReadFile(p); err == nil && headsetRE.Match(data) {
			return true
		}
	}
	return false
}

var headsetRE = regexp.MustCompile(`"bluez_output\.((?:[0-9A-F]{2}_){5}[0-9A-F]{2})\.1"`)

// ConfiguredPhone reports whether the headsets stay in the phone profile all
// the time.
func ConfiguredPhone(dir string) bool {
	if data, err := os.ReadFile(HeadsetsPath(dir)); err == nil {
		return strings.Contains(string(data), PhoneMarker)
	}
	a, b := HeadsetPaths(dir, Version{0, 5})
	for _, p := range []string{a, b} {
		if data, err := os.ReadFile(p); err == nil && strings.Contains(string(data), PhoneMarker) {
			return true
		}
	}
	return false
}

// ConfiguredHeadsets returns the addresses of the headsets set up, or nil if
// there are none. Without a headset list it reads those of older versions'
// rules.
func ConfiguredHeadsets(dir string) []string {
	if data, err := os.ReadFile(HeadsetsPath(dir)); err == nil {
		var addrs []string
		for _, l := range strings.Split(string(data), "\n") {
			if l = strings.ToUpper(strings.TrimSpace(l)); addressRE.MatchString(l) {
				addrs = append(addrs, l)
			}
		}
		return addrs
	}
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
