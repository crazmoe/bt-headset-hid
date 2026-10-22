package desktop

import (
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/dbustest"
	"github.com/crazmoe/bt-headset-hid/internal/hfp"
	"github.com/crazmoe/bt-headset-hid/internal/modem"
)

func TestInCall(t *testing.T) {
	addr := dbustest.Bus(t)
	watcher := dbustest.Connect(t, addr)
	sigs := make(chan *dbus.Signal, 32)
	watcher.Signal(sigs)
	if err := Watch(watcher); err != nil {
		t.Fatal(err)
	}
	if in, err := InCall(watcher); in || err != nil {
		t.Fatalf("InCall without modem = %v, %v", in, err)
	}

	srv, err := modem.Serve(dbustest.Connect(t, addr), func(hfp.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, sigs)
	for _, step := range []struct {
		state hfp.CallState
		want  bool
	}{{hfp.Incoming, true}, {hfp.Active, true}, {hfp.Idle, false}, {hfp.Active, true}} {
		if err := srv.SetState(step.state); err != nil {
			t.Fatal(err)
		}
		waitSignal(t, sigs)
		if in, err := InCall(watcher); in != step.want || err != nil {
			t.Fatalf("after %v: InCall = %v, %v", step.state, in, err)
		}
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, sigs)
	if in, err := InCall(watcher); in || err != nil {
		t.Fatalf("InCall after the modem left = %v, %v", in, err)
	}
}

func waitSignal(t *testing.T, sigs chan *dbus.Signal) {
	t.Helper()
	select {
	case <-sigs:
	case <-time.After(5 * time.Second):
		t.Fatal("no signal")
	}
	// Drain the rest of the burst.
	for {
		select {
		case <-sigs:
		case <-time.After(50 * time.Millisecond):
			return
		}
	}
}

type player struct {
	mu     sync.Mutex
	status string
	calls  []string
}

func (p *player) set(call, status string) *dbus.Error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, call)
	p.status = status
	return nil
}

func (p *player) Pause() *dbus.Error { return p.set("Pause", "Paused") }
func (p *player) Play() *dbus.Error  { return p.set("Play", "Playing") }

func (p *player) log() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

type props struct{ p *player }

func (pr props) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	if iface != mprisPlayer || name != "PlaybackStatus" {
		return dbus.Variant{}, dbus.MakeFailedError(nil)
	}
	pr.p.mu.Lock()
	defer pr.p.mu.Unlock()
	return dbus.MakeVariant(pr.p.status), nil
}

func TestPlayers(t *testing.T) {
	addr := dbustest.Bus(t)
	playing, paused := &player{status: "Playing"}, &player{status: "Paused"}
	for name, p := range map[string]*player{"spotify": playing, "vlc": paused, "playerctld": {status: "Playing"}} {
		conn := dbustest.Connect(t, addr)
		conn.Export(p, mprisPath, mprisPlayer)
		conn.Export(props{p}, mprisPath, "org.freedesktop.DBus.Properties")
		if _, err := conn.RequestName(mprisPrefix+name, 0); err != nil {
			t.Fatal(err)
		}
	}
	s := System{Session: dbustest.Connect(t, addr)}
	got, err := s.Playing()
	if err != nil || len(got) != 1 || got[0] != mprisPrefix+"spotify" {
		t.Fatalf("Playing = %q, %v", got, err)
	}
	if err := s.Pause(got[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.Play(got[0]); err != nil {
		t.Fatal(err)
	}
	if got, other := playing.log(), paused.log(); len(got) != 2 || got[0] != "Pause" || got[1] != "Play" || other != nil {
		t.Errorf("calls %q %q", got, other)
	}
}
