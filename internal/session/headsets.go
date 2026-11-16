package session

import (
	"log"
	"slices"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/tray"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
	"github.com/crazmoe/bt-headset-hid/internal/wpsetup"
)

// toggleHeadset sets up the headset addr for calls, or takes it out, as
// chosen in the tray menu. A headset taken out stays on the known list, so
// it is not set up again by itself.
func toggleHeadset(dir, addr string, logger *log.Logger) error {
	configured := wpconf.ConfiguredHeadsets(dir)
	selected := tray.Toggle(configured, addr)
	if err := wpconf.SetHeadsets(dir, selected, wpconf.ConfiguredPhone(dir)); err != nil {
		logger.Printf("headsets: %v", err)
		return err
	}
	if known, ok := wpconf.Known(dir); ok && !slices.ContainsFunc(known, func(a string) bool { return strings.EqualFold(a, addr) }) {
		if err := wpconf.SetKnown(dir, append(known, addr)); err != nil {
			logger.Printf("known headsets: %v", err)
		}
	}
	if len(selected) > len(configured) {
		logger.Printf("headset %s set up for calls", addr)
	} else {
		logger.Printf("headset %s no longer set up for calls", addr)
	}
	return nil
}

// cardName is PipeWire's device.name of the headset with Bluetooth address
// addr.
func cardName(addr string) string {
	return "bluez_card." + strings.ReplaceAll(strings.ToUpper(addr), ":", "_")
}

// watchPairing subscribes conn to BlueZ's signals about new, changed and
// removed devices, which pairingChanged and unpaired recognize.
func watchPairing(conn *dbus.Conn) error {
	err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesAdded"),
		dbus.WithMatchSender("org.bluez"),
	)
	if err != nil {
		return err
	}
	err = conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesRemoved"),
		dbus.WithMatchSender("org.bluez"),
	)
	if err != nil {
		return err
	}
	err = conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchSender("org.bluez"),
		dbus.WithMatchArg(0, "org.bluez.Device1"),
	)
	if err != nil {
		return err
	}
	// The battery level, for the tray icon.
	return conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchSender("org.bluez"),
		dbus.WithMatchArg(0, "org.bluez.Battery1"),
	)
}

// pairingChanged reports whether sig says a Bluetooth device appeared or was
// paired, or that its profiles changed.
func pairingChanged(sig *dbus.Signal) bool {
	switch sig.Name {
	case "org.freedesktop.DBus.ObjectManager.InterfacesAdded":
		if len(sig.Body) < 2 {
			return false
		}
		ifaces, _ := sig.Body[1].(map[string]map[string]dbus.Variant)
		_, ok := ifaces["org.bluez.Device1"]
		return ok
	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		if len(sig.Body) < 2 {
			return false
		}
		if iface, _ := sig.Body[0].(string); iface != "org.bluez.Device1" {
			return false
		}
		changed, _ := sig.Body[1].(map[string]dbus.Variant)
		_, paired := changed["Paired"]
		_, uuids := changed["UUIDs"]
		return paired || uuids
	}
	return false
}

// adoptHeadsets adds the headsets of the paired ones hs that were paired
// since the headsets were last set up to the headset list and returns their
// names. Without a list of the headsets known before, from an older version,
// it only writes that list.
func adoptHeadsets(hs []bluez.Headset, dir string, logger *log.Logger) []string {
	paired := wpsetup.Addresses(hs)
	configured := wpconf.ConfiguredHeadsets(dir)
	known, ok := wpconf.Known(dir)
	add := wpconf.NewHeadsets(paired, configured, known, ok)
	if !ok {
		if err := wpconf.SetKnown(dir, append(paired, configured...)); err != nil {
			logger.Printf("known headsets: %v", err)
		}
		return nil
	}
	if len(add) == 0 {
		return nil
	}
	if err := wpconf.SetHeadsets(dir, append(configured, add...), wpconf.ConfiguredPhone(dir)); err != nil {
		logger.Printf("set up %s: %v", strings.Join(add, ", "), err)
		return nil
	}
	if err := wpconf.SetKnown(dir, append(known, add...)); err != nil {
		logger.Printf("known headsets: %v", err)
	}
	logger.Printf("newly paired headset %s set up; to undo: bt-headset-hid wireplumber -headset %s",
		strings.Join(add, ", "), strings.Join(configured, ","))
	var names []string
	for _, a := range add {
		names = append(names, headsetName(hs, a))
	}
	return names
}

// headsetName returns the name of the headset addr in hs, with the
// address, or the address alone.
func headsetName(hs []bluez.Headset, addr string) string {
	for _, h := range hs {
		if strings.EqualFold(h.Address, addr) && h.Name != "" {
			return h.Name + " (" + h.Address + ")"
		}
	}
	return addr
}

// unpaired returns the device path if sig says BlueZ removed a device, as it
// does when the device is unpaired (or its adapter goes away).
func unpaired(sig *dbus.Signal) (dbus.ObjectPath, bool) {
	if sig.Name != "org.freedesktop.DBus.ObjectManager.InterfacesRemoved" || len(sig.Body) < 2 {
		return "", false
	}
	path, _ := sig.Body[0].(dbus.ObjectPath)
	ifaces, _ := sig.Body[1].([]string)
	for _, i := range ifaces {
		if i == "org.bluez.Device1" && bluez.AddressFromPath(path) != "" {
			return path, true
		}
	}
	return "", false
}

// dropHeadset takes the unpaired headset addr off the headset list and
// returns it, or "" if it was not set up. It stays on the known list, so
// pairing it again does not set it up again.
func dropHeadset(dir, addr string, logger *log.Logger) string {
	configured := wpconf.ConfiguredHeadsets(dir)
	keep := wpsetup.DroppedHeadsets(configured, []string{addr})
	if len(keep) == len(configured) {
		return ""
	}
	known, _ := wpconf.Known(dir)
	if err := wpconf.SetKnown(dir, append(known, addr)); err != nil {
		logger.Printf("known headsets: %v", err)
	}
	if err := wpconf.SetHeadsets(dir, keep, wpconf.ConfiguredPhone(dir)); err != nil {
		logger.Printf("unpaired headset %s: %v", addr, err)
		return ""
	}
	logger.Printf("unpaired headset %s no longer set up", addr)
	return addr
}
