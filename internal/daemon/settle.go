package daemon

import (
	"sync"
	"time"

	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

// settler passes call states on, but holds back Active when it follows Idle
// for delay. Some softphones signal Off-Hook for a moment before Ring when a
// call comes in; passed on, the headset would see an active call that ends
// at once and then a ringing one, and lose track of the call. A different
// state within delay drops the held Active.
type settler struct {
	delay time.Duration
	set   func(hfp.CallState)

	mu      sync.Mutex
	last    hfp.CallState // last state passed on
	pending *time.Timer
}

func (s *settler) SetState(st hfp.CallState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending != nil {
		if st == hfp.Active {
			return // still waiting for this one
		}
		s.pending.Stop()
		s.pending = nil
	}
	if st == hfp.Active && s.last == hfp.Idle {
		var t *time.Timer
		t = time.AfterFunc(s.delay, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.pending != t {
				return // dropped meanwhile
			}
			s.pending = nil
			s.pass(hfp.Active)
		})
		s.pending = t
		return
	}
	s.pass(st)
}

func (s *settler) pass(st hfp.CallState) {
	s.last = st
	s.set(st)
}
