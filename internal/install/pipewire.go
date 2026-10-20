package install

import (
	"fmt"
	"regexp"
	"strconv"
)

var pipewireRE = regexp.MustCompile(`libpipewire (\d+)\.(\d+)\.(\d+)`)

// CheckPipeWire reads the output of "pipewire --version" and returns an
// error if that version cannot run -mode pipewire. PipeWire before 1.2
// drops ModemManager's CallAdded signal (a shadowed variable in
// spa/plugins/bluez5/modemmanager.c), so the headset never rings and
// answering is refused with "modem not available".
func CheckPipeWire(out string) error {
	m := pipewireRE.FindAllStringSubmatch(out, -1)
	if m == nil {
		return fmt.Errorf("could not read the PipeWire version from %q", out)
	}
	last := m[len(m)-1] // prefer "Linked with" over "Compiled with"
	major, _ := strconv.Atoi(last[1])
	minor, _ := strconv.Atoi(last[2])
	if major == 0 || (major == 1 && minor < 2) {
		return fmt.Errorf("PipeWire %s.%s.%s is too old for -mode pipewire (needs 1.2 or later): "+
			"it ignores incoming calls, so the headset neither rings nor answers", last[1], last[2], last[3])
	}
	return nil
}
