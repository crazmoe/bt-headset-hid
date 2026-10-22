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
