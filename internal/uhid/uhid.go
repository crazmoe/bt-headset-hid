// Package uhid creates a virtual HID device through the Linux /dev/uhid
// interface (see linux/uhid.h). The kernel then exposes it as /dev/hidrawN.
//
// The event encoding is separate from the file so it can be tested without
// the kernel: a Device works on any io.ReadWriteCloser.
package uhid

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
)

// EventType is the type field of struct uhid_event.
type EventType uint32

// Event types; the numbering skips the legacy events of linux/uhid.h.
const (
	Destroy        EventType = 1
	Start          EventType = 2
	Stop           EventType = 3
	Open           EventType = 4
	Close          EventType = 5
	Output         EventType = 6
	GetReport      EventType = 9
	GetReportReply EventType = 10
	Create2        EventType = 11
	Input2         EventType = 12
	SetReport      EventType = 13
	SetReportReply EventType = 14
)

var typeNames = map[EventType]string{
	Destroy: "destroy", Start: "start", Stop: "stop", Open: "open", Close: "close",
	Output: "output", GetReport: "get-report", GetReportReply: "get-report-reply",
	Create2: "create2", Input2: "input2", SetReport: "set-report", SetReportReply: "set-report-reply",
}

func (t EventType) String() string {
	if s, ok := typeNames[t]; ok {
		return s
	}
	return fmt.Sprintf("uhid-event(%d)", uint32(t))
}

// ReportType is the rtype field of output, get-report and set-report events.
type ReportType uint8

const (
	FeatureReport ReportType = 0
	OutputReport  ReportType = 1
	InputReport   ReportType = 2
)

// Start flags: the kernel includes the report ID in the data of these report
// types. Always set for devices with numbered reports.
const (
	NumberedFeatureReports = 1 << 0
	NumberedOutputReports  = 1 << 1
	NumberedInputReports   = 1 << 2
)

// Bus types from linux/input.h.
const (
	BusUSB       = 0x03
	BusBluetooth = 0x05
	BusVirtual   = 0x06
)

// Sizes from linux/uhid.h and linux/hid.h.
const (
	DataMax       = 4096 // UHID_DATA_MAX
	DescriptorMax = 4096 // HID_MAX_DESCRIPTOR_SIZE
	nameLen       = 128
	physLen       = 64
	uniqLen       = 64

	// EventSize is sizeof(struct uhid_event); the largest member is create2.
	EventSize = 4 + nameLen + physLen + uniqLen + 2 + 2 + 4*4 + DescriptorMax
)

// Config describes the device for UHID_CREATE2.
type Config struct {
	Name       string // shown as HID_NAME, at most 127 bytes
	Phys       string // e.g. the adapter address, at most 63 bytes
	Uniq       string // e.g. the headset address, at most 63 bytes
	Bus        uint16
	Vendor     uint32
	Product    uint32
	Version    uint32
	Country    uint32
	Descriptor []byte
}

// Event is an event read from the kernel. Which fields are set depends on Type:
// Start sets Flags; Output sets Data and RType; GetReport sets ID, RNum and
// RType; SetReport sets ID, RNum, RType and Data.
type Event struct {
	Type  EventType
	Flags uint64
	ID    uint32
	RNum  uint8
	RType ReportType
	Data  []byte
}

var ne = binary.NativeEndian

func putString(b []byte, s string, field string) error {
	if len(s) >= len(b) {
		return fmt.Errorf("uhid: %s is %d bytes, at most %d allowed", field, len(s), len(b)-1)
	}
	copy(b, s)
	return nil
}

// EncodeCreate2 returns a UHID_CREATE2 event. Unused trailing descriptor
// bytes are left out; the kernel zero-fills short writes.
func EncodeCreate2(c Config) ([]byte, error) {
	if len(c.Descriptor) == 0 || len(c.Descriptor) > DescriptorMax {
		return nil, fmt.Errorf("uhid: descriptor is %d bytes, want 1..%d", len(c.Descriptor), DescriptorMax)
	}
	b := make([]byte, EventSize-DescriptorMax+len(c.Descriptor))
	ne.PutUint32(b, uint32(Create2))
	p := b[4:]
	if err := putString(p[:nameLen], c.Name, "name"); err != nil {
		return nil, err
	}
	p = p[nameLen:]
	if err := putString(p[:physLen], c.Phys, "phys"); err != nil {
		return nil, err
	}
	p = p[physLen:]
	if err := putString(p[:uniqLen], c.Uniq, "uniq"); err != nil {
		return nil, err
	}
	p = p[uniqLen:]
	ne.PutUint16(p[0:], uint16(len(c.Descriptor)))
	ne.PutUint16(p[2:], c.Bus)
	ne.PutUint32(p[4:], c.Vendor)
	ne.PutUint32(p[8:], c.Product)
	ne.PutUint32(p[12:], c.Version)
	ne.PutUint32(p[16:], c.Country)
	copy(p[20:], c.Descriptor)
	return b, nil
}

// EncodeInput2 returns a UHID_INPUT2 event carrying one input report.
func EncodeInput2(report []byte) ([]byte, error) {
	if len(report) == 0 || len(report) > DataMax {
		return nil, fmt.Errorf("uhid: input report is %d bytes, want 1..%d", len(report), DataMax)
	}
	b := make([]byte, 4+2+len(report))
	ne.PutUint32(b, uint32(Input2))
	ne.PutUint16(b[4:], uint16(len(report)))
	copy(b[6:], report)
	return b, nil
}

