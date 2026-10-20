package install

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// FixMarker is written by contrib/pipewire-1.0-fix.sh: "sha256sum" output of
// the Bluetooth plugin it installed.
const FixMarker = "/etc/bt-headset-hid/pipewire-fix"

// ErrPipeWireTooOld marks a PipeWire version that cannot run -mode pipewire.
var ErrPipeWireTooOld = errors.New("PipeWire is too old for -mode pipewire (needs 1.2 or later)")

var pipewireRE = regexp.MustCompile(`libpipewire (\d+)\.(\d+)\.(\d+)`)

// CheckPipeWire reads the output of "pipewire --version" and returns an
// error wrapping ErrPipeWireTooOld if that version cannot run
// -mode pipewire. PipeWire before 1.2 passes a NULL message to libdbus in
// its CallAdded handler (a shadowed variable in
// spa/plugins/bluez5/modemmanager.c); libdbus aborts on that, so every
// incoming call crashes WirePlumber. An old version is fine if the plugin
// fixed by contrib/pipewire-1.0-fix.sh is still in place below root ("" is
// the real root).
func CheckPipeWire(out, root string) error {
	m := pipewireRE.FindAllStringSubmatch(out, -1)
	if m == nil {
		return fmt.Errorf("could not read the PipeWire version from %q", out)
	}
	last := m[len(m)-1] // prefer "Linked with" over "Compiled with"
	major, _ := strconv.Atoi(last[1])
	minor, _ := strconv.Atoi(last[2])
	if major > 1 || (major == 1 && minor >= 2) {
		return nil
	}
	version := strings.Join(last[1:], ".")
	switch fixApplied(root) {
	case fixInPlace:
		return nil
	case fixOverwritten:
		return fmt.Errorf("%w: the fix for PipeWire %s was overwritten by an update; "+
			"run contrib/pipewire-1.0-fix.sh again", ErrPipeWireTooOld, version)
	}
	return fmt.Errorf("%w: PipeWire %s crashes WirePlumber on every incoming call "+
		"(contrib/pipewire-1.0-fix.sh fixes 1.0.x)", ErrPipeWireTooOld, version)
}

type fixState int

const (
	fixNone fixState = iota
	fixInPlace
	fixOverwritten
)

// fixApplied checks the files listed in FixMarker against their hashes.
func fixApplied(root string) fixState {
	marker, err := os.ReadFile(filepath.Join(root, FixMarker))
	if err != nil {
		return fixNone
	}
	state := fixNone
	sc := bufio.NewScanner(bytes.NewReader(marker))
	for sc.Scan() {
		sum, path, ok := strings.Cut(sc.Text(), "  ")
		if !ok || !filepath.IsAbs(path) {
			continue
		}
		state = fixOverwritten
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			continue
		}
		h := sha256.Sum256(data)
		if hex.EncodeToString(h[:]) == sum {
			return fixInPlace
		}
	}
	return state
}
