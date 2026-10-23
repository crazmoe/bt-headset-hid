// Package wpconf installs the WirePlumber configuration for a daemon mode
// into the user's configuration directory. WirePlumber 0.4 reads Lua files
// from bluetooth.lua.d, 0.5 and later read SPA-JSON from wireplumber.conf.d.
package wpconf

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Files are the same as contrib/wireplumber; a test keeps them in sync.
//
//go:embed files
var files embed.FS

// Version is a WirePlumber version.
type Version struct{ Major, Minor int }

var versionRE = regexp.MustCompile(`libwireplumber (\d+)\.(\d+)`)

// ParseVersion reads the output of "wireplumber --version", which contains
// e.g. "Linked with libwireplumber 0.4.17".
func ParseVersion(out string) (Version, error) {
	m := versionRE.FindAllStringSubmatch(out, -1)
	if m == nil {
		return Version{}, fmt.Errorf("wpconf: no version in %q", out)
	}
	last := m[len(m)-1] // prefer "Linked with" over "Compiled with"
	major, _ := strconv.Atoi(last[1])
	minor, _ := strconv.Atoi(last[2])
	return Version{major, minor}, nil
}

// Lua reports whether this version uses the Lua configuration (0.4.x).
func (v Version) Lua() bool { return v.Major == 0 && v.Minor < 5 }

// Paths returns the file to write for v and the file of the other
// configuration format, which must not exist at the same time, below the
// configuration directory dir (normally ~/.config).
func Paths(dir string, v Version) (target, other string) {
	lua := filepath.Join(dir, "wireplumber", "bluetooth.lua.d", "51-bt-headset-hid.lua")
	conf := filepath.Join(dir, "wireplumber", "wireplumber.conf.d", "51-bt-headset-hid.conf")
	if v.Lua() {
		return lua, conf
	}
	return conf, lua
}

// Content returns the configuration for mode ("pipewire", "hfp" or
// "ofono") in the format of v.
func Content(mode string, v Version) ([]byte, error) {
	if mode != "pipewire" && mode != "hfp" && mode != "ofono" {
		return nil, fmt.Errorf("wpconf: unknown mode %q", mode)
	}
	ext := ".conf"
	if v.Lua() {
		ext = ".lua"
	}
	return files.ReadFile("files/51-bt-headset-hid-" + mode + ext)
}

// Install writes the configuration for mode and removes the file of the
// other format. It returns the path written.
func Install(dir, mode string, v Version) (string, error) {
	data, err := Content(mode, v)
	if err != nil {
		return "", err
	}
	target, other := Paths(dir, v)
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

var modeRE = regexp.MustCompile(`^(?:--|#) Mode (pipewire|hfp|ofono),`)

// ConfiguredMode returns the mode of the configuration written before, from
// its first line, or "" if there is none.
func ConfiguredMode(dir string) string {
	a, b := Paths(dir, Version{0, 5})
	for _, p := range []string{a, b} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if m := modeRE.FindSubmatch(data); m != nil {
			return string(m[1])
		}
	}
	return ""
}

// Remove deletes the configuration in both formats. It returns the paths
// that existed.
func Remove(dir string) ([]string, error) {
	a, b := Paths(dir, Version{0, 5})
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

// Snapshot returns the contents of the WirePlumber and PipeWire files this
// package writes below dir, so a caller can tell whether they changed and
// PipeWire needs a restart. The headset list is not among them.
func Snapshot(dir string) string {
	a, b := Paths(dir, Version{0, 5})
	c, d := HeadsetPaths(dir, Version{0, 5})
	var sb strings.Builder
	for _, p := range []string{a, b, c, d, noAutoswitchPath(dir), DevicesPath(dir)} {
		data, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(&sb, "%s: none\n", p)
			continue
		}
		fmt.Fprintf(&sb, "%s: %d %q\n", p, len(data), data)
	}
	return sb.String()
}
