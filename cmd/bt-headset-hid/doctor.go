package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/crazmoe/bt-headset-hid/internal/doctor"
	"github.com/crazmoe/bt-headset-hid/internal/i18n"
	"github.com/crazmoe/bt-headset-hid/internal/sysfacts"
)

// errProblems makes "bt-headset-hid doctor" exit with 1 after it printed
// the problems.
var errProblems = errors.New("problems found")

// runDoctor implements "bt-headset-hid doctor": it checks the installation
// and prints what is wrong and how to fix it.
func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bt-headset-hid doctor")
		fmt.Fprintln(fs.Output(), "Checks the setup and says how to fix what is wrong. Run as your normal user.")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	de := i18n.German(os.Getenv)
	fmt.Println("bt-headset-hid", version, "doctor")
	if doctor.Report(os.Stdout, doctor.Check(sysfacts.Real().Facts(), de), de) {
		return errProblems
	}
	return nil
}
