package modem

import (
	"errors"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

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
