package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/crazmoe/bt-headset-hid/internal/install"
)

// runInstall implements "bt-headset-hid install" and "uninstall".
func runInstall(args []string, uninstall bool) error {
	name := "install"
	if uninstall {
		name = "uninstall"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	mode := ""
	if !uninstall {
		fs.StringVar(&mode, "mode", "", `"pipewire": PipeWire keeps HFP and the audio; "hfp": all buttons, no call audio (asked if not given)`)
	}
	fs.Usage = func() {
		if uninstall {
			fmt.Fprintln(fs.Output(), "usage: sudo bt-headset-hid uninstall")
		} else {
			fmt.Fprintln(fs.Output(), "usage: sudo bt-headset-hid install [-mode pipewire|hfp]")
		}
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return errors.New("run with sudo")
	}
	if !uninstall && mode == "" {
		var err error
		if mode, err = askMode(os.Stdin); err != nil {
			return err
		}
	}
	if mode == "pipewire" {
		out, _ := exec.Command("pipewire", "--version").CombinedOutput()
		switch err := install.CheckPipeWire(string(out), ""); {
		case errors.Is(err, install.ErrPipeWireTooOld):
			return fmt.Errorf("%v.\nUse \"sudo bt-headset-hid install -mode hfp\", update PipeWire or run the fix (see README)", err)
		case err != nil:
			fmt.Println("WARNING:", err, "- -mode pipewire needs PipeWire 1.2 or later")
		}
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return err
	}
	o := install.Options{Mode: mode, Self: self}
	if user, uid, ok := install.SudoUser(os.Getenv); ok {
		o.User, o.UID = user, uid
	} else {
		step := "wireplumber -mode " + mode
		if uninstall {
			step = "wireplumber -remove"
		}
		fmt.Println("note: not started via sudo from a desktop user; skipping the WirePlumber step,")
		fmt.Println("      run \"bt-headset-hid " + step + "\" as that user yourself")
	}

	var steps []install.Step
	if uninstall {
		steps = install.PlanUninstall(o)
	} else if steps, err = install.Plan(o); err != nil {
		return err
	}
	if err := install.Apply(steps, o, "", install.Exec{}, func(s string) { fmt.Println("==>", s) }); err != nil {
		return err
	}

	if uninstall {
		fmt.Println("bt-headset-hid removed. If you use ModemManager: sudo systemctl enable --now ModemManager")
		return nil
	}
	fmt.Println("bt-headset-hid installed in mode", mode+". Reconnect the headset, then check:")
	fmt.Println("  journalctl -u bt-headset-hid -f")
	return nil
}

// askMode asks for the mode on the terminal.
func askMode(in io.Reader) (string, error) {
	fmt.Println("Which mode?")
	fmt.Println("  1) pipewire: talk through the headset; answer and hang up work (recommended)")
	fmt.Println("  2) hfp:      all buttons (redial, volume) work, but no call audio through the headset")
	r := bufio.NewReader(in)
	for {
		fmt.Print("Choose 1 or 2 [1]: ")
		line, err := r.ReadString('\n')
		answer := strings.TrimSpace(line)
		if err != nil && answer == "" {
			return "", errors.New("no mode chosen; use -mode pipewire or -mode hfp")
		}
		switch answer {
		case "", "1", "pipewire":
			return "pipewire", nil
		case "2", "hfp":
			return "hfp", nil
		}
		if err != nil {
			return "", errors.New("no mode chosen; use -mode pipewire or -mode hfp")
		}
	}
}
