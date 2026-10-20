package hiddesc

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
)

// field is one bit of a report as described by a main item.
type field struct {
	page, usage uint16 // 0 for padding
	flags       byte   // main item data, e.g. 0x02 Data,Var,Abs
}

type reportKey struct {
	kind string // "input", "output", "feature"
	id   byte
}

// walk parses the short items of a report descriptor and returns the bit
// layout of every report. Only what Descriptor uses is supported; anything
// else is an error so the test notices when the descriptor grows.
func walk(d []byte) (map[reportKey][]field, error) {
	reports := map[reportKey][]field{}
	var page uint16
	var size, count int
	var id byte
	var usages []uint16
	depth := 0
	for i := 0; i < len(d); {
		prefix := d[i]
		n := int(prefix & 0x03)
		if n == 3 {
			n = 4
		}
		if prefix == 0xFE {
			return nil, fmt.Errorf("offset %d: long items not supported", i)
		}
		if i+1+n > len(d) {
			return nil, fmt.Errorf("offset %d: item truncated", i)
		}
		var v uint32
		for j := 0; j < n; j++ {
			v |= uint32(d[i+1+j]) << (8 * j)
		}
		switch tag := prefix & 0xFC; tag {
		case 0x04: // Usage Page
			page = uint16(v)
		case 0x14, 0x24: // Logical Minimum/Maximum
		case 0x74: // Report Size
			size = int(v)
		case 0x94: // Report Count
			count = int(v)
		case 0x84: // Report ID
			if v == 0 || v > 255 {
				return nil, fmt.Errorf("offset %d: invalid report ID %d", i, v)
			}
			id = byte(v)
		case 0x08: // Usage
			usages = append(usages, uint16(v))
		case 0xA0: // Collection
			if len(usages) > 1 {
				return nil, fmt.Errorf("offset %d: collection with %d usages", i, len(usages))
			}
			depth++
			usages = nil
		case 0xC0: // End Collection
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("offset %d: unbalanced End Collection", i)
			}
		case 0x80, 0x90, 0xB0: // Input, Output, Feature
			kind := map[byte]string{0x80: "input", 0x90: "output", 0xB0: "feature"}[tag]
			if depth == 0 {
				return nil, fmt.Errorf("offset %d: main item outside a collection", i)
			}
			if id == 0 {
				return nil, fmt.Errorf("offset %d: main item without report ID", i)
			}
			if size != 1 {
				return nil, fmt.Errorf("offset %d: report size %d, only bit fields expected", i, size)
			}
			constant := v&0x01 != 0
			if constant != (len(usages) == 0) {
				return nil, fmt.Errorf("offset %d: %d usages for constant=%v", i, len(usages), constant)
			}
			if !constant && len(usages) != count {
				return nil, fmt.Errorf("offset %d: %d usages for report count %d", i, len(usages), count)
			}
			k := reportKey{kind, id}
			for j := 0; j < count; j++ {
				f := field{flags: byte(v)}
				if !constant {
					f.page, f.usage = page, usages[j]
				}
				reports[k] = append(reports[k], f)
			}
			usages = nil
		default:
			return nil, fmt.Errorf("offset %d: unexpected item 0x%02x", i, prefix)
		}
		i += 1 + n
	}
	if depth != 0 {
		return nil, fmt.Errorf("%d collections not closed", depth)
	}
	return reports, nil
}

const (
	dataVarAbs = 0x02
	dataVarRel = 0x06
	pad        = 0x01
)

