package install

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// ErrPipeWireTooOld marks a PipeWire version that cannot run -mode pipewire.
var ErrPipeWireTooOld = errors.New("PipeWire is too old for -mode pipewire (needs 1.2 or later)")

var pipewireRE = regexp.MustCompile(`libpipewire (\d+)\.(\d+)\.(\d+)`)

// CheckPipeWire reads the output of "pipewire --version" and returns an
// error wrapping ErrPipeWireTooOld if that version cannot run
// -mode pipewire. PipeWire before 1.2 passes a NULL message to libdbus in
// its CallAdded handler (a shadowed variable in
// spa/plugins/bluez5/modemmanager.c); libdbus aborts on that, so every
// incoming call crashes WirePlumber.
func CheckPipeWire(out string) error {
	m := pipewireRE.FindAllStringSubmatch(out, -1)
	if m == nil {
		return fmt.Errorf("could not read the PipeWire version from %q", out)
	}
	last := m[len(m)-1] // prefer "Linked with" over "Compiled with"
	major, _ := strconv.Atoi(last[1])
	minor, _ := strconv.Atoi(last[2])
	if major == 0 || (major == 1 && minor < 2) {
		return fmt.Errorf("%w: PipeWire %s.%s.%s crashes WirePlumber on every incoming call",
			ErrPipeWireTooOld, last[1], last[2], last[3])
	}
	return nil
}
