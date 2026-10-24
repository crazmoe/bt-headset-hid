package bluez

import (
	"path"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

const (
	ifaceBatteryManager  = "org.bluez.BatteryProviderManager1"
	ifaceBatteryProvider = "org.bluez.BatteryProvider1"
	ifaceObjectManager   = "org.freedesktop.DBus.ObjectManager"
	// batteryRoot holds one battery object per headset.
	batteryRoot = dbus.ObjectPath("/bt_headset_hid/battery")
)

// Batteries reports the battery levels of headsets to BlueZ as a battery
// provider (org.bluez.BatteryProviderManager1), so BlueZ shows them as
// org.bluez.Battery1 on the device, where the desktop's Bluetooth settings
// and "bt-headset-hid session" read them. Some BlueZ versions offer the
// manager only with --experimental; then Set logs that once per adapter.
type Batteries struct {
	conn *dbus.Conn
	logf func(string, ...any)

	mu         sync.Mutex
	objs       map[dbus.ObjectPath]*battery // by device path
	registered map[dbus.ObjectPath]bool     // adapters, true once tried
}

type battery struct {
	path  dbus.ObjectPath
	props *prop.Properties
	level byte
}

// NewBatteries exports the provider's object manager on conn.
func NewBatteries(conn *dbus.Conn, logf func(string, ...any)) (*Batteries, error) {
	b := &Batteries{conn: conn, logf: logf, objs: map[dbus.ObjectPath]*battery{}, registered: map[dbus.ObjectPath]bool{}}
	if err := conn.Export(objectManager{b}, batteryRoot, ifaceObjectManager); err != nil {
		return nil, err
	}
	return b, nil
}

func batteryPath(dev dbus.ObjectPath) dbus.ObjectPath {
	return batteryRoot + "/" + dbus.ObjectPath(path.Base(string(dev)))
}

func (bt *battery) interfaces(dev dbus.ObjectPath) map[string]map[string]dbus.Variant {
	return map[string]map[string]dbus.Variant{ifaceBatteryProvider: {
		"Device":     dbus.MakeVariant(dev),
		"Percentage": dbus.MakeVariant(bt.level),
		"Source":     dbus.MakeVariant("HFP"),
	}}
}

// Set reports the battery level (0..100) of the headset dev.
func (b *Batteries) Set(dev dbus.ObjectPath, percent int) {
	if percent < 0 || percent > 100 || AddressFromPath(dev) == "" {
		return
	}
	level := byte(percent)
	b.mu.Lock()
	bt, ok := b.objs[dev]
	if ok {
		changed := bt.level != level
		bt.level = level
		b.mu.Unlock()
		if changed {
			bt.props.SetMust(ifaceBatteryProvider, "Percentage", level)
		}
		return
	}
	bt = &battery{path: batteryPath(dev), level: level}
	props, err := prop.Export(b.conn, bt.path, prop.Map{ifaceBatteryProvider: {
		"Device":     {Value: dev},
		"Percentage": {Value: level, Emit: prop.EmitTrue},
		"Source":     {Value: "HFP"},
	}})
	if err != nil {
		b.mu.Unlock()
		b.logf("battery of %s: %v", AddressFromPath(dev), err)
		return
	}
	bt.props = props
	b.objs[dev] = bt
	adapter := dbus.ObjectPath(path.Dir(string(dev)))
	register := !b.registered[adapter]
	b.registered[adapter] = true
	b.mu.Unlock()

	if register {
		// BlueZ reads the batteries with GetManagedObjects while this
		// call runs, so b.mu must not be held.
		err := b.conn.Object(service, adapter).Call(ifaceBatteryManager+".RegisterBatteryProvider", 0, batteryRoot).Err
		if err != nil {
			b.logf("BlueZ does not take the battery level (%v); it needs bluetoothd --experimental on older BlueZ", err)
		}
		return
	}
	b.conn.Emit(batteryRoot, ifaceObjectManager+".InterfacesAdded", bt.path, bt.interfaces(dev))
}

// Remove takes the battery of the headset dev away, when it disconnected.
func (b *Batteries) Remove(dev dbus.ObjectPath) {
	b.mu.Lock()
	bt, ok := b.objs[dev]
	delete(b.objs, dev)
	b.mu.Unlock()
	if !ok {
		return
	}
	b.conn.Export(nil, bt.path, "org.freedesktop.DBus.Properties")
	b.conn.Emit(batteryRoot, ifaceObjectManager+".InterfacesRemoved", bt.path, []string{ifaceBatteryProvider})
}

type objectManager struct{ b *Batteries }

func (m objectManager) GetManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, *dbus.Error) {
	m.b.mu.Lock()
	defer m.b.mu.Unlock()
	out := map[dbus.ObjectPath]map[string]map[string]dbus.Variant{}
	for dev, bt := range m.b.objs {
		out[bt.path] = bt.interfaces(dev)
	}
	return out, nil
}
