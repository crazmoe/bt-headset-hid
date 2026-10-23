// Package ofono poses as oFono's hands-free audio manager on the D-Bus system
// bus. PipeWire's oFono backend (bluez5.hfphsp-backend = "ofono") then takes
// the call audio of the headsets whose HFP connection this daemon handles,
// and ModemManager stays free for real modems (mobile broadband). Only the
// part of the oFono API that PipeWire uses is provided (see
// spa/plugins/bluez5/backend-ofono.c in PipeWire): the manager with Register,
// Unregister, GetCards and the CardAdded/CardRemoved signals, and cards with
// GetProperties and Acquire.
//
// The same bus name also carries the call state for "bt-headset-hid session"
// (StatusPath), which in mode pipewire reads the modem instead.
package ofono

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"

	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

// D-Bus names of oFono as used by PipeWire, and of the call state.
const (
	Service      = "org.ofono"
	ManagerPath  = dbus.ObjectPath("/")
	ifaceManager = Service + ".HandsfreeAudioManager"
	ifaceCard    = Service + ".HandsfreeAudioCard"

	errInvalidArgs = Service + ".Error.InvalidArguments"
	errInUse       = Service + ".Error.InUse"
	errNotFound    = Service + ".Error.NotFound"
	errNotAllowed  = Service + ".Error.NotAllowed"
	errNotImpl     = Service + ".Error.NotImplemented"
	errFailed      = Service + ".Error.Failed"

	// StatusPath and StatusIface publish the softphone's call state as the
	// property CallState: "idle", "incoming", "outgoing" or "active", and
	// the address of the headset that took the call as CallDevice ("" if
	// none did, e.g. when it was answered in the softphone).
	StatusPath  = dbus.ObjectPath("/bt_headset_hid")
	StatusIface = "io.github.crazmoe.BtHeadsetHid1"
)

// closeDelay is how long our copy of an acquired audio socket stays open
// after Acquire returned. godbus sends the reply, which carries a copy for
// PipeWire, only after the method returned and does not close ours.
const closeDelay = 2 * time.Second

// ErrNameTaken means another process (usually the real oFono) owns org.ofono.
var ErrNameTaken = errors.New("ofono: " + Service + " is already owned, is oFono running?")

// Card is a connected headset whose call audio the agent can take.
type Card struct {
	// Remote and Local are the Bluetooth addresses of the headset and of
	// the adapter, "AA:BB:CC:DD:EE:FF".
	Remote, Local string
	// Acquire opens the audio link with one of codecs (hfp.CodecCVSD,
	// hfp.CodecMSBC; what the agent supports) and returns the connected
	// socket, which the server closes, and the codec in use.
	Acquire func(codecs []byte) (fd int, codec byte, err error)
}

// CardPath is the object path of the card of a BlueZ device, like oFono's.
func CardPath(device dbus.ObjectPath) dbus.ObjectPath { return "/hfp" + device }

// agent is the program that registered for the audio (PipeWire).
type agent struct {
	sender string
	path   dbus.ObjectPath
	codecs []byte
}

// Server publishes the audio manager on a D-Bus connection.
type Server struct {
	conn   *dbus.Conn
	logf   func(format string, args ...any)
	status *prop.Properties

	mu    sync.Mutex
	cards map[dbus.ObjectPath]*Card
	agent *agent
}

// Serve exports the audio manager and the call state on conn (normally the
// system bus) and takes the oFono bus name, which makes PipeWire register.
func Serve(conn *dbus.Conn, logf func(format string, args ...any)) (*Server, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	s := &Server{conn: conn, logf: logf, cards: map[dbus.ObjectPath]*Card{}}
	if err := conn.Export(manager{s}, ManagerPath, ifaceManager); err != nil {
		return nil, err
	}
	if err := conn.Export(introspectable{s}, ManagerPath, introspect.IntrospectData.Name); err != nil {
		return nil, err
	}
	status, err := prop.Export(conn, StatusPath, prop.Map{
		StatusIface: {
			"CallState":  {Value: hfp.Idle.String(), Emit: prop.EmitTrue},
			"CallDevice": {Value: "", Emit: prop.EmitTrue},
		},
	})
	if err != nil {
		return nil, err
	}
	s.status = status
	reply, err := conn.RequestName(Service, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("ofono: request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, ErrNameTaken
	}
	return s, nil
}

// Close gives up the bus name.
func (s *Server) Close() error {
	_, err := s.conn.ReleaseName(Service)
	return err
}

// SetCallState publishes the softphone's call state.
func (s *Server) SetCallState(st hfp.CallState) {
	s.status.SetMust(StatusIface, "CallState", st.String())
}

// SetCallDevice publishes the headset that took the call, "" for none.
func (s *Server) SetCallDevice(addr string) {
	s.status.SetMust(StatusIface, "CallDevice", addr)
}

// AddCard offers the audio of the headset with the BlueZ device path device.
// A card that is already there is replaced.
func (s *Server) AddCard(device dbus.ObjectPath, c *Card) error {
	path := CardPath(device)
	if !path.IsValid() {
		return fmt.Errorf("ofono: bad device path %q", device)
	}
	s.mu.Lock()
	_, had := s.cards[path]
	s.cards[path] = c
	s.mu.Unlock()
	if err := s.conn.Export(card{s, path}, path, ifaceCard); err != nil {
		return err
	}
	if had {
		if err := s.conn.Emit(ManagerPath, ifaceManager+".CardRemoved", path); err != nil {
			return err
		}
	}
	return s.conn.Emit(ManagerPath, ifaceManager+".CardAdded", path, c.properties())
}

// RemoveCard withdraws the card of device, if there is one.
func (s *Server) RemoveCard(device dbus.ObjectPath) error {
	path := CardPath(device)
	s.mu.Lock()
	_, had := s.cards[path]
	delete(s.cards, path)
	s.mu.Unlock()
	if !had {
		return nil
	}
	return errors.Join(
		s.conn.Export(nil, path, ifaceCard),
		s.conn.Emit(ManagerPath, ifaceManager+".CardRemoved", path),
	)
}

// properties are the card properties PipeWire reads; it insists on strings.
func (c *Card) properties() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"RemoteAddress": dbus.MakeVariant(c.Remote),
		"LocalAddress":  dbus.MakeVariant(c.Local),
		// "gateway": we are the Audio Gateway, the remote is the headset.
		"Type": dbus.MakeVariant("gateway"),
	}
}

