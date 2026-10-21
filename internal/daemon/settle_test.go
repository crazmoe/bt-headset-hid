package daemon

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

func TestSettler(t *testing.T) {
	var mu sync.Mutex
	var got []hfp.CallState
	s := &settler{delay: 50 * time.Millisecond, set: func(st hfp.CallState) {
		mu.Lock()
		got = append(got, st)
		mu.Unlock()
	}}
	wait := func(want ...hfp.CallState) {
		t.Helper()
		time.Sleep(150 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("passed %v, want %v", got, want)
		}
	}

	// Off-Hook flicker before Ring: only Incoming reaches the headset, and
	// answering is passed on at once.
	s.SetState(hfp.Active)
	s.SetState(hfp.Incoming)
	s.SetState(hfp.Active)
	s.SetState(hfp.Idle)
	wait(hfp.Incoming, hfp.Active, hfp.Idle)

	// A real outgoing call: Active arrives after the delay, repeats do not
	// postpone or duplicate it.
	mu.Lock()
	got = nil
	mu.Unlock()
	s.SetState(hfp.Active)
	s.SetState(hfp.Active)
	wait(hfp.Active)
}
