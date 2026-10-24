package bluez

import (
	"slices"
	"sort"

	"github.com/godbus/dbus/v5"
)

// UUIDs of the headset roles a device announces.
const (
	UUIDHFPHF = "0000111e-0000-1000-8000-00805f9b34fb"
	UUIDHSPHS = "00001108-0000-1000-8000-00805f9b34fb"
)

// Headset is a paired Bluetooth device with a headset role.
type Headset struct {
	Address   string
	Name      string
	Connected bool
	Battery   int // percent, -1 if BlueZ does not know it
}

// Headsets lists the paired headsets BlueZ knows, sorted by address.
func Headsets(conn *dbus.Conn) ([]Headset, error) {
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := conn.Object(service, "/").Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objs); err != nil {
		return nil, err
	}
	var hs []Headset
	for _, ifaces := range objs {
		d, ok := ifaces["org.bluez.Device1"]
		if !ok {
			continue
		}
		paired, _ := d["Paired"].Value().(bool)
		uuids, _ := d["UUIDs"].Value().([]string)
		if !paired || !(slices.Contains(uuids, UUIDHFPHF) || slices.Contains(uuids, UUIDHSPHS)) {
			continue
		}
		h := Headset{}
		h.Address, _ = d["Address"].Value().(string)
		h.Name, _ = d["Alias"].Value().(string)
		h.Connected, _ = d["Connected"].Value().(bool)
		h.Battery = -1
		if p, ok := ifaces["org.bluez.Battery1"]["Percentage"].Value().(byte); ok && p <= 100 {
			h.Battery = int(p)
		}
		hs = append(hs, h)
	}
	sort.Slice(hs, func(i, j int) bool { return hs[i].Address < hs[j].Address })
	return hs, nil
}
