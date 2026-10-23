package wpconf

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// SessionUnit is the systemd user service that runs "bt-headset-hid session".
const SessionUnit = "bt-headset-hid-session.service"

// SessionUnitPath is the user service below the configuration directory dir.
func SessionUnitPath(dir string) string {
	return filepath.Join(dir, "systemd", "user", SessionUnit)
}

// SessionUnitContent returns the user service for the program bin.
func SessionUnitContent(bin string) []byte {
	return []byte(`# ~/.config/systemd/user/` + SessionUnit + `, written by "bt-headset-hid wireplumber".
[Unit]
Description=bt-headset-hid: headsets to the phone profile during calls, media paused
After=pipewire.service wireplumber.service

[Service]
ExecStart=` + bin + ` session
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`)
}

// InstallSessionUnit writes the user service for bin and returns its path.
func InstallSessionUnit(dir, bin string) (string, error) {
	p := SessionUnitPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, SessionUnitContent(bin), 0o644)
}

// RemoveSessionUnit deletes the user service and reports whether it existed.
func RemoveSessionUnit(dir string) (bool, error) {
	err := os.Remove(SessionUnitPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// OffPath is the file that says the user removed the setup with
// "bt-headset-hid wireplumber -remove", below the configuration directory
// dir. The session service, which the package starts for every user, then
// leaves the user's PipeWire alone.
func OffPath(dir string) string {
	return filepath.Join(dir, "bt-headset-hid", "off")
}

// Off reports whether the user removed the setup.
func Off(dir string) bool {
	_, err := os.Stat(OffPath(dir))
	return err == nil
}

// SetOff records whether the user removed the setup.
func SetOff(dir string, off bool) error {
	p := OffPath(dir)
	if !off {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte("# bt-headset-hid wireplumber -remove: the session service leaves PipeWire alone.\n# To set it up again: bt-headset-hid wireplumber\n"), 0o644)
}
