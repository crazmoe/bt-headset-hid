package daemon

import (
	"bufio"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/crazmoe/bt-headset-hid/internal/hfp"
	"github.com/crazmoe/bt-headset-hid/internal/hiddesc"
	"github.com/crazmoe/bt-headset-hid/internal/uhid"
)

// fakeHID replays queued kernel events and records what the bridge sends.
type fakeHID struct {
	mu     sync.Mutex
	events []uhid.Event
	inputs [][]byte
	getRep map[uint32][]byte
	errnos map[uint32]syscall.Errno
}

func (f *fakeHID) Input(r []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, r)
	return nil
}

func (f *fakeHID) ReplyGetReport(id uint32, errno syscall.Errno, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getRep[id], f.errnos[id] = data, errno
	return nil
}

func (f *fakeHID) ReplySetReport(id uint32, errno syscall.Errno) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errnos[id] = errno
	return nil
}

func (f *fakeHID) ReadEvent() (uhid.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.events) == 0 {
		return uhid.Event{}, io.EOF
	}
	e := f.events[0]
	f.events = f.events[1:]
	return e, nil
}

func TestBridge(t *testing.T) {
	ring := hiddesc.LEDs{Ring: true}.Pack()
	f := &fakeHID{
		getRep: map[uint32][]byte{}, errnos: map[uint32]syscall.Errno{},
		events: []uhid.Event{
			{Type: uhid.Start},
			{Type: uhid.Output, RType: uhid.OutputReport, Data: ring},
			{Type: uhid.Output, RType: uhid.OutputReport, Data: []byte{9, 9}}, // ignored
			{Type: uhid.SetReport, ID: 1, RType: uhid.OutputReport, Data: hiddesc.LEDs{OffHook: true}.Pack()},
			{Type: uhid.SetReport, ID: 2, RType: uhid.FeatureReport, Data: []byte{4, 0}},
			{Type: uhid.GetReport, ID: 3, RNum: hiddesc.ReportLEDs, RType: uhid.OutputReport},
			{Type: uhid.GetReport, ID: 4, RNum: 7, RType: uhid.FeatureReport},
		},
	}
	b := NewBridge(f)
	var states []hfp.CallState
	b.OnState(func(s hfp.CallState) { states = append(states, s) })
	var logged []string
	err := b.ServeHID(func(format string, args ...any) { logged = append(logged, format) })
	if err != io.EOF {
		t.Fatalf("ServeHID = %v", err)
	}
	if want := []hfp.CallState{hfp.Incoming, hfp.Active}; !reflect.DeepEqual(states, want) {
		t.Errorf("states %v, want %v", states, want)
	}
	if len(logged) != 1 {
		t.Errorf("logged %q", logged)
	}
	if f.errnos[1] != 0 || f.errnos[2] != syscall.EIO || f.errnos[3] != 0 || f.errnos[4] != syscall.EINVAL {
		t.Errorf("errnos %v", f.errnos)
	}
	if want := (hiddesc.LEDs{OffHook: true}).Pack(); !reflect.DeepEqual(f.getRep[3], want) {
		t.Errorf("get report LEDs % x", f.getRep[3])
	}

	// Off-hook now (from the LED), so the HSP button hangs up.
	if err := b.HeadsetEvent(hfp.Event{Kind: hfp.Button}); err != nil {
		t.Fatal(err)
	}
	if want := [][]byte{{hiddesc.ReportTelephony, 0}}; !reflect.DeepEqual(f.inputs, want) {
		t.Errorf("inputs % x", f.inputs)
	}
}

// headset is the far end of a session.
type headset struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

func (h headset) send(line string) {
	h.t.Helper()
	if _, err := h.conn.Write([]byte(line + "\r")); err != nil {
		h.t.Fatal(err)
	}
}

// expect reads framed result lines ("\r\n<line>\r\n").
func (h headset) expect(want ...string) {
	h.t.Helper()
	h.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for _, w := range want {
		var line string
		for line == "" {
			l, err := h.r.ReadString('\n')
			if err != nil {
				h.t.Fatalf("waiting for %q: %v", w, err)
			}
			line = strings.TrimSpace(l)
		}
		if line != w {
			h.t.Fatalf("got %q, want %q", line, w)
		}
	}
}

func TestSession(t *testing.T) {
	ours, theirs := net.Pipe()
	h := headset{t, theirs, bufio.NewReader(theirs)}
	events := make(chan hfp.Event, 4)
	s := NewSession(ours, hfp.HFP, hfp.DefaultFeatures, func(e hfp.Event) { events <- e }, nil)
	done := make(chan error, 1)
	go func() { done <- s.Run() }()

	h.send("AT+BRSF=959")
	h.expect("+BRSF: 36", "OK")
	h.send("AT+CIND=?")
	h.expect(`+CIND: ("service",(0,1)),("call",(0,1)),("callsetup",(0-3)),("callheld",(0-2)),("signal",(0-5)),("roam",(0,1)),("battchg",(0-5))`, "OK")
	h.send("AT+CIND?")
	h.expect("+CIND: 1,0,0,0,5,0,5", "OK")
	h.send("AT+CMER=3,0,0,1")
	h.expect("OK")

	go s.SetState(hfp.Incoming)
	h.expect("+CIEV: 3,1", "RING")
	h.send("ATA")
	h.expect("OK")
	if e := <-events; e.Kind != hfp.Answer {
		t.Fatalf("event %v", e.Kind)
	}
	go s.SetState(hfp.Active)
	h.expect("+CIEV: 2,1", "+CIEV: 3,0")

	h.send("AT+NOTREAL")
	h.expect("ERROR")
	h.send("garbage")
	h.expect("ERROR")
	h.send(strings.Repeat("A", 300))
	h.expect("ERROR")

	theirs.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not end")
	}
}
