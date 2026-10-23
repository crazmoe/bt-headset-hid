// Package bluez registers the HFP and HSP Audio Gateway profiles with BlueZ
// (org.bluez.ProfileManager1) and hands out the RFCOMM sockets of connecting
// headsets, which is only needed in the mode without audio, where this daemon
// instead of PipeWire talks HFP to the headset. Headsets lists the paired
// headsets for the PipeWire configuration.
package bluez

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

const (
	service        = "org.bluez"
	managerPath    = dbus.ObjectPath("/org/bluez")
	ifaceManager   = "org.bluez.ProfileManager1"
	ifaceProfile   = "org.bluez.Profile1"
	errRejected    = "org.bluez.Error.Rejected"
	errInvalidArgs = "org.bluez.Error.InvalidArguments"

	// UUIDs of the Audio Gateway roles.
	UUIDHFPAG = "0000111f-0000-1000-8000-00805f9b34fb"
	UUIDHSPAG = "00001112-0000-1000-8000-00805f9b34fb"
)

// profileSpec describes how one profile is registered.
type profileSpec struct {
	path    dbus.ObjectPath
	uuid    string
	name    string
	channel uint16
	opts    map[string]dbus.Variant
}

func spec(p hfp.Profile, features int) profileSpec {
	// The SDP features are the low bits of the +BRSF features, plus
	// wideband speech (bit 5) where we negotiate the codec.
	sdp := features & 0x1f
	if features&hfp.FeatureCodecNegotiation != 0 {
		sdp |= 0x20
	}
	if p == hfp.HSP {
		return profileSpec{
			path: "/org/bluez/bt_headset_hid/hsp_ag", uuid: UUIDHSPAG, name: "bt-headset-hid HSP AG", channel: 12,
		}
	}
	return profileSpec{
		path: "/org/bluez/bt_headset_hid/hfp_ag", uuid: UUIDHFPAG, name: "bt-headset-hid HFP AG", channel: 13,
		opts: map[string]dbus.Variant{
			"Version":  dbus.MakeVariant(uint16(0x0107)),
			"Features": dbus.MakeVariant(uint16(sdp)),
		},
	}
}

// ErrProfileInUse means another program already registered the Audio
// Gateway profile with BlueZ, usually PipeWire, PulseAudio or oFono.
var ErrProfileInUse = errors.New("another program (usually PipeWire) already provides the HFP/HSP Audio Gateway; " +
	"disable it there (\"bt-headset-hid wireplumber\" with the same -mode, see README) or use -mode pipewire")

func isAlreadyExists(err error) bool {
	var e dbus.Error
	if errors.As(err, &e) && e.Name == "org.bluez.Error.AlreadyExists" {
		return true
	}
	// Older BlueZ versions only say so in the message.
	return strings.Contains(err.Error(), "already registered")
}

// Connection is a headset that connected to one of our profiles.
type Connection struct {
	Device  dbus.ObjectPath // e.g. /org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF
	Profile hfp.Profile
	// File is the RFCOMM socket; the receiver owns and closes it. It is also
	// closed when BlueZ asks to disconnect, which ends pending reads.
	File *os.File
}

// Address returns the Bluetooth address encoded in the device path, or "".
func (c Connection) Address() string { return AddressFromPath(c.Device) }

// AddressFromPath turns .../dev_AA_BB_CC_DD_EE_FF into AA:BB:CC:DD:EE:FF.
func AddressFromPath(p dbus.ObjectPath) string {
	s := string(p)
	i := strings.LastIndex(s, "/dev_")
	if i < 0 {
		return ""
	}
	addr := strings.ReplaceAll(s[i+5:], "_", ":")
	if len(addr) != 17 {
		return ""
	}
	return addr
}

// LocalAddress returns the address of the adapter that device (a BlueZ
// device path) belongs to.
func LocalAddress(conn *dbus.Conn, device dbus.ObjectPath) (string, error) {
	i := strings.LastIndex(string(device), "/")
	if i <= 0 {
		return "", fmt.Errorf("bluez: no adapter in %s", device)
	}
	v, err := conn.Object(service, device[:i]).GetProperty("org.bluez.Adapter1.Address")
	if err != nil {
		return "", fmt.Errorf("bluez: adapter address: %w", err)
	}
	addr, _ := v.Value().(string)
	if len(addr) != 17 {
		return "", fmt.Errorf("bluez: adapter address %q", addr)
	}
	return strings.ToUpper(addr), nil
}

