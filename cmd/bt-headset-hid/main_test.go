package main

import (
	"io"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
)

func TestVersion(t *testing.T) {
	if version == "" {
		t.Fatal("version must not be empty")
	}
}

func TestAskMode(t *testing.T) {
	for in, want := range map[string]string{"\n": "ofono", "1\n": "ofono", "2\n": "pipewire", "3\n": "hfp", "x\nhfp\n": "hfp", "3": "hfp", "ofono\n": "ofono"} {
		got, err := askMode(strings.NewReader(in))
		if err != nil || got != want {
			t.Errorf("askMode(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "x"} {
		if got, err := askMode(strings.NewReader(in)); err == nil {
			t.Errorf("askMode(%q) = %q, want error", in, got)
		}
	}
}

func TestDroppedHeadsets(t *testing.T) {
	before := []string{"50:C2:75:0D:4E:03", "84:D3:52:8C:B9:D0"}
	got := droppedHeadsets(before, []string{"84:d3:52:8c:b9:d0"})
	if len(got) != 1 || got[0] != "50:C2:75:0D:4E:03" {
		t.Errorf("droppedHeadsets = %q", got)
	}
	if got := droppedHeadsets(before, []string{"84:D3:52:8C:B9:D0", "50:c2:75:0d:4e:03"}); got != nil {
		t.Errorf("droppedHeadsets with both = %q", got)
	}
}

func TestChooseHeadsets(t *testing.T) {
	jabra := bluez.Headset{Address: "50:C2:75:0D:4E:03", Name: "Jabra", Connected: true}
	jbl := bluez.Headset{Address: "AC:BF:71:12:34:56", Name: "JBL"}
	car := bluez.Headset{Address: "00:11:22:33:44:55", Name: "Car"}
	all := []bluez.Headset{car, jabra, jbl}
	names := func(hs []bluez.Headset) string {
		var n []string
		for _, h := range hs {
			n = append(n, h.Name)
		}
		return strings.Join(n, ",")
	}
	for _, c := range []struct {
		hs          []bluez.Headset
		in          string
		interactive bool
		want        string
	}{
		{nil, "", true, ""},
		{[]bluez.Headset{jbl}, "", true, "JBL"},
		{all, "", false, "Jabra"},
		{[]bluez.Headset{car, jbl}, "", false, "Car,JBL"},
		{all, "\n", true, "Jabra"},
		{all, "", true, "Jabra"},
		{all, "all\n", true, "Car,Jabra,JBL"},
		{all, "3, 1\n", true, "JBL,Car"},
		{all, "4\nx\n2 2\n", true, "Jabra"},
		{all, "4", true, "Jabra"},
	} {
		if got := names(chooseHeadsets(c.hs, strings.NewReader(c.in), c.interactive, io.Discard)); got != c.want {
			t.Errorf("chooseHeadsets(%s, %q, %v) = %s, want %s", names(c.hs), c.in, c.interactive, got, c.want)
		}
	}
}

func TestPairingChanged(t *testing.T) {
	props := "org.freedesktop.DBus.Properties.PropertiesChanged"
	added := "org.freedesktop.DBus.ObjectManager.InterfacesAdded"
	v := dbus.MakeVariant(true)
	for _, c := range []struct {
		sig  dbus.Signal
		want bool
	}{
		{dbus.Signal{Name: props, Body: []any{"org.bluez.Device1", map[string]dbus.Variant{"Paired": v}, []string{}}}, true},
		{dbus.Signal{Name: props, Body: []any{"org.bluez.Device1", map[string]dbus.Variant{"UUIDs": v}, []string{}}}, true},
		{dbus.Signal{Name: props, Body: []any{"org.bluez.Device1", map[string]dbus.Variant{"RSSI": v}, []string{}}}, false},
		{dbus.Signal{Name: props, Body: []any{"org.ofono.bt-headset-hid", map[string]dbus.Variant{"Paired": v}, []string{}}}, false},
		{dbus.Signal{Name: added, Body: []any{dbus.ObjectPath("/org/bluez/hci0/dev_X"), map[string]map[string]dbus.Variant{"org.bluez.Device1": {}}}}, true},
		{dbus.Signal{Name: added, Body: []any{dbus.ObjectPath("/org/bluez/hci0/dev_X/sep1"), map[string]map[string]dbus.Variant{"org.bluez.MediaEndpoint1": {}}}}, false},
		{dbus.Signal{Name: props}, false},
		{dbus.Signal{Name: "org.ofono.VoiceCallManager.CallAdded"}, false},
	} {
		if got := pairingChanged(&c.sig); got != c.want {
			t.Errorf("pairingChanged(%s %v) = %v", c.sig.Name, c.sig.Body, got)
		}
	}
	if got := cardName("50:c2:75:0d:4e:03"); got != "bluez_card.50_C2_75_0D_4E_03" {
		t.Errorf("cardName = %s", got)
	}
}

func TestUnpaired(t *testing.T) {
	removed := "org.freedesktop.DBus.ObjectManager.InterfacesRemoved"
	dev := dbus.ObjectPath("/org/bluez/hci0/dev_84_D3_52_8C_B9_D0")
	if p, ok := unpaired(&dbus.Signal{Name: removed, Body: []any{dev, []string{"org.freedesktop.DBus.Properties", "org.bluez.Device1"}}}); !ok || p != dev {
		t.Errorf("unpaired device = %s, %v", p, ok)
	}
	if _, ok := unpaired(&dbus.Signal{Name: removed, Body: []any{dev + "/sep1", []string{"org.bluez.MediaEndpoint1"}}}); ok {
		t.Error("endpoint taken as unpaired device")
	}
	if _, ok := unpaired(&dbus.Signal{Name: removed}); ok {
		t.Error("signal without body taken as unpaired device")
	}
}