func TestDescriptorLayout(t *testing.T) {
	reports, err := walk(Descriptor)
	if err != nil {
		t.Fatal(err)
	}
	padding := field{flags: pad}
	want := map[reportKey][]field{
		{"input", ReportTelephony}: {
			{0x0B, 0x20, dataVarAbs}, // Hook Switch
			{0x0B, 0x21, dataVarRel}, // Flash
			{0x0B, 0x24, dataVarRel}, // Redial
			{0x0B, 0x26, dataVarRel}, // Drop
			{0x0B, 0x2F, dataVarRel}, // Phone Mute
			padding, padding, padding,
		},
		{"output", ReportLEDs}: {
			{0x08, 0x17, dataVarAbs}, // Off-Hook
			{0x08, 0x18, dataVarAbs}, // Ring
			{0x08, 0x09, dataVarAbs}, // Mute
			{0x08, 0x20, dataVarAbs}, // Hold
			padding, padding, padding, padding,
		},
		{"input", ReportConsumer}: {
			{0x0C, 0xE9, dataVarAbs}, // Volume Increment
			{0x0C, 0xEA, dataVarAbs}, // Volume Decrement
			padding, padding, padding, padding, padding, padding,
		},
	}
	if !reflect.DeepEqual(reports, want) {
		t.Fatalf("layout\n got %v\nwant %v", reports, want)
	}
	for k, fields := range reports {
		if len(fields)%8 != 0 {
			t.Errorf("%v: %d bits, not byte aligned", k, len(fields))
		}
		if 1+len(fields)/8 != ReportLen {
			t.Errorf("%v: %d bytes with ID, ReportLen is %d", k, 1+len(fields)/8, ReportLen)
		}
	}
}

func TestWalkRejectsBroken(t *testing.T) {
	for name, d := range map[string][]byte{
		"truncated":  Descriptor[:len(Descriptor)-2],
		"unbalanced": append(append([]byte{}, Descriptor...), 0xC0),
		"no id":      {0x05, 0x0B, 0x09, 0x05, 0xA1, 0x01, 0x75, 0x01, 0x95, 0x01, 0x81, 0x01, 0xC0},
	} {
		if _, err := walk(d); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestTelephonyRoundTrip(t *testing.T) {
	cases := map[Telephony]byte{
		{}:                               0x00,
		{HookSwitch: true}:               0x01,
		{Flash: true}:                    0x02,
		{Redial: true}:                   0x04,
		{Drop: true}:                     0x08,
		{Mute: true}:                     0x10,
		{HookSwitch: true, Redial: true}: 0x05,
	}
	for in, b := range cases {
		got := in.Pack()
		if !bytes.Equal(got, []byte{ReportTelephony, b}) {
			t.Errorf("%+v.Pack() = % x", in, got)
		}
		out, err := UnpackTelephony(got)
		if err != nil || out != in {
			t.Errorf("UnpackTelephony(% x) = %+v, %v", got, out, err)
		}
	}
}

func TestLEDsRoundTrip(t *testing.T) {
	cases := map[LEDs]byte{
		{}:              0x00,
		{OffHook: true}: 0x01,
		{Ring: true}:    0x02,
		{Mute: true}:    0x04,
		{Hold: true}:    0x08,
		{OffHook: true, Ring: true, Mute: true, Hold: true}: 0x0F,
	}
	for in, b := range cases {
		got := in.Pack()
		if !bytes.Equal(got, []byte{ReportLEDs, b}) {
			t.Errorf("%+v.Pack() = % x", in, got)
		}
		out, err := UnpackLEDs(got)
		if err != nil || out != in {
			t.Errorf("UnpackLEDs(% x) = %+v, %v", got, out, err)
		}
	}
	// Padding bits set by the host are ignored.
	if l, err := UnpackLEDs([]byte{ReportLEDs, 0xF1}); err != nil || l != (LEDs{OffHook: true}) {
		t.Errorf("padding: %+v, %v", l, err)
	}
}

func TestConsumerRoundTrip(t *testing.T) {
	for in, b := range map[Consumer]byte{{}: 0, {VolumeUp: true}: 1, {VolumeDown: true}: 2} {
		got := in.Pack()
		if !bytes.Equal(got, []byte{ReportConsumer, b}) {
			t.Errorf("%+v.Pack() = % x", in, got)
		}
		out, err := UnpackConsumer(got)
		if err != nil || out != in {
			t.Errorf("UnpackConsumer(% x) = %+v, %v", got, out, err)
		}
	}
}

func TestUnpackRejects(t *testing.T) {
	for _, r := range [][]byte{nil, {ReportLEDs}, {ReportLEDs, 0, 0}, {ReportTelephony, 1}, {0, 1}} {
		if l, err := UnpackLEDs(r); err == nil {
			t.Errorf("UnpackLEDs(% x) = %+v, want error", r, l)
		}
	}
}
