// Package hiddesc holds the HID report descriptor of the virtual device and
// packs/unpacks its reports. The layout follows common USB headset dongles so
// softphones (WebHID, Teams, Zoom, ...) treat the device like one.
//
//	Report 1, input:  Telephony page, Headset collection
//	  bit 0 Hook Switch (absolute), bit 1 Flash, bit 2 Redial, bit 3 Drop,
//	  bit 4 Phone Mute (relative: 1 on press, 0 on release), bits 5-7 padding
//	Report 2, output: LED page, inside the Headset collection
//	  bit 0 Off-Hook, bit 1 Ring, bit 2 Mute, bit 3 Hold, bits 4-7 padding
//	Report 3, input:  Consumer page, Consumer Control collection
//	  bit 0 Volume Increment, bit 1 Volume Decrement, bits 2-7 padding
//
// Every report is two bytes on the wire: the report ID followed by the bits.
package hiddesc

import "fmt"

// Report IDs.
const (
	ReportTelephony = 1
	ReportLEDs      = 2
	ReportConsumer  = 3
)

// ReportLen is the length of every report including the report ID byte.
const ReportLen = 2

// Descriptor is the HID report descriptor passed to UHID_CREATE2.
var Descriptor = []byte{
	0x05, 0x0B, // Usage Page (Telephony)
	0x09, 0x05, // Usage (Headset)
	0xA1, 0x01, // Collection (Application)
	0x85, ReportTelephony, //   Report ID (1)
	0x15, 0x00, //   Logical Minimum (0)
	0x25, 0x01, //   Logical Maximum (1)
	0x75, 0x01, //   Report Size (1)
	0x09, 0x20, //   Usage (Hook Switch)
	0x95, 0x01, //   Report Count (1)
	0x81, 0x02, //   Input (Data,Var,Abs)
	0x09, 0x21, //   Usage (Flash)
	0x09, 0x24, //   Usage (Redial)
	0x09, 0x26, //   Usage (Drop)
	0x09, 0x2F, //   Usage (Phone Mute)
	0x95, 0x04, //   Report Count (4)
	0x81, 0x06, //   Input (Data,Var,Rel)
	0x95, 0x03, //   Report Count (3)
	0x81, 0x01, //   Input (Const) padding
	0x85, ReportLEDs, //   Report ID (2)
	0x05, 0x08, //   Usage Page (LED)
	0x09, 0x17, //   Usage (Off-Hook)
	0x09, 0x18, //   Usage (Ring)
	0x09, 0x09, //   Usage (Mute)
	0x09, 0x20, //   Usage (Hold)
	0x95, 0x04, //   Report Count (4)
	0x91, 0x02, //   Output (Data,Var,Abs)
	0x95, 0x04, //   Report Count (4)
	0x91, 0x01, //   Output (Const) padding
	0xC0, // End Collection

	0x05, 0x0C, // Usage Page (Consumer)
	0x09, 0x01, // Usage (Consumer Control)
	0xA1, 0x01, // Collection (Application)
	0x85, ReportConsumer, //   Report ID (3)
	0x15, 0x00, //   Logical Minimum (0)
	0x25, 0x01, //   Logical Maximum (1)
	0x75, 0x01, //   Report Size (1)
	0x09, 0xE9, //   Usage (Volume Increment)
	0x09, 0xEA, //   Usage (Volume Decrement)
	0x95, 0x02, //   Report Count (2)
	0x81, 0x02, //   Input (Data,Var,Abs)
	0x95, 0x06, //   Report Count (6)
	0x81, 0x01, //   Input (Const) padding
	0xC0, // End Collection
}

// Telephony is input report 1.
type Telephony struct {
	HookSwitch bool
	Flash      bool
	Redial     bool
	Drop       bool
	Mute       bool
}

// LEDs is output report 2, set by the host.
type LEDs struct {
	OffHook bool
	Ring    bool
	Mute    bool
	Hold    bool
}

// Consumer is input report 3.
type Consumer struct {
	VolumeUp   bool
	VolumeDown bool
}

func bits(b ...bool) byte {
	var v byte
	for i, set := range b {
		if set {
			v |= 1 << i
		}
	}
	return v
}

func bit(v byte, i int) bool { return v&(1<<i) != 0 }

// unpack checks the report ID and length and returns the payload byte.
// Padding bits are ignored.
func unpack(r []byte, id byte) (byte, error) {
	if len(r) != ReportLen {
		return 0, fmt.Errorf("hiddesc: report %d has length %d, want %d", id, len(r), ReportLen)
	}
	if r[0] != id {
		return 0, fmt.Errorf("hiddesc: report ID %d, want %d", r[0], id)
	}
	return r[1], nil
}

// Pack returns the input report including the report ID.
func (t Telephony) Pack() []byte {
	return []byte{ReportTelephony, bits(t.HookSwitch, t.Flash, t.Redial, t.Drop, t.Mute)}
}

// UnpackTelephony parses input report 1.
func UnpackTelephony(r []byte) (Telephony, error) {
	v, err := unpack(r, ReportTelephony)
	return Telephony{bit(v, 0), bit(v, 1), bit(v, 2), bit(v, 3), bit(v, 4)}, err
}

// Pack returns the output report including the report ID.
func (l LEDs) Pack() []byte {
	return []byte{ReportLEDs, bits(l.OffHook, l.Ring, l.Mute, l.Hold)}
}

// UnpackLEDs parses output report 2 as received from the host (UHID_OUTPUT
// or UHID_SET_REPORT data, which starts with the report ID).
func UnpackLEDs(r []byte) (LEDs, error) {
	v, err := unpack(r, ReportLEDs)
	return LEDs{bit(v, 0), bit(v, 1), bit(v, 2), bit(v, 3)}, err
}

// Pack returns the input report including the report ID.
func (c Consumer) Pack() []byte {
	return []byte{ReportConsumer, bits(c.VolumeUp, c.VolumeDown)}
}

// UnpackConsumer parses input report 3.
func UnpackConsumer(r []byte) (Consumer, error) {
	v, err := unpack(r, ReportConsumer)
	return Consumer{bit(v, 0), bit(v, 1)}, err
}
