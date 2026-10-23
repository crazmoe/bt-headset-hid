// Package install sets up bt-headset-hid on the host: binary, systemd
// service, udev rule, D-Bus policy (modes pipewire and ofono) and, as the invoking user,
// the WirePlumber configuration. Plan builds the steps without side effects
// so they can be tested; Apply carries them out.
package install

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// Files are copies of contrib/udev and contrib/dbus; a test keeps them in sync.
//
//go:embed files
var files embed.FS

// Paths on the host.
const (
	Binary     = "/usr/local/bin/bt-headset-hid"
	Unit       = "/etc/systemd/system/bt-headset-hid.service"
	UdevRule   = "/etc/udev/rules.d/70-bt-headset-hid.rules"
	DBusPolicy = "/etc/dbus-1/system.d/bt-headset-hid.conf"
	ModulesCfg = "/etc/modules-load.d/bt-headset-hid.conf"
	Service    = "bt-headset-hid.service"
	// PackagedBinary is where the deb and rpm packages put the program. The
	// package manager owns it, so install uses it in place and uninstall
	// leaves it alone.
	PackagedBinary = "/usr/bin/bt-headset-hid"
)

// Options describe an installation.
type Options struct {
	Mode string // "pipewire", "hfp" or "ofono"
	// Self is the running executable, copied to Binary unless it is
	// PackagedBinary.
	Self string
	// User is the desktop user whose WirePlumber configuration is set up
	// (from SUDO_USER); empty skips that step.
	User string
	UID  int
}

// Step is one action. Exactly one of the fields is set.
type Step struct {
	Write   *File    // write a file
	Copy    *File    // copy File.From to File.Path
	Remove  string   // remove a file if it exists
	Run     []string // run a command as root; failure aborts
	Try     []string // run a command as root; failure is only reported
	AsUser  []string // run a command as Options.User in their session
	Comment string   // printed before the step
}

// File is a file to write or copy.
type File struct {
	Path string
	Data []byte
	From string
	Perm fs.FileMode
}

// binary is the program the service and the WirePlumber step run.
func (o Options) binary() string {
	if o.Self == PackagedBinary {
		return PackagedBinary
	}
	return Binary
}

// UnitFile returns the systemd unit that runs bin in mode.
func UnitFile(bin, mode string) []byte {
	return []byte(`[Unit]
Description=Bluetooth headset buttons as HID telephony device
After=bluetooth.service dbus.service
Wants=bluetooth.service

[Service]
# Switch modes with "sudo bt-headset-hid install -mode pipewire" (or ofono, hfp).
ExecStart=` + bin + ` -mode ` + mode + `
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`)
}

func asset(name string) []byte {
	b, err := files.ReadFile("files/" + name)
	if err != nil {
		panic(err) // embedded, cannot fail
	}
	return b
}

