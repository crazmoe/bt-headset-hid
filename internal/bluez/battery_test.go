package bluez

import (
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/dbustest"
)

// fakeBatteryManager plays org.bluez.BatteryProviderManager1: like BlueZ it
// reads the provider's batteries when the provider registers.
type fakeBatteryManager struct {
	conn    *dbus.Conn
	objects chan map[dbus.ObjectPath]map[string]map[string]dbus.Variant
}

func (m *fakeBatteryManager) RegisterBatteryProvider(root dbus.ObjectPath, sender dbus.Sender) *dbus.Error {
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := m.conn.Object(string(sender), root).Call(ifaceObjectManager+".GetManagedObjects", 0).Store(&objs); err != nil {
		return dbus.MakeFailedError(err)
	}
	m.objects <- objs
	return nil
}

func TestBatteries(t *testing.T) {
	addr := dbustest.Bus(t)
	bluez := dbustest.Connect(t, addr)
	m := &fakeBatteryManager{conn: bluez, objects: make(chan map[dbus.ObjectPath]map[string]map[string]dbus.Variant, 2)}
	if err := bluez.Export(m, "/org/bluez/hci0", ifaceBatteryManager); err != nil {
		t.Fatal(err)
	}
	if _, err := bluez.RequestName(service, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	conn := dbustest.Connect(t, addr)
	var logged []string
	b, err := NewBatteries(conn, func(f string, a ...any) { logged = append(logged, f) })
	if err != nil {
		t.Fatal(err)
	}

	sigs := make(chan *dbus.Signal, 8)
	bluez.Signal(sigs)
	if err := bluez.AddMatchSignal(dbus.WithMatchSender(conn.Names()[0])); err != nil {
		t.Fatal(err)
	}

	dev := dbus.ObjectPath("/org/bluez/hci0/dev_50_C2_75_0D_4E_03")
	b.Set(dev, 80)
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	select {
	case objs = <-m.objects:
	case <-time.After(5 * time.Second):
		t.Fatal("provider not registered")
	}
	p := objs[batteryRoot+"/dev_50_C2_75_0D_4E_03"][ifaceBatteryProvider]
	if p["Device"].Value() != dev || p["Percentage"].Value() != byte(80) || p["Source"].Value() != "HFP" {
		t.Errorf("battery = %v", objs)
	}

	wait := func(name string) *dbus.Signal {
		t.Helper()
		for {
			select {
			case s := <-sigs:
				if s.Name == name {
					return s
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("no %s", name)
			}
		}
	}
	b.Set(dev, 70)
	s := wait("org.freedesktop.DBus.Properties.PropertiesChanged")
	if changed, _ := s.Body[1].(map[string]dbus.Variant); changed["Percentage"].Value() != byte(70) {
		t.Errorf("PropertiesChanged %v", s.Body)
	}
	b.Set(dev, 101) // ignored

	// A second headset on the same adapter is announced, not registered again.
	dev2 := dbus.ObjectPath("/org/bluez/hci0/dev_84_D3_52_8C_B9_D0")
	b.Set(dev2, 30)
	s = wait(ifaceObjectManager + ".InterfacesAdded")
	if s.Body[0] != batteryRoot+"/dev_84_D3_52_8C_B9_D0" {
		t.Errorf("InterfacesAdded %v", s.Body)
	}
	b.Remove(dev)
	s = wait(ifaceObjectManager + ".InterfacesRemoved")
	if s.Body[0] != batteryRoot+"/dev_50_C2_75_0D_4E_03" {
		t.Errorf("InterfacesRemoved %v", s.Body)
	}
	objs = nil
	if err := bluez.Object(conn.Names()[0], batteryRoot).Call(ifaceObjectManager+".GetManagedObjects", 0).Store(&objs); err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 || objs[batteryRoot+"/dev_84_D3_52_8C_B9_D0"][ifaceBatteryProvider]["Percentage"].Value() != byte(30) {
		t.Errorf("objects after Remove = %v", objs)
	}
	if len(logged) != 0 {
		t.Errorf("logged %v", logged)
	}

	// An adapter without the manager: logged, nothing else.
	b.Set("/org/bluez/hci1/dev_11_22_33_44_55_66", 50)
	if len(logged) != 1 {
		t.Errorf("logged %v", logged)
	}
}