// Profiles is the set of registered profiles.
type Profiles struct {
	conn  *dbus.Conn
	specs []profileSpec
	ch    chan Connection

	mu    sync.Mutex
	files map[dbus.ObjectPath]*os.File // by device, to close on RequestDisconnection
}

// Register registers the given Audio Gateway profiles with BlueZ on conn
// (the system bus). features are the HFP AG features (see hfp.DefaultFeatures).
// New connections arrive on Connections.
func Register(conn *dbus.Conn, features int, profiles ...hfp.Profile) (*Profiles, error) {
	ps := &Profiles{conn: conn, ch: make(chan Connection, 4), files: map[dbus.ObjectPath]*os.File{}}
	for _, p := range profiles {
		sp := spec(p, features)
		if err := conn.Export(profile{ps, p}, sp.path, ifaceProfile); err != nil {
			return nil, err
		}
		opts := map[string]dbus.Variant{
			"Name":                  dbus.MakeVariant(sp.name),
			"Role":                  dbus.MakeVariant("server"),
			"Channel":               dbus.MakeVariant(sp.channel),
			"RequireAuthentication": dbus.MakeVariant(true),
			"RequireAuthorization":  dbus.MakeVariant(false),
			"AutoConnect":           dbus.MakeVariant(true),
		}
		for k, v := range sp.opts {
			opts[k] = v
		}
		err := conn.Object(service, managerPath).Call(ifaceManager+".RegisterProfile", 0, sp.path, sp.uuid, opts).Err
		if err != nil {
			ps.Close()
			if isAlreadyExists(err) {
				err = ErrProfileInUse
			}
			return nil, fmt.Errorf("bluez: register %s: %w", sp.name, err)
		}
		ps.specs = append(ps.specs, sp)
	}
	return ps, nil
}

// Connections delivers headsets as they connect.
func (ps *Profiles) Connections() <-chan Connection { return ps.ch }

// Close unregisters the profiles. Open connections are not closed.
func (ps *Profiles) Close() error {
	var errs []error
	for _, sp := range ps.specs {
		errs = append(errs, ps.conn.Object(service, managerPath).Call(ifaceManager+".UnregisterProfile", 0, sp.path).Err)
		errs = append(errs, ps.conn.Export(nil, sp.path, ifaceProfile))
	}
	ps.specs = nil
	return errors.Join(errs...)
}

// Forget drops the bookkeeping for a device after its connection ended.
func (ps *Profiles) Forget(device dbus.ObjectPath, f *os.File) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.files[device] == f {
		delete(ps.files, device)
	}
}

// profile implements org.bluez.Profile1 for one profile path.
type profile struct {
	ps *Profiles
	p  hfp.Profile
}

func (profile) Release() *dbus.Error { return nil }

func (pr profile) NewConnection(device dbus.ObjectPath, fd dbus.UnixFD, _ map[string]dbus.Variant) *dbus.Error {
	if fd < 0 {
		return dbus.NewError(errInvalidArgs, []any{"no file descriptor"})
	}
	// Non-blocking, so the Go poller handles the socket and Close ends a
	// pending Read.
	if err := syscall.SetNonblock(int(fd), true); err != nil {
		syscall.Close(int(fd))
		return dbus.MakeFailedError(err)
	}
	f := os.NewFile(uintptr(fd), "rfcomm:"+string(device))
	ps := pr.ps
	ps.mu.Lock()
	if old := ps.files[device]; old != nil {
		// Only one connection per headset; a reconnect replaces the old one.
		old.Close()
	}
	ps.files[device] = f
	ps.mu.Unlock()
	select {
	case ps.ch <- Connection{Device: device, Profile: pr.p, File: f}:
		return nil
	default:
		ps.Forget(device, f)
		f.Close()
		return dbus.NewError(errRejected, []any{"too many pending connections"})
	}
}

func (pr profile) RequestDisconnection(device dbus.ObjectPath) *dbus.Error {
	ps := pr.ps
	ps.mu.Lock()
	f := ps.files[device]
	delete(ps.files, device)
	ps.mu.Unlock()
	if f != nil {
		f.Close()
	}
	return nil
}
