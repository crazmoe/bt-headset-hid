package modem

import (
	"errors"
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"

	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

// D-Bus names of ModemManager as used by PipeWire.
const (
	Service      = "org.freedesktop.ModemManager1"
	RootPath     = dbus.ObjectPath("/org/freedesktop/ModemManager1")
	ModemPath    = RootPath + "/Modem/0"
	CallPrefix   = RootPath + "/Call"
	ifaceModem   = Service + ".Modem"
	iface3gpp    = Service + ".Modem.Modem3gpp"
	ifaceVoice   = Service + ".Modem.Voice"
	ifaceCall    = Service + ".Call"
	ifaceObjMgr  = "org.freedesktop.DBus.ObjectManager"
	ifaceProps   = "org.freedesktop.DBus.Properties"
	errNotSupp   = Service + ".Error.Core.Unsupported"
	errWrongStat = Service + ".Error.Core.WrongState"
	errNotFound  = Service + ".Error.Core.NotFound"
)

// MMModemState and MMModem3gppRegistrationState values we report: a modem
// registered on its home network, so the headset shows service.
const (
	modemStateRegistered = int32(8)
	regStateHome         = uint32(1)
)

// ErrNameTaken means another process (usually the real ModemManager) owns
// org.freedesktop.ModemManager1.
var ErrNameTaken = errors.New("modem: " + Service + " is already owned, is ModemManager running?")

// Server publishes the modem on a D-Bus connection.
type Server struct {
	conn    *dbus.Conn
	onEvent func(hfp.Event)
	evMu    sync.Mutex // serializes onEvent; godbus runs each method call in its own goroutine

	mu        sync.Mutex
	calls     *Calls
	modem     *prop.Properties
	callProps map[dbus.ObjectPath]*prop.Properties
}

// Serve exports the modem on conn (normally the system bus) and takes the
// ModemManager bus name. onEvent receives Answer and HangUp when the headset
// presses its button; it is called from D-Bus goroutines, but never
// concurrently with itself.
func Serve(conn *dbus.Conn, onEvent func(hfp.Event)) (*Server, error) {
	s := &Server{
		conn:      conn,
		onEvent:   onEvent,
		calls:     NewCalls(string(CallPrefix)),
		callProps: map[dbus.ObjectPath]*prop.Properties{},
	}
	ro := func(v any) *prop.Prop { return &prop.Prop{Value: v, Emit: prop.EmitTrue} }
	props, err := prop.Export(conn, ModemPath, prop.Map{
		ifaceModem: {
			"State":               ro(modemStateRegistered),
			"Manufacturer":        ro("bt-headset-hid"),
			"Model":               ro("HID bridge"),
			"Revision":            ro("1"),
			"EquipmentIdentifier": ro("bt-headset-hid"),
			"Device":              ro("bt-headset-hid"),
			"OwnNumbers":          ro([]string{}),
			"SignalQuality": ro(struct {
				Percent uint32
				Recent  bool
			}{100, true}),
		},
		iface3gpp: {
			"OperatorName":      ro("bt-headset-hid"),
			"RegistrationState": ro(regStateHome),
		},
		ifaceVoice: {
			"Calls": ro([]dbus.ObjectPath{}),
		},
	})
	if err != nil {
		return nil, err
	}
	s.modem = props
	if err := conn.Export(voice{s}, ModemPath, ifaceVoice); err != nil {
		return nil, err
	}
	if err := conn.Export(objectManager{s}, RootPath, ifaceObjMgr); err != nil {
		return nil, err
	}
	for _, p := range staticPaths {
		if err := s.exportIntrospectable(p); err != nil {
			return nil, err
		}
	}
	reply, err := conn.RequestName(Service, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("modem: request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, ErrNameTaken
	}
	return s, nil
}

// SetState follows the softphone's call state, see Calls.SetState.
func (s *Server) SetState(st hfp.CallState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publish(s.calls.SetState(st))
}

// Close releases the bus name; the connection stays open.
func (s *Server) Close() error {
	_, err := s.conn.ReleaseName(Service)
	return err
}

// publish exports, updates and removes call objects and emits the signals
// PipeWire listens for. Must be called with s.mu held.
func (s *Server) publish(sigs []Signal) error {
	var errs []error
	for _, sig := range sigs {
		path := dbus.ObjectPath(sig.Call.Path)
		switch sig.Kind {
		case Added:
			if err := s.exportCall(sig.Call); err != nil {
				errs = append(errs, err)
				continue
			}
			s.modem.SetMust(ifaceVoice, "Calls", s.callPaths())
			errs = append(errs, s.conn.Emit(ModemPath, ifaceVoice+".CallAdded", path))
		case StateChanged:
			if p := s.callProps[path]; p != nil {
				p.SetMust(ifaceCall, "State", int32(sig.Call.State))
				p.SetMust(ifaceCall, "StateReason", int32(sig.Call.Reason))
			}
			errs = append(errs, s.conn.Emit(path, ifaceCall+".StateChanged",
				int32(sig.OldState), int32(sig.Call.State), uint32(sig.Call.Reason)))
		case Deleted:
			delete(s.callProps, path)
			s.modem.SetMust(ifaceVoice, "Calls", s.callPaths())
			errs = append(errs, s.conn.Emit(ModemPath, ifaceVoice+".CallDeleted", path))
			errs = append(errs, s.conn.Export(nil, path, ifaceCall), s.conn.Export(nil, path, ifaceProps),
				s.conn.Export(nil, path, introspect.IntrospectData.Name))
		}
	}
	return errors.Join(errs...)
}

func (s *Server) exportCall(c Call) error {
	path := dbus.ObjectPath(c.Path)
	ro := func(v any) *prop.Prop { return &prop.Prop{Value: v, Emit: prop.EmitTrue} }
	p, err := prop.Export(s.conn, path, prop.Map{ifaceCall: {
		"State":       ro(int32(c.State)),
		"StateReason": ro(int32(c.Reason)),
		"Direction":   ro(int32(c.Direction)),
		"Number":      ro(""),
	}})
	if err != nil {
		return err
	}
	if err := s.conn.Export(call{s, path}, path, ifaceCall); err != nil {
		return err
	}
	if err := s.exportIntrospectable(path); err != nil {
		return err
	}
	s.callProps[path] = p
	return nil
}

func (s *Server) callPaths() []dbus.ObjectPath {
	paths := []dbus.ObjectPath{}
	for p := range s.callProps {
		paths = append(paths, p)
	}
	return paths
}

func (s *Server) handle(f func() ([]Signal, *hfp.Event, error)) *dbus.Error {
	// evMu is taken first so events reach onEvent in the order the calls
	// changed state.
	s.evMu.Lock()
	defer s.evMu.Unlock()
	s.mu.Lock()
	sigs, ev, err := f()
	if err == nil {
		err = s.publish(sigs)
	}
	s.mu.Unlock()
	switch {
	case errors.Is(err, ErrNoCall):
		return dbus.NewError(errNotFound, []any{err.Error()})
	case errors.Is(err, ErrWrongState):
		return dbus.NewError(errWrongStat, []any{err.Error()})
	case err != nil:
		return dbus.MakeFailedError(err)
	}
	if ev != nil && s.onEvent != nil {
		s.onEvent(*ev)
	}
	return nil
}

// call implements org.freedesktop.ModemManager1.Call for one call object.
type call struct {
	s    *Server
	path dbus.ObjectPath
}

func (c call) Accept() *dbus.Error {
	return c.s.handle(func() ([]Signal, *hfp.Event, error) { return c.s.calls.Accept(string(c.path)) })
}

func (c call) Hangup() *dbus.Error {
	return c.s.handle(func() ([]Signal, *hfp.Event, error) { return c.s.calls.Hangup(string(c.path)) })
}

func (c call) SendDtmf(string) *dbus.Error {
	return dbus.NewError(errNotSupp, []any{"DTMF is not supported"})
}

// voice implements the methods of org.freedesktop.ModemManager1.Modem.Voice.
type voice struct{ s *Server }

// CreateCall is what PipeWire calls for ATD and AT+BLDN; the softphone cannot
// be told to dial, so it is refused.
func (voice) CreateCall(map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	return "/", dbus.NewError(errNotSupp, []any{"dialing from the headset is not supported"})
}

func (v voice) ListCalls() ([]dbus.ObjectPath, *dbus.Error) {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	return v.s.callPaths(), nil
}

// objectManager implements org.freedesktop.DBus.ObjectManager on RootPath.
type objectManager struct{ s *Server }

func (o objectManager) GetManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, *dbus.Error) {
	o.s.mu.Lock()
	defer o.s.mu.Unlock()
	objs := map[dbus.ObjectPath]map[string]map[string]dbus.Variant{}
	modem := map[string]map[string]dbus.Variant{}
	for _, iface := range []string{ifaceModem, iface3gpp, ifaceVoice} {
		all, err := o.s.modem.GetAll(iface)
		if err != nil {
			return nil, err
		}
		modem[iface] = all
	}
	objs[ModemPath] = modem
	for path, p := range o.s.callProps {
		all, err := p.GetAll(ifaceCall)
		if err != nil {
			return nil, err
		}
		objs[path] = map[string]map[string]dbus.Variant{ifaceCall: all}
	}
	return objs, nil
}