// hasOwner reports whether a unique bus name is still connected.
func (s *Server) hasOwner(name string) bool {
	var ok bool
	err := s.conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&ok)
	return err != nil || ok // when in doubt, keep the agent
}

// manager implements org.ofono.HandsfreeAudioManager.
type manager struct{ s *Server }

// cardEntry is one element of GetCards, D-Bus type (oa{sv}).
type cardEntry struct {
	Path  dbus.ObjectPath
	Props map[string]dbus.Variant
}

func (m manager) GetCards() ([]cardEntry, *dbus.Error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	cards := []cardEntry{}
	for p, c := range m.s.cards {
		cards = append(cards, cardEntry{p, c.properties()})
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].Path < cards[j].Path })
	return cards, nil
}

func (m manager) Register(sender dbus.Sender, path dbus.ObjectPath, codecs []byte) *dbus.Error {
	if !path.IsValid() || len(codecs) == 0 || len(codecs) > 8 {
		return dbus.NewError(errInvalidArgs, []any{"Invalid arguments in method call"})
	}
	for _, c := range codecs {
		if c != hfp.CodecCVSD && c != hfp.CodecMSBC {
			return dbus.NewError(errInvalidArgs, []any{fmt.Sprintf("unknown codec %d", c)})
		}
	}
	s := m.s
	s.mu.Lock()
	old := s.agent
	s.mu.Unlock()
	// An agent that went away without Unregister (PipeWire restarted)
	// does not block the new one.
	if old != nil && (old.sender == string(sender) || s.hasOwner(old.sender)) {
		return dbus.NewError(errInUse, []any{"an audio agent is already registered"})
	}
	s.mu.Lock()
	s.agent = &agent{sender: string(sender), path: path, codecs: append([]byte(nil), codecs...)}
	s.mu.Unlock()
	s.logf("audio agent %s%s registered, codecs %v", sender, path, codecs)
	return nil
}

func (m manager) Unregister(sender dbus.Sender, path dbus.ObjectPath) *dbus.Error {
	s := m.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agent == nil || s.agent.sender != string(sender) || s.agent.path != path {
		return dbus.NewError(errNotFound, []any{"no such agent"})
	}
	s.agent = nil
	s.logf("audio agent %s%s unregistered", sender, path)
	return nil
}

// card implements org.ofono.HandsfreeAudioCard for one card.
type card struct {
	s    *Server
	path dbus.ObjectPath
}

func (c card) lookup() (*Card, *dbus.Error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	cd := c.s.cards[c.path]
	if cd == nil {
		return nil, dbus.NewError(errNotFound, []any{"no such card"})
	}
	return cd, nil
}

func (c card) GetProperties() (map[string]dbus.Variant, *dbus.Error) {
	cd, err := c.lookup()
	if err != nil {
		return nil, err
	}
	return cd.properties(), nil
}

// Connect would open the audio link and hand it to the agent with
// NewConnection; PipeWire uses Acquire instead.
func (c card) Connect() *dbus.Error {
	return dbus.NewError(errNotImpl, []any{"use Acquire"})
}

// Acquire opens the audio link and returns its socket and codec. Only the
// registered agent may take the audio, since it includes the microphone.
func (c card) Acquire(sender dbus.Sender) (dbus.UnixFD, byte, *dbus.Error) {
	cd, derr := c.lookup()
	if derr != nil {
		return -1, 0, derr
	}
	c.s.mu.Lock()
	ag := c.s.agent
	c.s.mu.Unlock()
	if ag == nil || ag.sender != string(sender) {
		return -1, 0, dbus.NewError(errNotAllowed, []any{"only the registered audio agent may acquire"})
	}
	fd, codec, err := cd.Acquire(ag.codecs)
	if err != nil {
		c.s.logf("audio for %s: %v", cd.Remote, err)
		return -1, 0, dbus.NewError(errFailed, []any{err.Error()})
	}
	c.s.logf("audio for %s: codec %d", cd.Remote, codec)
	time.AfterFunc(closeDelay, func() { syscall.Close(fd) })
	return dbus.UnixFD(fd), codec, nil
}

// introspectable answers Introspect on "/", which PipeWire calls to see
// whether oFono is there.
type introspectable struct{ s *Server }

func (i introspectable) Introspect() (string, *dbus.Error) {
	n := &introspect.Node{
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			{Name: ifaceManager, Methods: introspect.Methods(manager{}), Signals: []introspect.Signal{
				{Name: "CardAdded", Args: []introspect.Arg{{Name: "path", Type: "o"}, {Name: "properties", Type: "a{sv}"}}},
				{Name: "CardRemoved", Args: []introspect.Arg{{Name: "path", Type: "o"}}},
			}},
		},
		Children: []introspect.Node{{Name: strings.TrimPrefix(string(StatusPath), "/")}},
	}
	i.s.mu.Lock()
	if len(i.s.cards) > 0 {
		n.Children = append(n.Children, introspect.Node{Name: "hfp"})
	}
	i.s.mu.Unlock()
	return introspect.NewIntrospectable(n).Introspect()
}