// EncodeDestroy returns a UHID_DESTROY event.
func EncodeDestroy() []byte {
	b := make([]byte, 4)
	ne.PutUint32(b, uint32(Destroy))
	return b
}

// EncodeGetReportReply answers a GetReport event. errno 0 means success and
// data is the report including its ID; otherwise data is ignored.
func EncodeGetReportReply(id uint32, errno syscall.Errno, data []byte) ([]byte, error) {
	if errno != 0 {
		data = nil
	}
	if len(data) > DataMax {
		return nil, fmt.Errorf("uhid: get-report reply is %d bytes, at most %d", len(data), DataMax)
	}
	b := make([]byte, 4+4+2+2+len(data))
	ne.PutUint32(b, uint32(GetReportReply))
	ne.PutUint32(b[4:], id)
	ne.PutUint16(b[8:], uint16(errno))
	ne.PutUint16(b[10:], uint16(len(data)))
	copy(b[12:], data)
	return b, nil
}

// EncodeSetReportReply answers a SetReport event; errno 0 means success.
func EncodeSetReportReply(id uint32, errno syscall.Errno) []byte {
	b := make([]byte, 4+4+2)
	ne.PutUint32(b, uint32(SetReportReply))
	ne.PutUint32(b[4:], id)
	ne.PutUint16(b[8:], uint16(errno))
	return b
}

var errShort = errors.New("uhid: event truncated")

// Decode parses an event read from the kernel. Short events are treated as
// zero-filled, as linux/uhid.h asks of user space. Data is copied, so b may be
// reused. Unknown event types are returned with only Type set.
func Decode(b []byte) (Event, error) {
	if len(b) < 4 {
		return Event{}, errShort
	}
	if len(b) < EventSize {
		b = append(b[:len(b):len(b)], make([]byte, EventSize-len(b))...)
	}
	e := Event{Type: EventType(ne.Uint32(b))}
	p := b[4:]
	switch e.Type {
	case Start:
		e.Flags = ne.Uint64(p)
	case Output:
		// struct uhid_output_req: data[4096], size u16, rtype u8.
		n := int(ne.Uint16(p[DataMax:]))
		if n > DataMax {
			return Event{}, fmt.Errorf("uhid: output size %d", n)
		}
		e.Data = append([]byte(nil), p[:n]...)
		e.RType = ReportType(p[DataMax+2])
	case GetReport:
		e.ID = ne.Uint32(p)
		e.RNum = p[4]
		e.RType = ReportType(p[5])
	case SetReport:
		e.ID = ne.Uint32(p)
		e.RNum = p[4]
		e.RType = ReportType(p[5])
		n := int(ne.Uint16(p[6:]))
		if n > DataMax {
			return Event{}, fmt.Errorf("uhid: set-report size %d", n)
		}
		e.Data = append([]byte(nil), p[8:8+n]...)
	}
	return e, nil
}

// Device is a created uhid device. Input and the reply methods may be called
// concurrently with ReadEvent.
type Device struct {
	rw  io.ReadWriteCloser
	mu  sync.Mutex // serializes writes; each event must be one write
	buf []byte
}

// Path is the uhid character device.
const Path = "/dev/uhid"

// OpenDevice opens /dev/uhid and creates the device described by c.
func OpenDevice(c Config) (*Device, error) {
	f, err := os.OpenFile(Path, os.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	d, err := New(f, c)
	if err != nil {
		f.Close()
		return nil, err
	}
	return d, nil
}

// New creates the device described by c on rw, normally an open /dev/uhid.
func New(rw io.ReadWriteCloser, c Config) (*Device, error) {
	b, err := EncodeCreate2(c)
	if err != nil {
		return nil, err
	}
	d := &Device{rw: rw, buf: make([]byte, EventSize)}
	if err := d.write(b); err != nil {
		return nil, fmt.Errorf("uhid: create: %w", err)
	}
	return d, nil
}

func (d *Device) write(b []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	n, err := d.rw.Write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	return err
}

// Input sends one input report, including its report ID.
func (d *Device) Input(report []byte) error {
	b, err := EncodeInput2(report)
	if err != nil {
		return err
	}
	return d.write(b)
}

// ReplyGetReport answers a GetReport event.
func (d *Device) ReplyGetReport(id uint32, errno syscall.Errno, data []byte) error {
	b, err := EncodeGetReportReply(id, errno, data)
	if err != nil {
		return err
	}
	return d.write(b)
}

// ReplySetReport answers a SetReport event.
func (d *Device) ReplySetReport(id uint32, errno syscall.Errno) error {
	return d.write(EncodeSetReportReply(id, errno))
}

// ReadEvent blocks until the kernel sends an event. It must not be called
// concurrently with itself.
func (d *Device) ReadEvent() (Event, error) {
	n, err := d.rw.Read(d.buf)
	if err != nil {
		return Event{}, err
	}
	return Decode(d.buf[:n])
}

// Close destroys the device and closes the underlying file.
func (d *Device) Close() error {
	werr := d.write(EncodeDestroy())
	cerr := d.rw.Close()
	if werr != nil {
		return werr
	}
	return cerr
}
