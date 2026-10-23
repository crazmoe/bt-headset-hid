package bluez

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/crazmoe/bt-headset-hid/internal/dbustest"
	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

// fakeManager plays org.bluez.ProfileManager1.
type fakeManager struct {
	mu           sync.Mutex
	registered   map[dbus.ObjectPath]string // path -> uuid
	opts         map[dbus.ObjectPath]map[string]dbus.Variant
	unregistered []dbus.ObjectPath
}

func (m *fakeManager) RegisterProfile(path dbus.ObjectPath, uuid string, opts map[string]dbus.Variant) *dbus.Error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.registered {
		if u == uuid {
			return dbus.NewError("org.bluez.Error.AlreadyExists", []any{"UUID already registered"})
		}
	}
	m.registered[path] = uuid
	m.opts[path] = opts
	return nil
}

func (m *fakeManager) UnregisterProfile(path dbus.ObjectPath) *dbus.Error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unregistered = append(m.unregistered, path)
	return nil
}

func fakeBlueZ(t *testing.T, addr string) (*dbus.Conn, *fakeManager) {
	conn := dbustest.Connect(t, addr)
	m := &fakeManager{registered: map[dbus.ObjectPath]string{}, opts: map[dbus.ObjectPath]map[string]dbus.Variant{}}
	if err := conn.Export(m, managerPath, ifaceManager); err != nil {
		t.Fatal(err)
	}
	if r, err := conn.RequestName(service, 0); err != nil || r != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("RequestName: %v %v", r, err)
	}
	return conn, m
}

func TestAddressFromPath(t *testing.T) {
	for in, want := range map[dbus.ObjectPath]string{
		"/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF": "AA:BB:CC:DD:EE:FF",
		"/org/bluez/hci0":                       "",
		"/org/bluez/hci0/dev_AA_BB":             "",
	} {
		if got := AddressFromPath(in); got != want {
			t.Errorf("AddressFromPath(%s) = %q", in, got)
		}
	}
}

func TestRegisterAndConnect(t *testing.T) {
	addr := dbustest.Bus(t)
	bluez, mgr := fakeBlueZ(t, addr)
	conn := dbustest.Connect(t, addr)

	ps, err := Register(conn, hfp.DefaultFeatures, hfp.HFP, hfp.HSP)
	if err != nil {
		t.Fatal(err)
	}
	hfpPath := spec(hfp.HFP, 0).path
	mgr.mu.Lock()
	if mgr.registered[hfpPath] != UUIDHFPAG || mgr.registered[spec(hfp.HSP, 0).path] != UUIDHSPAG {
		t.Fatalf("registered %v", mgr.registered)
	}
	opts := mgr.opts[hfpPath]
	if _, ok := opts["Role"]; ok || opts["Channel"].Value() != uint16(13) ||
		opts["Features"].Value() != uint16(hfp.DefaultFeatures&0x1f) {
		t.Errorf("HFP options %v", opts)
	}
	mgr.mu.Unlock()

	// BlueZ hands over one end of the RFCOMM "socket".
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	headset := os.NewFile(uintptr(fds[0]), "headset")
	defer headset.Close()
	device := dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF")
	call := bluez.Object(conn.Names()[0], hfpPath).Call(ifaceProfile+".NewConnection", 0,
		device, dbus.UnixFD(fds[1]), map[string]dbus.Variant{})
	syscall.Close(fds[1]) // the daemon got its own copy
	if call.Err != nil {
		t.Fatal(call.Err)
	}

	var c Connection
	select {
	case c = <-ps.Connections():
	case <-time.After(5 * time.Second):
		t.Fatal("no connection")
	}
	if c.Device != device || c.Profile != hfp.HFP || c.Address() != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("connection %+v", c)
	}
	if _, err := headset.Write([]byte("AT+BRSF=959\r")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := c.File.Read(buf)
	if err != nil || string(buf[:n]) != "AT+BRSF=959\r" {
		t.Fatalf("read %q, %v", buf[:n], err)
	}

	// RequestDisconnection closes the socket, which ends a pending read.
	done := make(chan error, 1)
	go func() { _, err := c.File.Read(buf); done <- err }()
	if err := bluez.Object(conn.Names()[0], hfpPath).Call(ifaceProfile+".RequestDisconnection", 0, device).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("read after disconnect: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read not interrupted by RequestDisconnection")
	}

	if err := ps.Close(); err != nil {
		t.Fatal(err)
	}
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.unregistered) != 2 {
		t.Errorf("unregistered %v", mgr.unregistered)
	}
}

func TestRegisterFails(t *testing.T) {
	addr := dbustest.Bus(t)
	// No BlueZ on the bus.
	if _, err := Register(dbustest.Connect(t, addr), hfp.DefaultFeatures, hfp.HFP); err == nil {
		t.Fatal("Register without BlueZ succeeded")
	}
}

func TestProfileInUse(t *testing.T) {
	addr := dbustest.Bus(t)
	fakeBlueZ(t, addr)
	// PipeWire got there first.
	if _, err := Register(dbustest.Connect(t, addr), hfp.DefaultFeatures, hfp.HFP); err != nil {
		t.Fatal(err)
	}
	_, err := Register(dbustest.Connect(t, addr), hfp.DefaultFeatures, hfp.HFP, hfp.HSP)
	if !errors.Is(err, ErrProfileInUse) {
		t.Fatalf("second Register: %v", err)
	}
}

func TestSpecWideband(t *testing.T) {
	if f := spec(hfp.HFP, hfp.DefaultFeatures).opts["Features"].Value(); f != uint16(hfp.DefaultFeatures&0x1f) {
		t.Errorf("features = %v", f)
	}
	f := spec(hfp.HFP, hfp.DefaultFeatures|hfp.FeatureCodecNegotiation).opts["Features"].Value()
	if f != uint16(hfp.DefaultFeatures&0x1f|0x20) {
		t.Errorf("features with codec negotiation = %v, want wideband speech (0x20) too", f)
	}
}

func TestLocalAddress(t *testing.T) {
	addr := dbustest.Bus(t)
	bluez, _ := fakeBlueZ(t, addr)
	_, err := prop.Export(bluez, "/org/bluez/hci0", prop.Map{
		"org.bluez.Adapter1": {"Address": {Value: "00:1a:7d:da:71:13"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn := dbustest.Connect(t, addr)
	got, err := LocalAddress(conn, "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF")
	if err != nil || got != "00:1A:7D:DA:71:13" {
		t.Fatalf("LocalAddress = %q, %v", got, err)
	}
	if _, err := LocalAddress(conn, "/org/bluez/hci1/dev_AA_BB_CC_DD_EE_FF"); err == nil {
		t.Fatal("no error for a missing adapter")
	}
}
