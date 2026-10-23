package daemon

import (
	"bufio"
	"context"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

// connect hands h an HSP connection for device and returns the headset's end.
func connect(t *testing.T, conns chan bluez.Connection, device dbus.ObjectPath) headset {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Non-blocking like bluez hands it out, so Close ends a pending Read.
	if err := syscall.SetNonblock(fds[0], true); err != nil {
		t.Fatal(err)
	}
	ours := os.NewFile(uintptr(fds[0]), "ours")
	f := os.NewFile(uintptr(fds[1]), "theirs")
	theirs, err := net.FileConn(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { theirs.Close() })
	conns <- bluez.Connection{Device: device, Profile: hfp.HSP, File: ours}
	return headset{t, theirs, bufio.NewReader(theirs)}
}

// gone reads until the session closed the headset's end.
func (h headset) gone() {
	h.t.Helper()
	h.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		if _, err := h.r.ReadString('\n'); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				h.t.Fatal("connection still open")
			}
			return
		}
	}
}

// startHeadsets serves headsets; the channels receive the device of each
// onConnected, onClosed and HSP button press.
func startHeadsets(t *testing.T, all bool) (h *headsets, conns chan bluez.Connection, opened, closed, pressed chan dbus.ObjectPath) {
	conns = make(chan bluez.Connection)
	opened = make(chan dbus.ObjectPath, 4)
	closed = make(chan dbus.ObjectPath, 4)
	pressed = make(chan dbus.ObjectPath, 4)
	h = &headsets{
		bridge: NewBridge(&fakeHID{}), conns: conns, forget: func(dbus.ObjectPath, *os.File) {},
		// Not t.Logf: sessions end after the test.
		features: hfp.DefaultFeatures, all: all, logf: func(string, ...any) {},
		onConnected: func(c bluez.Connection, _ *Session) { opened <- c.Device },
		onClosed:    func(c bluez.Connection) { closed <- c.Device },
		onEvent: func(c bluez.Connection, e hfp.Event) {
			if e.Kind == hfp.Button {
				pressed <- c.Device
			}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.serve(ctx)
	return h, conns, opened, closed, pressed
}

func expectDevice(t *testing.T, ch chan dbus.ObjectPath, want dbus.ObjectPath) {
	t.Helper()
	select {
	case d := <-ch:
		if d != want {
			t.Fatalf("device %s, want %s", d, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no call for %s", want)
	}
}

func TestHeadsetsAll(t *testing.T) {
	const jabra, jbl = dbus.ObjectPath("/org/bluez/hci0/dev_50_C2_75_0D_4E_03"), dbus.ObjectPath("/org/bluez/hci0/dev_84_D3_52_8C_B9_D0")
	h, conns, opened, closed, pressed := startHeadsets(t, true)
	a := connect(t, conns, jabra)
	expectDevice(t, opened, jabra)
	b := connect(t, conns, jbl)
	expectDevice(t, opened, jbl)

	// Both ring.
	h.setState(hfp.Incoming)
	a.expect("RING")
	b.expect("RING")
	h.setState(hfp.Idle)

	// Button presses say which headset they come from.
	b.send("AT+CKPD=200")
	b.expect("OK")
	expectDevice(t, pressed, jbl)

	// A reconnect replaces the old connection without closing the card.
	a2 := connect(t, conns, jabra)
	expectDevice(t, opened, jabra)
	a.gone()
	select {
	case d := <-closed:
		t.Fatalf("onClosed(%s) for a replaced connection", d)
	case <-time.After(100 * time.Millisecond):
	}

	b.conn.Close()
	expectDevice(t, closed, jbl)
	h.setState(hfp.Incoming)
	a2.expect("RING")
}

func TestHeadsetsLatest(t *testing.T) {
	const jabra, jbl = dbus.ObjectPath("/org/bluez/hci0/dev_50_C2_75_0D_4E_03"), dbus.ObjectPath("/org/bluez/hci0/dev_84_D3_52_8C_B9_D0")
	h, conns, opened, _, _ := startHeadsets(t, false)
	a := connect(t, conns, jabra)
	expectDevice(t, opened, jabra)
	b := connect(t, conns, jbl)
	expectDevice(t, opened, jbl)
	a.gone()
	h.setState(hfp.Incoming)
	b.expect("RING")
}
