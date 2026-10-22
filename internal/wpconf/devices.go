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

// DevicesPath is the PipeWire configuration with the fixed devices below the
// configuration directory dir (normally ~/.config).
func DevicesPath(dir string) string {
	return filepath.Join(dir, "pipewire", "pipewire.conf.d", "51-bt-headset-hid.conf")
}

var addressRE = regexp.MustCompile(`^([0-9A-F]{2}:){5}[0-9A-F]{2}$`)

func errNotAddress(addr string) error {
	return fmt.Errorf("wpconf: %q is not a Bluetooth address", addr)
}

// DevicesContent returns the PipeWire configuration for a speaker and a
// microphone named "bt-headset-hid" that always exist and pass the sound to
// and from the headset with Bluetooth address addr while its HFP profile is
// active. Softphones can then keep "bt-headset-hid" selected for the HID
// device, the speaker and the microphone.
//
// Both are loopbacks whose side towards the headset is passive, so the call
// audio (SCO) to the headset only runs while a program uses them. While the
// headset is away, WirePlumber connects them to the default devices.
func DevicesContent(addr string) ([]byte, error) {
	addr = strings.ToUpper(addr)
	if !addressRE.MatchString(addr) {
		return nil, errNotAddress(addr)
	}
	id := strings.ReplaceAll(addr, ":", "_")
	return []byte(`# bt-headset-hid: fixed speaker and microphone "bt-headset-hid" for the headset ` + addr + `.
# ~/.config/pipewire/pipewire.conf.d/51-bt-headset-hid.conf, written by "bt-headset-hid wireplumber".
context.modules = [
  { name = libpipewire-module-loopback
    args = {
      node.description = "bt-headset-hid"
      capture.props = {
        node.name = "bt-headset-hid.speaker"
        media.class = "Audio/Sink"
        audio.position = [ MONO ]
      }
      playback.props = {
        node.name = "bt-headset-hid.speaker.out"
        target.object = "bluez_output.` + id + `.1"
        audio.position = [ MONO ]
        stream.dont-remix = true
        node.passive = true
      }
    }
  }
  { name = libpipewire-module-loopback
    args = {
      node.description = "bt-headset-hid"
      capture.props = {
        node.name = "bt-headset-hid.microphone.in"
        target.object = "bluez_input.` + id + `.0"
        audio.position = [ MONO ]
        stream.dont-remix = true
        node.passive = true
      }
      playback.props = {
        node.name = "bt-headset-hid.microphone"
        media.class = "Audio/Source"
        audio.position = [ MONO ]
      }
    }
  }
]
`), nil
}

// InstallDevices writes the fixed devices for the headset addr and returns
// the path written.
func InstallDevices(dir, addr string) (string, error) {
	data, err := DevicesContent(addr)
	if err != nil {
		return "", err
	}
	p := DevicesPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, data, 0o644)
}

// RemoveDevices deletes the fixed devices and reports whether they existed.
func RemoveDevices(dir string) (bool, error) {
	err := os.Remove(DevicesPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

var configuredRE = regexp.MustCompile(`"bluez_output\.((?:[0-9A-F]{2}_){5}[0-9A-F]{2})\.1"`)

// ConfiguredHeadset returns the address of the headset the fixed devices
// were written for, or "" if there are none.
func ConfiguredHeadset(dir string) string {
	data, err := os.ReadFile(DevicesPath(dir))
	if err != nil {
		return ""
	}
	m := configuredRE.FindSubmatch(data)
	if m == nil {
		return ""
	}
	return strings.ReplaceAll(string(m[1]), "_", ":")
}
