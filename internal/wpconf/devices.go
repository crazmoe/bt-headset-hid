package wpconf

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
// and from the call audio of the connected bt-headset-hid headset.
// Softphones can then keep "bt-headset-hid" selected for the HID device, the
// speaker and the microphone.
//
// Both are loopbacks whose side towards the headset is passive, so the call
// audio (SCO) to the headset only runs while a program uses them.
// "bt-headset-hid session" points that side at the headset (target.object in
// the metadata); without one, WirePlumber connects them to the default
// devices.
func DevicesContent() []byte {
	return []byte(`# bt-headset-hid: fixed speaker and microphone "bt-headset-hid" for the connected headset.
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
`)
}

// InstallDevices writes the fixed devices and returns the path written.
func InstallDevices(dir string) (string, error) {
	p := DevicesPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, DevicesContent(), 0o644)
}

// RemoveDevices deletes the fixed devices and reports whether they existed.
func RemoveDevices(dir string) (bool, error) {
	err := os.Remove(DevicesPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
