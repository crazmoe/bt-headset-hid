package uhid

import (
	"bytes"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crazmoe/bt-headset-hid/internal/hiddesc"
)

// Offsets of struct uhid_event members, written out from linux/uhid.h rather
// than derived from the constants in uhid.go.
const (
	offName    = 4
	offPhys    = 132
	offUniq    = 196
	offRDSize  = 260
	offBus     = 262
	offVendor  = 264
	offProduct = 268
	offVersion = 272
	offCountry = 276
	offRDData  = 280
)

func TestEventSize(t *testing.T) {
	if EventSize != 4376 {
		t.Fatalf("EventSize = %d, sizeof(struct uhid_event) is 4376", EventSize)
	}
}

func TestEncodeCreate2(t *testing.T) {
	c := Config{
		Name: "bt-headset-hid", Phys: "00:11:22:33:44:55", Uniq: "aa:bb:cc:dd:ee:ff",
		Bus: BusBluetooth, Vendor: 0x1234, Product: 0x5678, Version: 0x0100, Country: 7,
		Descriptor: hiddesc.Descriptor,
	}
	b, err := EncodeCreate2(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != offRDData+len(c.Descriptor) {
		t.Fatalf("len = %d", len(b))
	}
	if ne.Uint32(b) != 11 {
		t.Errorf("type = %d", ne.Uint32(b))
	}
	str := func(off, n int) string { return strings.TrimRight(string(b[off:off+n]), "\x00") }
	if str(offName, 128) != c.Name || str(offPhys, 64) != c.Phys || str(offUniq, 64) != c.Uniq {
		t.Errorf("strings: %q %q %q", str(offName, 128), str(offPhys, 64), str(offUniq, 64))
	}
	got := []uint32{
		uint32(ne.Uint16(b[offRDSize:])), uint32(ne.Uint16(b[offBus:])),
		ne.Uint32(b[offVendor:]), ne.Uint32(b[offProduct:]), ne.Uint32(b[offVersion:]), ne.Uint32(b[offCountry:]),
	}
	want := []uint32{uint32(len(c.Descriptor)), BusBluetooth, 0x1234, 0x5678, 0x0100, 7}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fields = %#x, want %#x", got, want)
	}
	if !bytes.Equal(b[offRDData:], c.Descriptor) {
		t.Error("descriptor not copied")
	}
}

