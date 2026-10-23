package install

import "testing"

func TestPickMode(t *testing.T) {
	for _, c := range []struct {
		s    System
		want string
	}{
		{System{OFonoBackend: 1, ModemManager: true}, "ofono"},
		{System{OFonoBackend: -1}, "ofono"},
		{System{OFono: true, OFonoBackend: 1, PipeWireOK: true}, "pipewire"},
		{System{OFonoBackend: 0, PipeWireOK: true}, "pipewire"},
		{System{OFonoBackend: 0, PipeWireOK: true, ModemManager: true}, "hfp"},
		{System{OFonoBackend: 0}, "hfp"},
	} {
		mode, why := PickMode(c.s)
		if mode != c.want || (mode == "ofono") != (why == "") {
			t.Errorf("PickMode(%+v) = %q, %q; want %q", c.s, mode, why, c.want)
		}
	}
}
