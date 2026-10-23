package ofono

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/dbustest"
	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

const device = dbus.ObjectPath("/org/bluez/hci0/dev_50_C2_75_0D_4E_03")

func errName(err error) string {
	var e dbus.Error
	if errors.As(err, &e) {
		return e.Name
	}
	return ""
}

// waitSignal returns the next signal named member, or fails after a second.
func waitSignal(t *testing.T, ch <-chan *dbus.Signal, member string) *dbus.Signal {
	t.Helper()
	timeout := time.After(time.Second)
	for {
		select {
		case s := <-ch:
			if s.Name == member {
				return s
			}
		case <-timeout:
			t.Fatalf("no signal %s", member)
		}
	}
}

func TestServer(t *testing.T) {
	addr := dbustest.Bus(t)
	srv, err := Serve(dbustest.Connect(t, addr), t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	// PipeWire checks that oFono is there with Introspect on "/".
	pw := dbustest.Connect(t, addr)
	var xml string
	if err := pw.Object(Service, ManagerPath).Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&xml); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(xml, ifaceManager) || !strings.Contains(xml, `"Register"`) {
		t.Fatalf("introspection lacks the manager:\n%s", xml)
	}
	sigs := make(chan *dbus.Signal, 16)
	pw.Signal(sigs)
	if err := pw.AddMatchSignal(dbus.WithMatchSender(Service)); err != nil {
		t.Fatal(err)
	}
	mgr := pw.Object(Service, ManagerPath)
	if err := mgr.Call(ifaceManager+".Register", 0, dbus.ObjectPath("/Profile/ofono"), []byte{3}).Err; errName(err) != errInvalidArgs {
		t.Fatalf("Register with codec 3: %v", err)
	}
	if err := mgr.Call(ifaceManager+".Register", 0, dbus.ObjectPath("/Profile/ofono"), []byte{1, 2}).Err; err != nil {
		t.Fatal(err)
	}
	var cards []cardEntry
	if err := mgr.Call(ifaceManager+".GetCards", 0).Store(&cards); err != nil || len(cards) != 0 {
		t.Fatalf("GetCards = %v, %v", cards, err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// The server closes the descriptor it gets; hand it a copy of w.
	wfd, err := syscall.Dup(int(w.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	gotCodecs := make(chan []byte, 1)
	err = srv.AddCard(device, &Card{Remote: "50:C2:75:0D:4E:03", Local: "00:1A:7D:DA:71:13",
		Acquire: func(codecs []byte) (int, byte, error) {
			gotCodecs <- codecs
			return wfd, hfp.CodecMSBC, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	s := waitSignal(t, sigs, ifaceManager+".CardAdded")
	path := CardPath(device)
	if s.Body[0] != path {
		t.Fatalf("CardAdded %v", s.Body)
	}
	props := s.Body[1].(map[string]dbus.Variant)
	if props["RemoteAddress"].Value() != "50:C2:75:0D:4E:03" || props["LocalAddress"].Value() != "00:1A:7D:DA:71:13" || props["Type"].Value() != "gateway" {
		t.Fatalf("card properties %v", props)
	}
	if err := mgr.Call(ifaceManager+".GetCards", 0).Store(&cards); err != nil || len(cards) != 1 || cards[0].Path != path {
		t.Fatalf("GetCards = %v, %v", cards, err)
	}

	// Someone else may not take the microphone.
	other := dbustest.Connect(t, addr)
	if err := other.Object(Service, path).Call(ifaceCard+".Acquire", 0).Err; errName(err) != errNotAllowed {
		t.Fatalf("Acquire by another program: %v", err)
	}
	var fd dbus.UnixFD
	var codec byte
	if err := pw.Object(Service, path).Call(ifaceCard+".Acquire", 0).Store(&fd, &codec); err != nil {
		t.Fatal(err)
	}
	if c := <-gotCodecs; codec != hfp.CodecMSBC || string(c) != "\x01\x02" {
		t.Fatalf("Acquire codec %d, agent codecs %v", codec, c)
	}
	// The descriptor PipeWire got is the pipe's write end.
	f := os.NewFile(uintptr(fd), "acquired")
	if _, err := f.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	f.Close()
	buf := make([]byte, 1)
	if _, err := r.Read(buf); err != nil || buf[0] != 'x' {
		t.Fatalf("read %q, %v", buf, err)
	}

	// A second agent is refused while the first is connected.
	if err := other.Object(Service, ManagerPath).Call(ifaceManager+".Register", 0, dbus.ObjectPath("/Profile/ofono"), []byte{1}).Err; errName(err) != errInUse {
		t.Fatalf("second Register: %v", err)
	}

	srv.SetCallState(hfp.Incoming)
	var st string
	if err := pw.Object(Service, StatusPath).Call("org.freedesktop.DBus.Properties.Get", 0, StatusIface, "CallState").Store(&st); err != nil || st != "incoming" {
		t.Fatalf("CallState = %q, %v", st, err)
	}

	if err := srv.RemoveCard(device); err != nil {
		t.Fatal(err)
	}
	if s := waitSignal(t, sigs, ifaceManager+".CardRemoved"); s.Body[0] != path {
		t.Fatalf("CardRemoved %v", s.Body)
	}
	if err := pw.Object(Service, path).Call(ifaceCard+".Acquire", 0).Err; err == nil {
		t.Fatal("Acquire on a removed card")
	}
	if err := srv.RemoveCard(device); err != nil {
		t.Fatalf("removing twice: %v", err)
	}

	if err := mgr.Call(ifaceManager+".Unregister", 0, dbus.ObjectPath("/Profile/other")).Err; errName(err) != errNotFound {
		t.Fatalf("Unregister of another path: %v", err)
	}
	if err := mgr.Call(ifaceManager+".Unregister", 0, dbus.ObjectPath("/Profile/ofono")).Err; err != nil {
		t.Fatal(err)
	}
}

// TestAgentGone checks that an agent that disconnected without Unregister,
// like a restarted PipeWire, does not block the next one.
func TestAgentGone(t *testing.T) {
	addr := dbustest.Bus(t)
	srv, err := Serve(dbustest.Connect(t, addr), t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	first, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Object(Service, ManagerPath).Call(ifaceManager+".Register", 0, dbus.ObjectPath("/Profile/ofono"), []byte{1}).Err; err != nil {
		t.Fatal(err)
	}
	if err := first.Object(Service, ManagerPath).Call(ifaceManager+".Register", 0, dbus.ObjectPath("/Profile/ofono"), []byte{1}).Err; errName(err) != errInUse {
		t.Fatalf("registering twice: %v", err)
	}
	first.Close()
	second := dbustest.Connect(t, addr)
	if err := second.Object(Service, ManagerPath).Call(ifaceManager+".Register", 0, dbus.ObjectPath("/Profile/ofono"), []byte{1}).Err; err != nil {
		t.Fatalf("Register after the first agent left: %v", err)
	}
}

func TestNameTaken(t *testing.T) {
	addr := dbustest.Bus(t)
	if r, err := dbustest.Connect(t, addr).RequestName(Service, 0); err != nil || r != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("RequestName: %v %v", r, err)
	}
	if _, err := Serve(dbustest.Connect(t, addr), nil); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("Serve = %v, want ErrNameTaken", err)
	}
}

func TestCardPath(t *testing.T) {
	if p := CardPath(device); p != "/hfp/org/bluez/hci0/dev_50_C2_75_0D_4E_03" || !p.IsValid() {
		t.Fatalf("CardPath = %s", p)
	}
}
