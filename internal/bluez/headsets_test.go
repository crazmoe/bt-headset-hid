package bluez

import (
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/dbustest"
)

type fakeObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

func (f fakeObjects) GetManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, *dbus.Error) {
	return f, nil
}

func device(addr, name string, paired, connected bool, uuids ...string) map[string]map[string]dbus.Variant {
	return map[string]map[string]dbus.Variant{"org.bluez.Device1": {
		"Address": dbus.MakeVariant(addr), "Alias": dbus.MakeVariant(name),
		"Paired": dbus.MakeVariant(paired), "Connected": dbus.MakeVariant(connected),
		"UUIDs": dbus.MakeVariant(uuids),
	}}
}

func TestHeadsets(t *testing.T) {
	addr := dbustest.Bus(t)
	conn, _ := fakeBlueZ(t, addr)
	objs := fakeObjects{
		"/org/bluez":                            {"org.bluez.ProfileManager1": {}},
		"/org/bluez/hci0":                       {"org.bluez.Adapter1": {}},
		"/org/bluez/hci0/dev_50_C2_75_0D_4E_03": device("50:C2:75:0D:4E:03", "Jabra Evolve 65", true, true, UUIDHFPHF, UUIDHSPHS),
		"/org/bluez/hci0/dev_11_22_33_44_55_66": device("11:22:33:44:55:66", "Phone", true, false, UUIDHFPAG),
		"/org/bluez/hci0/dev_00_11_22_33_44_55": device("00:11:22:33:44:55", "Old headset", true, false, UUIDHSPHS),
		"/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF": device("AA:BB:CC:DD:EE:FF", "Not paired", false, false, UUIDHFPHF),
	}
	if err := conn.Export(objs, "/", "org.freedesktop.DBus.ObjectManager"); err != nil {
		t.Fatal(err)
	}
	hs, err := Headsets(dbustest.Connect(t, addr))
	if err != nil {
		t.Fatal(err)
	}
	want := []Headset{
		{"00:11:22:33:44:55", "Old headset", false},
		{"50:C2:75:0D:4E:03", "Jabra Evolve 65", true},
	}
	if !reflect.DeepEqual(hs, want) {
		t.Fatalf("Headsets = %v", hs)
	}
	if h, ok := PickHeadset(hs); !ok || h.Name != "Jabra Evolve 65" {
		t.Errorf("PickHeadset = %v, %v", h, ok)
	}
	if _, ok := PickHeadset([]Headset{hs[0], {Address: "x"}}); ok {
		t.Error("picked one of two disconnected headsets")
	}
	if h, ok := PickHeadset(hs[:1]); !ok || h != hs[0] {
		t.Errorf("single headset: %v, %v", h, ok)
	}
}