func TestEncodeCreate2Rejects(t *testing.T) {
	ok := Config{Descriptor: []byte{0x05, 0x0B}}
	for name, mod := range map[string]func(*Config){
		"no descriptor":   func(c *Config) { c.Descriptor = nil },
		"long descriptor": func(c *Config) { c.Descriptor = make([]byte, DescriptorMax+1) },
		"long name":       func(c *Config) { c.Name = strings.Repeat("x", 128) },
		"long phys":       func(c *Config) { c.Phys = strings.Repeat("x", 64) },
		"long uniq":       func(c *Config) { c.Uniq = strings.Repeat("x", 64) },
	} {
		c := ok
		mod(&c)
		if _, err := EncodeCreate2(c); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	c := ok
	c.Name = strings.Repeat("x", 127)
	if _, err := EncodeCreate2(c); err != nil {
		t.Errorf("127 byte name: %v", err)
	}
}

func TestEncodeSmallEvents(t *testing.T) {
	in, err := EncodeInput2([]byte{1, 0x05})
	if err != nil {
		t.Fatal(err)
	}
	if ne.Uint32(in) != 12 || ne.Uint16(in[4:]) != 2 || !bytes.Equal(in[6:], []byte{1, 0x05}) {
		t.Errorf("input2 = % x", in)
	}
	if _, err := EncodeInput2(nil); err == nil {
		t.Error("empty input report accepted")
	}
	if _, err := EncodeInput2(make([]byte, DataMax+1)); err == nil {
		t.Error("oversized input report accepted")
	}
	if d := EncodeDestroy(); ne.Uint32(d) != 1 || len(d) != 4 {
		t.Errorf("destroy = % x", d)
	}

	r, err := EncodeGetReportReply(42, 0, []byte{2, 1})
	if err != nil {
		t.Fatal(err)
	}
	if ne.Uint32(r) != 10 || ne.Uint32(r[4:]) != 42 || ne.Uint16(r[8:]) != 0 || ne.Uint16(r[10:]) != 2 || !bytes.Equal(r[12:], []byte{2, 1}) {
		t.Errorf("get-report reply = % x", r)
	}
	r, _ = EncodeGetReportReply(43, syscall.EIO, []byte{2, 1})
	if ne.Uint16(r[8:]) != uint16(syscall.EIO) || ne.Uint16(r[10:]) != 0 || len(r) != 12 {
		t.Errorf("get-report error reply = % x", r)
	}

	s := EncodeSetReportReply(7, syscall.EINVAL)
	if ne.Uint32(s) != 14 || ne.Uint32(s[4:]) != 7 || ne.Uint16(s[8:]) != uint16(syscall.EINVAL) {
		t.Errorf("set-report reply = % x", s)
	}
}

func event(typ EventType, fill func(p []byte)) []byte {
	b := make([]byte, EventSize)
	ne.PutUint32(b, uint32(typ))
	if fill != nil {
		fill(b[4:])
	}
	return b
}

func TestDecode(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want Event
	}{
		{"start", event(Start, func(p []byte) { ne.PutUint64(p, NumberedOutputReports|NumberedInputReports) }),
			Event{Type: Start, Flags: NumberedOutputReports | NumberedInputReports}},
		{"open", event(Open, nil), Event{Type: Open}},
		{"output", event(Output, func(p []byte) {
			copy(p, []byte{hiddesc.ReportLEDs, 0x03})
			ne.PutUint16(p[4096:], 2)
			p[4098] = byte(OutputReport)
		}), Event{Type: Output, Data: []byte{hiddesc.ReportLEDs, 0x03}, RType: OutputReport}},
		{"get-report", event(GetReport, func(p []byte) {
			ne.PutUint32(p, 99)
			p[4], p[5] = 2, byte(OutputReport)
		}), Event{Type: GetReport, ID: 99, RNum: 2, RType: OutputReport}},
		{"set-report", event(SetReport, func(p []byte) {
			ne.PutUint32(p, 5)
			p[4], p[5] = 2, byte(OutputReport)
			ne.PutUint16(p[6:], 2)
			copy(p[8:], []byte{2, 0x01})
		}), Event{Type: SetReport, ID: 5, RNum: 2, RType: OutputReport, Data: []byte{2, 0x01}}},
		// The kernel may send short events; the rest counts as zero.
		{"short start", event(Start, nil)[:4], Event{Type: Start}},
		{"unknown", event(200, nil), Event{Type: 200}},
	}
	for _, c := range cases {
		got, err := Decode(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got.Data != nil && len(got.Data) == 0 {
			got.Data = nil
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	for name, b := range map[string][]byte{
		"empty":      nil,
		"no type":    {1, 0},
		"output":     event(Output, func(p []byte) { ne.PutUint16(p[4096:], 4097) }),
		"set-report": event(SetReport, func(p []byte) { ne.PutUint16(p[6:], 4097) }),
	} {
		if e, err := Decode(b); err == nil {
			t.Errorf("%s: %+v, want error", name, e)
		}
	}
}

func TestDecodeCopiesData(t *testing.T) {
	b := event(Output, func(p []byte) { p[0] = 2; ne.PutUint16(p[4096:], 1) })
	e, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	b[4] = 9
	if e.Data[0] != 2 {
		t.Error("Data aliases the read buffer")
	}
}

// fakeUHID records writes and returns queued events from Read.
type fakeUHID struct {
	writes [][]byte
	reads  [][]byte
	closed bool
}

func (f *fakeUHID) Write(b []byte) (int, error) {
	f.writes = append(f.writes, append([]byte(nil), b...))
	return len(b), nil
}

func (f *fakeUHID) Read(b []byte) (int, error) {
	if len(f.reads) == 0 {
		return 0, io.EOF
	}
	n := copy(b, f.reads[0])
	f.reads = f.reads[1:]
	return n, nil
}

func (f *fakeUHID) Close() error { f.closed = true; return nil }

func TestDevice(t *testing.T) {
	f := &fakeUHID{reads: [][]byte{
		event(Start, func(p []byte) { ne.PutUint64(p, NumberedOutputReports) }),
		event(SetReport, func(p []byte) { ne.PutUint32(p, 3); ne.PutUint16(p[6:], 2); copy(p[8:], []byte{2, 1}) }),
	}}
	d, err := New(f, Config{Name: "test", Descriptor: hiddesc.Descriptor})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 1 || EventType(ne.Uint32(f.writes[0])) != Create2 {
		t.Fatalf("writes after New: %d", len(f.writes))
	}
	if e, err := d.ReadEvent(); err != nil || e.Type != Start || e.Flags != NumberedOutputReports {
		t.Fatalf("first event %+v, %v", e, err)
	}
	e, err := d.ReadEvent()
	if err != nil || e.Type != SetReport || !bytes.Equal(e.Data, []byte{2, 1}) {
		t.Fatalf("second event %+v, %v", e, err)
	}
	if err := d.ReplySetReport(e.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := d.Input(hiddesc.Telephony{HookSwitch: true}.Pack()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ReadEvent(); !errors.Is(err, io.EOF) {
		t.Fatalf("read at end: %v", err)
	}
	if err := d.Close(); err != nil || !f.closed {
		t.Fatalf("Close: %v, closed %v", err, f.closed)
	}
	var types []EventType
	for _, w := range f.writes {
		types = append(types, EventType(ne.Uint32(w)))
	}
	if want := []EventType{Create2, SetReportReply, Input2, Destroy}; !reflect.DeepEqual(types, want) {
		t.Errorf("writes %v, want %v", types, want)
	}
}

// TestKernel creates the real device when /dev/uhid is accessible (root or
// the uhid group); everywhere else, including CI, it is skipped.
func TestKernel(t *testing.T) {
	d, err := OpenDevice(Config{Name: "bt-headset-hid test", Bus: BusVirtual, Descriptor: hiddesc.Descriptor})
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		t.Skipf("no access to %s: %v", Path, err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	done := make(chan Event, 1)
	go func() {
		for {
			e, err := d.ReadEvent()
			if err != nil || e.Type == Start {
				done <- e
				return
			}
		}
	}()
	select {
	case e := <-done:
		if e.Type != Start {
			t.Fatal("no start event")
		}
		// The descriptor has numbered input and output reports, no feature reports.
		if want := uint64(NumberedOutputReports | NumberedInputReports); e.Flags&want != want {
			t.Errorf("start flags %#x, want numbered reports", e.Flags)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for start event")
	}
	if err := d.Input(hiddesc.Telephony{HookSwitch: true}.Pack()); err != nil {
		t.Fatal(err)
	}
}
