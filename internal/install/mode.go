package install

// System is what decides the mode when none is given.
type System struct {
	// OFono reports whether the oFono service runs; it owns the D-Bus name
	// that mode ofono serves.
	OFono bool
	// OFonoBackend is 1 if PipeWire's Bluetooth plugin has the oFono
	// backend, 0 if not, -1 if unknown.
	OFonoBackend int
	// PipeWireOK reports whether PipeWire handles mode pipewire (1.2 or
	// later, or 1.0 with the fix).
	PipeWireOK bool
	// ModemManager reports whether ModemManager runs; mode pipewire would
	// stop it.
	ModemManager bool
}

// PickMode returns the mode for s: ofono, which needs no choices, unless it
// cannot work; then pipewire if PipeWire handles it and nothing needs
// ModemManager, else hfp. why says why ofono was not taken.
func PickMode(s System) (mode, why string) {
	switch {
	case s.OFono:
		why = "the oFono service runs and owns the name mode ofono needs"
	case s.OFonoBackend == 0:
		why = "this PipeWire has no oFono backend"
	default:
		return "ofono", ""
	}
	if s.PipeWireOK && !s.ModemManager {
		return "pipewire", why
	}
	return "hfp", why
}
