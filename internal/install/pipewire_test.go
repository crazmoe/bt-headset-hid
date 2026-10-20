package install

import (
	"errors"
	"testing"
)

var errUnreadable = errors.New("unreadable")

func TestCheckPipeWire(t *testing.T) {
	for out, want := range map[string]error{
		"pipewire\nCompiled with libpipewire 1.0.5\nLinked with libpipewire 1.0.5\n": ErrPipeWireTooOld,
		"Compiled with libpipewire 0.3.79\nLinked with libpipewire 0.3.79":           ErrPipeWireTooOld,
		"Compiled with libpipewire 1.0.7\nLinked with libpipewire 1.2.0":             nil,
		"Compiled with libpipewire 1.4.2\nLinked with libpipewire 1.4.2":             nil,
		"Compiled with libpipewire 2.0.0\nLinked with libpipewire 2.0.0":             nil,
		"bash: pipewire: command not found":                                          errUnreadable,
	} {
		err := CheckPipeWire(out)
		switch want {
		case nil:
			if err != nil {
				t.Errorf("CheckPipeWire(%q) = %v", out, err)
			}
		case errUnreadable:
			if err == nil || errors.Is(err, ErrPipeWireTooOld) {
				t.Errorf("CheckPipeWire(%q) = %v, want a read error", out, err)
			}
		default:
			if !errors.Is(err, want) {
				t.Errorf("CheckPipeWire(%q) = %v, want %v", out, err, want)
			}
		}
	}
}
