package avrcp

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

func event(typ, code uint16, value int32) []byte {
	b := make([]byte, eventSize)
	o := eventSize - 8
	binary.NativeEndian.PutUint16(b[o:], typ)
	binary.NativeEndian.PutUint16(b[o+2:], code)
	binary.NativeEndian.PutUint32(b[o+4:], uint32(value))
	return b
}

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		name string
		bus  uint16
		want bool
	}{
		{"Jabra Evolve 65 (AVRCP)", 0x05, true},
		{"Jabra Evolve 65 (AVRCP)", 0x03, false},
		{"Jabra Evolve 65", 0x05, false},
		{"AT Translated Set 2 keyboard", 0x11, false},
	} {
		if got := Match(c.name, c.bus); got != c.want {
			t.Errorf("Match(%q, %#x) = %v", c.name, c.bus, got)
		}
	}
}

func TestDecode(t *testing.T) {
	d := Decoder{Debounce: 300 * time.Millisecond}
	t0 := time.Unix(1000, 0)
	for _, c := range []struct {
		ev   []byte
		at   time.Duration
		want bool
	}{
		{event(evKey, keyPlayPause, 1), 0, true},
		{event(evKey, keyPlayPause, 0), 10 * time.Millisecond, false}, // release
		{event(evKey, keyPauseCD, 1), 100 * time.Millisecond, false},  // same press, other key
		{event(evKey, keyPlayCD, 1), time.Second, true},
		{event(evKey, keyPlay, 2), 2 * time.Second, false}, // repeat
		{event(evKey, 163, 1), 3 * time.Second, false},     // KEY_NEXTSONG
		{event(0x00, 0, 0), 3 * time.Second, false},        // EV_SYN
		{event(evKey, keyPause, 1), 4 * time.Second, true},
		{event(evKey, keyPlay, 1), 5 * time.Second, true},
	} {
		e, ok, err := d.Decode(c.ev, t0.Add(c.at))
		if err != nil || ok != c.want || (ok && e.Kind != hfp.Button) {
			t.Errorf("at %v: %v %v %v, want %v", c.at, e.Kind, ok, err, c.want)
		}
	}
	if _, _, err := d.Decode(make([]byte, eventSize-1), t0); !errors.Is(err, ErrShortEvent) {
		t.Errorf("short event: %v", err)
	}
}

func TestScan(t *testing.T) {
	sys := t.TempDir()
	dev := func(n, name, bus string) {
		d := filepath.Join(sys, "class/input", n, "device")
		if err := os.MkdirAll(filepath.Join(d, "id"), 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(d, "name"), []byte(name+"\n"), 0o644)
		os.WriteFile(filepath.Join(d, "id/bustype"), []byte(bus+"\n"), 0o644)
	}
	dev("event3", "AT Translated Set 2 keyboard", "0011")
	dev("event7", "Jabra Evolve 65 (AVRCP)", "0005")
	dev("event8", "Fake (AVRCP)", "0003")
	got, err := Scan(sys, "/dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "/dev/input/event7" || got[0].Name != "Jabra Evolve 65 (AVRCP)" {
		t.Fatalf("Scan = %+v", got)
	}
}

func TestServe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []hfp.Event
	wt := &Watcher{OnEvent: func(e hfp.Event) { mu.Lock(); got = append(got, e); mu.Unlock() }}
	done := make(chan error)
	go func() { done <- wt.serve(r) }()
	w.Write(append(event(evKey, keyPlayPause, 1), event(evKey, keyPlayPause, 0)...))
	w.Write(event(0, 0, 0))
	w.Close()
	if err := <-done; err == nil {
		t.Fatal("serve returned nil at EOF")
	}
	if len(got) != 1 || got[0].Kind != hfp.Button {
		t.Fatalf("events %v", got)
	}
}
