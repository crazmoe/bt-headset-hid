package modem

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/crazmoe/bt-headset-hid/internal/dbustest"
	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

func nextSignal(t *testing.T, ch <-chan *dbus.Signal, name string) *dbus.Signal {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case s := <-ch:
			if s.Name == name {
				return s
			}
		case <-timeout:
			t.Fatalf("timeout waiting for %s", name)
		}
	}
}

// TestPipeWireView drives the server the way PipeWire's modemmanager.c does.
func TestPipeWireView(t *testing.T) {
	addr := dbustest.Bus(t)
	events := make(chan hfp.Event, 4)
	srv, err := Serve(dbustest.Connect(t, addr), func(e hfp.Event) { events <- e })
	if err != nil {
		t.Fatal(err)
	}

	pw := dbustest.Connect(t, addr)
	for _, member := range []string{"CallAdded", "CallDeleted"} {
		if err := pw.AddMatchSignal(dbus.WithMatchSender(Service), dbus.WithMatchInterface(ifaceVoice), dbus.WithMatchMember(member)); err != nil {
			t.Fatal(err)
		}
	}
	if err := pw.AddMatchSignal(dbus.WithMatchSender(Service), dbus.WithMatchInterface(ifaceCall), dbus.WithMatchMember("StateChanged")); err != nil {
		t.Fatal(err)
	}
	sigs := make(chan *dbus.Signal, 16)
	pw.Signal(sigs)

	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := pw.Object(Service, RootPath).Call(ifaceObjMgr+".GetManagedObjects", 0).Store(&objs); err != nil {
		t.Fatal(err)
	}
	modem := objs[ModemPath]
	if modem == nil || modem[ifaceVoice] == nil || modem[iface3gpp] == nil {
		t.Fatalf("managed objects: %v", objs)
	}
	if st := modem[ifaceModem]["State"].Value(); st != modemStateRegistered {
		t.Errorf("modem State = %v", st)
	}
	// A (ub) struct decodes as []any{uint32, bool}; PipeWire reads the u.
	if sq, ok := modem[ifaceModem]["SignalQuality"].Value().([]any); !ok || len(sq) != 2 || sq[0] != uint32(100) || sq[1] != true {
		t.Errorf("SignalQuality = %#v", modem[ifaceModem]["SignalQuality"].Value())
	}

	// Ringing: PipeWire sees CallAdded and reads the call properties.
	if err := srv.SetState(hfp.Incoming); err != nil {
		t.Fatal(err)
	}
	added := nextSignal(t, sigs, ifaceVoice+".CallAdded")
	path := added.Body[0].(dbus.ObjectPath)
	var props map[string]dbus.Variant
	if err := pw.Object(Service, path).Call(ifaceProps+".GetAll", 0, ifaceCall).Store(&props); err != nil {
		t.Fatal(err)
	}
	if props["State"].Value() != int32(CallRingingIn) || props["Direction"].Value() != int32(DirectionIncoming) {
		t.Fatalf("call properties %v", props)
	}

	// The headset sends ATA: PipeWire calls Accept.
	if err := pw.Object(Service, path).Call(ifaceCall+".Accept", 0).Err; err != nil {
		t.Fatal(err)
	}
	if e := <-events; e.Kind != hfp.Answer {
		t.Fatalf("event %v", e.Kind)
	}
	changed := nextSignal(t, sigs, ifaceCall+".StateChanged")
	if changed.Path != path || len(changed.Body) != 3 ||
		changed.Body[0] != int32(CallRingingIn) || changed.Body[1] != int32(CallActive) || changed.Body[2] != uint32(ReasonAccepted) {
		t.Fatalf("StateChanged %v %v", changed.Path, changed.Body)
	}
	if err := pw.Object(Service, path).Call(ifaceCall+".Accept", 0).Err; err == nil {
		t.Fatal("second Accept succeeded")
	}

	// The softphone hangs up.
	if err := srv.SetState(hfp.Active); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetState(hfp.Idle); err != nil {
		t.Fatal(err)
	}
	if d := nextSignal(t, sigs, ifaceVoice+".CallDeleted"); d.Body[0] != path {
		t.Fatalf("CallDeleted %v", d.Body)
	}
	if err := pw.Object(Service, path).Call(ifaceProps+".GetAll", 0, ifaceCall).Err; err == nil {
		t.Fatal("deleted call still exported")
	}
	var calls []dbus.ObjectPath
	if err := pw.Object(Service, ModemPath).Call(ifaceVoice+".ListCalls", 0).Store(&calls); err != nil || len(calls) != 0 {
		t.Fatalf("ListCalls %v, %v", calls, err)
	}
	if err := pw.Object(Service, ModemPath).Call(ifaceVoice+".CreateCall", 0, map[string]dbus.Variant{}).Err; err == nil {
		t.Fatal("CreateCall succeeded")
	}
}

