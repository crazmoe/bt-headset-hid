package session

import (
	"bytes"
	"errors"
	"log"
	"reflect"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/dbustest"
	"github.com/crazmoe/bt-headset-hid/internal/doctor"
	"github.com/crazmoe/bt-headset-hid/internal/i18n"
	"github.com/crazmoe/bt-headset-hid/internal/install"
	"github.com/crazmoe/bt-headset-hid/internal/wpconf"
	"github.com/crazmoe/bt-headset-hid/internal/wpsetup"
)

const (
	jabra = "50:C2:75:0D:4E:03"
	jbl   = "AC:BF:71:12:34:56"
)

func logger() (*log.Logger, *bytes.Buffer) {
	var b bytes.Buffer
	return log.New(&b, "", 0), &b
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
		{dbus.Signal{Name: added}, false},
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

func TestWatchPairing(t *testing.T) {
	if err := watchPairing(dbustest.Connect(t, dbustest.Bus(t))); err != nil {
		t.Fatal(err)
	}
}

func TestHeadsetName(t *testing.T) {
	hs := []bluez.Headset{{Address: jabra, Name: "Jabra"}, {Address: jbl}}
	for addr, want := range map[string]string{
		strings.ToLower(jabra): "Jabra (" + jabra + ")",
		jbl:                    jbl,
		"00:11:22:33:44:55":    "00:11:22:33:44:55",
	} {
		if got := headsetName(hs, addr); got != want {
			t.Errorf("headsetName(%s) = %q, want %q", addr, got, want)
		}
	}
}

func TestAdoptHeadsets(t *testing.T) {
	dir := t.TempDir()
	lg, _ := logger()
	hs := []bluez.Headset{{Address: jabra, Name: "Jabra"}}
	// Without a known list, from an older version, it only writes that.
	if added := adoptHeadsets(hs, dir, lg); added != nil {
		t.Errorf("first run adopted %v", added)
	}
	if known, ok := wpconf.Known(dir); !ok || !reflect.DeepEqual(known, []string{jabra}) {
		t.Errorf("known = %v, %v", known, ok)
	}
	// A headset paired later is set up.
	hs = append(hs, bluez.Headset{Address: jbl, Name: "JBL"})
	if added := adoptHeadsets(hs, dir, lg); !reflect.DeepEqual(added, []string{"JBL (" + jbl + ")"}) {
		t.Errorf("adopted %v", added)
	}
	if got := wpconf.ConfiguredHeadsets(dir); !reflect.DeepEqual(got, []string{jbl}) {
		t.Errorf("headsets = %v", got)
	}
	// Only once.
	if added := adoptHeadsets(hs, dir, lg); added != nil {
		t.Errorf("adopted again: %v", added)
	}
}

func TestDropHeadset(t *testing.T) {
	dir := t.TempDir()
	lg, _ := logger()
	if err := wpconf.SetHeadsets(dir, []string{jabra, jbl}, true); err != nil {
		t.Fatal(err)
	}
	if got := dropHeadset(dir, "00:11:22:33:44:55", lg); got != "" {
		t.Errorf("dropped a headset not set up: %q", got)
	}
	if got := dropHeadset(dir, strings.ToLower(jbl), lg); got != strings.ToLower(jbl) {
		t.Errorf("dropHeadset = %q", got)
	}
	if got := wpconf.ConfiguredHeadsets(dir); !reflect.DeepEqual(got, []string{jabra}) {
		t.Errorf("headsets = %v", got)
	}
	if !wpconf.ConfiguredPhone(dir) {
		t.Error("phone profile lost")
	}
	// It stays known, so pairing it again does not set it up again.
	known, _ := wpconf.Known(dir)
	if added := wpconf.NewHeadsets([]string{jbl}, []string{jabra}, known, true); added != nil {
		t.Errorf("dropped headset would be set up again: %v", added)
	}
}

func TestToggleHeadset(t *testing.T) {
	dir := t.TempDir()
	lg, out := logger()
	if err := wpconf.SetKnown(dir, []string{jabra}); err != nil {
		t.Fatal(err)
	}
	if err := toggleHeadset(dir, jbl, lg); err != nil {
		t.Fatal(err)
	}
	if got := wpconf.ConfiguredHeadsets(dir); !reflect.DeepEqual(got, []string{jbl}) {
		t.Errorf("headsets = %v", got)
	}
	if known, _ := wpconf.Known(dir); len(known) != 2 {
		t.Errorf("known = %v", known)
	}
	if err := toggleHeadset(dir, jbl, lg); err != nil {
		t.Fatal(err)
	}
	if got := wpconf.ConfiguredHeadsets(dir); got != nil {
		t.Errorf("headsets after toggling back = %v", got)
	}
	if !strings.Contains(out.String(), "set up for calls") || !strings.Contains(out.String(), "no longer set up") {
		t.Errorf("log:\n%s", out)
	}
}

func TestSetUpUser(t *testing.T) {
	unit := install.UnitFile(install.PackagedBinary, "ofono")
	var notes []i18n.Message
	notify := func(m i18n.Message) { notes = append(notes, m) }
	lg, _ := logger()
	never := func(wpsetup.Options) error {
		t.Error("set up without need")
		return nil
	}

	// Without the daemon there is nothing to do.
	if mode := setUpUser(nil, t.TempDir(), lg, notify, never); mode != "" {
		t.Errorf("without the daemon: %q", mode)
	}

	// The mode is set up once, and the user told.
	dir := t.TempDir()
	var got wpsetup.Options
	setup := func(o wpsetup.Options) error {
		got = o
		_, err := wpconf.Install(dir, o.Mode, wpconf.Version{Major: 0, Minor: 5})
		return err
	}
	if mode := setUpUser(unit, dir, lg, notify, setup); mode != "ofono" {
		t.Errorf("mode = %q", mode)
	}
	if got != (wpsetup.Options{Mode: "ofono", FromSession: true}) {
		t.Errorf("setup options = %+v", got)
	}
	if len(notes) != 1 || !strings.Contains(notes[0].BodyEN, "speaker and microphone") {
		t.Errorf("notifications = %v", notes)
	}
	if mode := setUpUser(unit, dir, lg, notify, never); mode != "ofono" {
		t.Errorf("mode when set up = %q", mode)
	}

	// Mode hfp has no speaker and microphone.
	notes = nil
	if mode := setUpUser(install.UnitFile(install.PackagedBinary, "hfp"), t.TempDir(), lg, notify, func(wpsetup.Options) error { return nil }); mode != "hfp" {
		t.Errorf("mode = %q", mode)
	}
	if len(notes) != 1 || strings.Contains(notes[0].BodyEN, "speaker") {
		t.Errorf("notifications in hfp = %v", notes)
	}

	// A failed setup is reported, and the service stops.
	notes = nil
	if mode := setUpUser(unit, t.TempDir(), lg, notify, func(wpsetup.Options) error { return errors.New("no wireplumber") }); mode != "" {
		t.Errorf("mode after a failed setup = %q", mode)
	}
	if len(notes) != 1 || !strings.Contains(notes[0].SummaryEN, "failed") {
		t.Errorf("notifications = %v", notes)
	}
}

func TestProblemMessage(t *testing.T) {
	m := problemMessage(doctor.Finding{Level: doctor.Fail, Text: "service stopped", Fix: "sudo systemctl start bt-headset-hid"})
	if m.SummaryEN != "bt-headset-hid: service stopped" || !strings.HasPrefix(m.BodyEN, "Fix: sudo systemctl") || !strings.HasPrefix(m.BodyDE, "Lösung: sudo systemctl") {
		t.Errorf("problemMessage = %+v", m)
	}
	m = problemMessage(doctor.Finding{Level: doctor.Warn, Text: "x"})
	if m.BodyEN != "Details: bt-headset-hid doctor" {
		t.Errorf("without fix: %+v", m)
	}
}
