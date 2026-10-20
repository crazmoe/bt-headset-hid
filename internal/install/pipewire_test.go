package install

import "testing"

func TestCheckPipeWire(t *testing.T) {
	for out, ok := range map[string]bool{
		"pipewire\nCompiled with libpipewire 1.0.5\nLinked with libpipewire 1.0.5\n": false,
		"Compiled with libpipewire 0.3.79\nLinked with libpipewire 0.3.79":           false,
		"Compiled with libpipewire 1.0.7\nLinked with libpipewire 1.2.0":             true,
		"Compiled with libpipewire 1.4.2\nLinked with libpipewire 1.4.2":             true,
		"Compiled with libpipewire 2.0.0\nLinked with libpipewire 2.0.0":             true,
		"bash: pipewire: command not found":                                          false,
	} {
		if err := CheckPipeWire(out); (err == nil) != ok {
			t.Errorf("CheckPipeWire(%q) = %v", out, err)
		}
	}
}
