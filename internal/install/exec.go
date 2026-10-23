package install

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
)

// Exec runs commands on the host.
type Exec struct{}

func (Exec) Run(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	// Stdin lets "bt-headset-hid wireplumber" ask which headsets to set up.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %w", argv, err)
	}
	return nil
}

// RunAs uses runuser and points the command at the user's session, so that
// "systemctl --user" reaches their PipeWire.
func (Exec) RunAs(name string, uid int, argv []string) error {
	u, err := user.Lookup(name)
	if err != nil {
		return err
	}
	run := "/run/user/" + strconv.Itoa(uid)
	// sudo keeps root's HOME on some systems; the configuration must land in
	// the user's own directory.
	args := append([]string{"-u", name, "--", "env", "-u", "XDG_CONFIG_HOME",
		"HOME=" + u.HomeDir,
		"XDG_RUNTIME_DIR=" + run,
		"DBUS_SESSION_BUS_ADDRESS=unix:path=" + run + "/bus",
	}, argv...)
	return Exec{}.Run(append([]string{"runuser"}, args...))
}
