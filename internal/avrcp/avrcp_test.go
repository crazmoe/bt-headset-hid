package avrcp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// fakeSysfs makes the sysfs entries of input devices: name and bus type.
func fakeSysfs(t *testing.T, devs map[string][2]string) string {
	sys := t.TempDir()
	for n, d := range devs {
		dir := filepath.Join(sys, "class/input", n, "device")
		if err := os.MkdirAll(filepath.Join(dir, "id"), 0o755); err != nil {
			t.Fatal(err)
		}
		if d[0] != "" {
			os.WriteFile(filepath.Join(dir, "name"), []byte(d[0]+"\n"), 0o644)
		}
		if d[1] != "" {
			os.WriteFile(filepath.Join(dir, "id/bustype"), []byte(d[1]+"\n"), 0o644)
		}
	}
	return sys
}

func TestScanSkipsBroken(t *testing.T) {
	sys := fakeSysfs(t, map[string][2]string{
		"event1": {"", "0005"},                   // no name
		"event2": {"Jabra (AVRCP)", ""},          // no bus type
		"event3": {"Jabra (AVRCP)", "bluetooth"}, // bus type not hex
		"event4": {"Jabra (AVRCP)", "0005"},
	})
	got, err := Scan(sys, "/dev")
	if err != nil || len(got) != 1 || got[0].Path != "/dev/input/event4" {
		t.Fatalf("Scan = %+v, %v", got, err)
	}
}

// logs collects the log lines of a Watcher.
type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logs) has(s string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line, s) {
			return true
		}
	}
	return false
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestWatcher(t *testing.T) {
	sys := fakeSysfs(t, map[string][2]string{
		"event3": {"AT Translated Set 2 keyboard", "0011"},
		"event7": {"Jabra Evolve 65 (AVRCP)", "0005"},
		"event9": {"JBL (AVRCP)", "0005"},
	})
	var mu sync.Mutex
	opened := map[string]int{}
	writers := map[string]*os.File{}
	var events []hfp.Event
	var l logs
	w := &Watcher{
		Sysfs: sys, Dev: "/dev", Interval: 10 * time.Millisecond,
		OnEvent: func(e hfp.Event) { mu.Lock(); events = append(events, e); mu.Unlock() },
		Logf:    l.logf,
		Open: func(path string) (io.ReadCloser, error) {
			mu.Lock()
			defer mu.Unlock()
			opened[path]++
			if path == "/dev/input/event9" {
				return nil, errors.New("busy")
			}
			r, wr, err := os.Pipe()
			if err != nil {
				return nil, err
			}
			writers[path] = wr
			return r, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	eventually(t, "the headset grabbed", func() bool { return l.has(`media keys of "Jabra Evolve 65 (AVRCP)" now go to the softphone`) })
	if !l.has("JBL (AVRCP): busy") {
		t.Errorf("failed grab not logged: %q", l.lines)
	}
	mu.Lock()
	wr := writers["/dev/input/event7"]
	mu.Unlock()
	wr.Write(append(event(evKey, keyPlayPause, 1), event(evKey, keyPlayPause, 0)...))
	eventually(t, "the key press", func() bool { mu.Lock(); defer mu.Unlock(); return len(events) == 1 })

	// While it is open, later scans leave it alone.
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if n := opened["/dev/input/event7"]; n != 1 {
		t.Errorf("opened %d times while open", n)
	}
	mu.Unlock()

	// When the device goes away, it is released and taken again once it is
	// back.
	wr.Close()
	eventually(t, "the release", func() bool { return l.has(`media keys of "Jabra Evolve 65 (AVRCP)" released`) })
	eventually(t, "the second grab", func() bool { mu.Lock(); defer mu.Unlock(); return opened["/dev/input/event7"] == 2 })

	cancel()
	<-done
}

func TestWatcherBadSysfs(t *testing.T) {
	var l logs
	w := &Watcher{Sysfs: "[", Dev: "/dev", Interval: time.Hour, Logf: l.logf}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.Run(ctx)
	if !l.has("media keys:") {
		t.Errorf("bad pattern not logged: %q", l.lines)
	}
}

func TestGrabFails(t *testing.T) {
	if _, err := grab(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("grab of a missing device succeeded")
	}
	// A plain file is no input device: EVIOCGRAB fails.
	p := filepath.Join(t.TempDir(), "event0")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := grab(p); err == nil || !strings.Contains(err.Error(), "EVIOCGRAB") {
		t.Errorf("grab of a plain file: %v", err)
	}
}