// Plan returns the steps that install bt-headset-hid for o.Mode.
func Plan(o Options) ([]Step, error) {
	if o.Mode != "pipewire" && o.Mode != "hfp" && o.Mode != "ofono" {
		return nil, fmt.Errorf("install: unknown mode %q", o.Mode)
	}
	var s []Step
	switch o.Self {
	case PackagedBinary:
		// Drop a copy left from an installation without the package.
		s = append(s, Step{Remove: Binary})
	case Binary:
	default:
		s = append(s, Step{Comment: "install " + Binary, Copy: &File{Path: Binary, From: o.Self, Perm: 0o755}})
	}
	s = append(s,
		Step{Comment: "load the uhid kernel module now and at boot", Write: &File{Path: ModulesCfg, Data: []byte("uhid\n"), Perm: 0o644}},
		Step{Try: []string{"modprobe", "uhid"}},
		Step{Comment: "udev rule for /dev/hidrawN access", Write: &File{Path: UdevRule, Data: asset("70-bt-headset-hid.rules"), Perm: 0o644}},
		Step{Try: []string{"udevadm", "control", "--reload"}},
	)
	switch o.Mode {
	case "pipewire":
		s = append(s,
			Step{Comment: "D-Bus policy for the ModemManager name", Write: &File{Path: DBusPolicy, Data: asset("bt-headset-hid.conf"), Perm: 0o644}},
			Step{Comment: "stop ModemManager (it would own the same bus name)", Try: []string{"systemctl", "disable", "--now", "ModemManager.service"}},
		)
	case "ofono":
		s = append(s,
			Step{Comment: "D-Bus policy for the oFono name", Write: &File{Path: DBusPolicy, Data: asset("bt-headset-hid-ofono.conf"), Perm: 0o644}},
			Step{Comment: "start ModemManager again if mode pipewire stopped it (mobile broadband)", Try: []string{"systemctl", "enable", "--now", "ModemManager.service"}},
		)
	default:
		s = append(s, Step{Remove: DBusPolicy})
	}
	s = append(s,
		Step{Comment: "systemd service with -mode " + o.Mode, Write: &File{Path: Unit, Data: UnitFile(o.binary(), o.Mode), Perm: 0o644}},
		Step{Run: []string{"systemctl", "daemon-reload"}},
		Step{Run: []string{"systemctl", "enable", Service}},
	)
	// WirePlumber first, so PipeWire has released (or taken) HFP before the
	// daemon starts.
	if o.User != "" {
		s = append(s, Step{Comment: "WirePlumber configuration for " + o.User, AsUser: []string{o.binary(), "wireplumber", "-mode", o.Mode}})
	}
	s = append(s, Step{Comment: "start the service", Run: []string{"systemctl", "restart", Service}})
	return s, nil
}

// PlanUninstall returns the steps that remove everything Plan installs.
// ModemManager stays disabled; the caller tells the user how to enable it.
func PlanUninstall(o Options) []Step {
	var s []Step
	if o.User != "" {
		s = append(s, Step{Comment: "remove WirePlumber configuration of " + o.User, AsUser: []string{o.binary(), "wireplumber", "-remove"}})
	}
	s = append(s,
		Step{Comment: "stop and disable the service", Try: []string{"systemctl", "disable", "--now", Service}},
		Step{Remove: Unit},
		Step{Run: []string{"systemctl", "daemon-reload"}},
		Step{Remove: UdevRule},
		Step{Try: []string{"udevadm", "control", "--reload"}},
		Step{Remove: DBusPolicy},
		Step{Remove: ModulesCfg},
		Step{Remove: Binary},
	)
	return s
}

// Runner executes commands; Exec is the real one.
type Runner interface {
	Run(argv []string) error
	// RunAs runs argv as user (uid) with their session environment.
	RunAs(user string, uid int, argv []string) error
}

// Apply carries out the steps. root is prepended to every file path (""
// for the real system). log receives progress lines.
func Apply(steps []Step, o Options, root string, r Runner, log func(string)) error {
	for _, st := range steps {
		if st.Comment != "" {
			log(st.Comment)
		}
		var err error
		switch {
		case st.Write != nil:
			err = writeFile(filepath.Join(root, st.Write.Path), st.Write.Data, st.Write.Perm)
		case st.Copy != nil:
			var data []byte
			if data, err = os.ReadFile(st.Copy.From); err == nil {
				err = writeFile(filepath.Join(root, st.Copy.Path), data, st.Copy.Perm)
			}
		case st.Remove != "":
			if err = os.Remove(filepath.Join(root, st.Remove)); errors.Is(err, fs.ErrNotExist) {
				err = nil
			} else if err == nil {
				log("removed " + st.Remove)
			}
		case st.Run != nil:
			err = r.Run(st.Run)
		case st.Try != nil:
			if e := r.Run(st.Try); e != nil {
				log(fmt.Sprintf("  (ignored: %v)", e))
			}
		case st.AsUser != nil:
			err = r.RunAs(o.User, o.UID, st.AsUser)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// writeFile replaces path atomically, so a running binary can be updated.
func writeFile(path string, data []byte, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SudoUser returns the user who ran sudo, from SUDO_USER and SUDO_UID.
func SudoUser(getenv func(string) string) (string, int, bool) {
	user := getenv("SUDO_USER")
	uid, err := strconv.Atoi(getenv("SUDO_UID"))
	if user == "" || user == "root" || err != nil || uid == 0 {
		return "", 0, false
	}
	return user, uid, true
}
