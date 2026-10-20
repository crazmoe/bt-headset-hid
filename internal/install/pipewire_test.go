package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var errUnreadable = errors.New("unreadable")

func TestCheckPipeWire(t *testing.T) {
	root := t.TempDir() // no fix installed
	for out, want := range map[string]error{
		"pipewire\nCompiled with libpipewire 1.0.5\nLinked with libpipewire 1.0.5\n": ErrPipeWireTooOld,
		"Compiled with libpipewire 0.3.79\nLinked with libpipewire 0.3.79":           ErrPipeWireTooOld,
		"Compiled with libpipewire 1.0.7\nLinked with libpipewire 1.2.0":             nil,
		"Compiled with libpipewire 1.4.2\nLinked with libpipewire 1.4.2":             nil,
		"Compiled with libpipewire 2.0.0\nLinked with libpipewire 2.0.0":             nil,
		"bash: pipewire: command not found":                                          errUnreadable,
	} {
		err := CheckPipeWire(out, root)
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

// TestCheckPipeWireFix: the marker written by contrib/pipewire-1.0-fix.sh
// makes 1.0.x acceptable while the fixed plugin is in place.
func TestCheckPipeWireFix(t *testing.T) {
	root := t.TempDir()
	const plugin = "/usr/lib/x86_64-linux-gnu/spa-0.2/bluez5/libspa-bluez5.so"
	write := func(path, data string) {
		t.Helper()
		p := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const out = "Linked with libpipewire 1.0.7"
	write(plugin, "fixed plugin")
	sum := sha256.Sum256([]byte("fixed plugin"))
	write(FixMarker, hex.EncodeToString(sum[:])+"  "+plugin+"\n")
	if err := CheckPipeWire(out, root); err != nil {
		t.Fatalf("with fix: %v", err)
	}

	write(plugin, "plugin from a distribution update")
	err := CheckPipeWire(out, root)
	if !errors.Is(err, ErrPipeWireTooOld) || !strings.Contains(err.Error(), "overwritten") {
		t.Fatalf("after update: %v", err)
	}
}