func TestNameTaken(t *testing.T) {
	addr := dbustest.Bus(t)
	if _, err := Serve(dbustest.Connect(t, addr), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Serve(dbustest.Connect(t, addr), nil); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("second Serve: %v", err)
	}
}

// TestIntrospection walks the tree from "/" the way busctl tree does.
func TestIntrospection(t *testing.T) {
	addr := dbustest.Bus(t)
	srv, err := Serve(dbustest.Connect(t, addr), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.SetState(hfp.Incoming); err != nil {
		t.Fatal(err)
	}
	cl := dbustest.Connect(t, addr)
	found := map[dbus.ObjectPath][]string{}
	var walk func(p dbus.ObjectPath)
	walk = func(p dbus.ObjectPath) {
		n, err := introspect.Call(cl.Object(Service, p))
		if err != nil {
			t.Fatalf("introspect %s: %v", p, err)
		}
		for _, i := range n.Interfaces {
			found[p] = append(found[p], i.Name)
		}
		for _, c := range n.Children {
			child := p + "/" + dbus.ObjectPath(c.Name)
			if p == "/" {
				child = "/" + dbus.ObjectPath(c.Name)
			}
			walk(child)
		}
	}
	walk("/")
	want := map[dbus.ObjectPath][]string{
		RootPath:          {introspect.IntrospectData.Name, ifaceObjMgr},
		ModemPath:         {introspect.IntrospectData.Name, ifaceProps, ifaceModem, iface3gpp, ifaceVoice},
		CallPrefix + "/1": {introspect.IntrospectData.Name, ifaceProps, ifaceCall},
	}
	for p, ifaces := range want {
		if !reflect.DeepEqual(found[p], ifaces) {
			t.Errorf("%s: interfaces %v, want %v", p, found[p], ifaces)
		}
	}

	// A deleted call disappears from the tree.
	if err := srv.SetState(hfp.Idle); err != nil {
		t.Fatal(err)
	}
	n, err := introspect.Call(cl.Object(Service, CallPrefix))
	if err != nil || len(n.Children) != 0 {
		t.Fatalf("calls after hang-up: %+v, %v", n, err)
	}
}

// TestInterfacesAddedOnRestart: PipeWire 1.0 only learns about a modem that
// appears after its start from InterfacesAdded, e.g. when the daemon restarts.
func TestInterfacesAddedOnRestart(t *testing.T) {
	addr := dbustest.Bus(t)
	pw := dbustest.Connect(t, addr)
	if err := pw.AddMatchSignal(dbus.WithMatchSender(Service), dbus.WithMatchInterface(ifaceObjMgr), dbus.WithMatchMember("InterfacesAdded")); err != nil {
		t.Fatal(err)
	}
	sigs := make(chan *dbus.Signal, 4)
	pw.Signal(sigs)

	for i := 0; i < 2; i++ {
		conn := dbustest.Connect(t, addr)
		if _, err := Serve(conn, nil); err != nil {
			t.Fatal(err)
		}
		s := nextSignal(t, sigs, ifaceObjMgr+".InterfacesAdded")
		if s.Path != RootPath || len(s.Body) != 2 || s.Body[0] != ModemPath {
			t.Fatalf("InterfacesAdded %v %v", s.Path, s.Body)
		}
		ifaces, ok := s.Body[1].(map[string]map[string]dbus.Variant)
		if !ok || ifaces[ifaceModem] == nil || ifaces[iface3gpp] == nil || ifaces[ifaceVoice] == nil {
			t.Fatalf("interfaces %#v", s.Body[1])
		}
		conn.Close() // the daemon stops; the name goes away
	}
}
