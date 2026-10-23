package daemon

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/crazmoe/bt-headset-hid/internal/atparser"
	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

// RingInterval is how often RING is repeated while a call is incoming.
const RingInterval = 3 * time.Second

// Session speaks HFP or HSP as Audio Gateway on one headset connection, in
// the mode without audio.
type Session struct {
	conn    io.ReadWriteCloser
	onEvent func(hfp.Event)
	logf    func(format string, args ...any)

	mu          sync.Mutex // guards ag, writes to conn, ringing, codec and onConnected
	ag          *hfp.AG
	ringing     chan struct{} // closed to stop the ring loop; nil when not ringing
	codec       chan int      // receives the answer to a codec proposal; nil when none is open
	onConnected func()

	negotiate sync.Mutex // one codec negotiation at a time
}

// NewSession returns a session on conn. onEvent receives the headset's
// button presses and gain changes; it is called from the goroutine running
// Run without the session locked.
func NewSession(conn io.ReadWriteCloser, p hfp.Profile, features int, onEvent func(hfp.Event), logf func(string, ...any)) *Session {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Session{conn: conn, onEvent: onEvent, logf: logf, ag: hfp.New(p, features)}
}

func (s *Session) write(lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	_, err := s.conn.Write(hfp.Encode(lines))
	return err
}

// OnConnected sets a function called once, from Run, when the service level
// connection is established and the headset can take part in calls.
func (s *Session) OnConnected(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onConnected = f
}

// Run reads AT commands until the connection ends, answers them and reports
// events. It returns nil when the headset disconnects.
func (s *Session) Run() error {
	s.mu.Lock()
	connected := s.ag.Connected()
	s.mu.Unlock()
	if connected {
		s.connected()
	}
	defer s.Close()
	r := atparser.NewReader(s.conn)
	for {
		line, err := r.ReadLine()
		switch {
		case errors.Is(err, atparser.ErrLineTooLong):
			s.logf("headset sent an overlong line")
			s.mu.Lock()
			err = s.write([]string{"ERROR"})
			s.mu.Unlock()
			if err != nil {
				return err
			}
			continue
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return err
		}
		reply := hfp.Reply{Lines: []string{"ERROR"}}
		cmd, perr := atparser.Parse(line)
		s.mu.Lock()
		if perr == nil {
			reply = s.ag.Handle(cmd)
		}
		err = s.write(reply.Lines)
		nowConnected := !connected && s.ag.Connected()
		var codec chan int
		if reply.Event != nil && reply.Event.Kind == hfp.CodecSelected {
			codec, s.codec = s.codec, nil
		}
		s.mu.Unlock()
		if err != nil {
			return err
		}
		if perr != nil {
			s.logf("unparsable AT line %q", line)
		}
		if nowConnected {
			connected = true
			s.connected()
		}
		switch {
		case reply.Event == nil:
		case reply.Event.Kind == hfp.CodecSelected:
			if codec != nil {
				codec <- reply.Event.Value
			}
		case s.onEvent != nil:
			s.onEvent(*reply.Event)
		}
	}
}

func (s *Session) connected() {
	s.mu.Lock()
	f := s.onConnected
	s.mu.Unlock()
	if f != nil {
		f()
	}
}

// NegotiateCodec agrees with the headset on the codec for the next audio
// link, out of allowed (CVSD is always possible), and returns it. Without
// codec negotiation that is CVSD. If the headset refuses mSBC or does not
// answer within timeout, it tries CVSD; if that gets no answer either, it
// returns CVSD anyway, which headsets accept without negotiation. Run must
// be running to receive the answer.
func (s *Session) NegotiateCodec(allowed []int, timeout time.Duration) (int, error) {
	s.negotiate.Lock()
	defer s.negotiate.Unlock()
	for {
		ch := make(chan int, 1)
		s.mu.Lock()
		line, codec := s.ag.ProposeCodec(allowed...)
		if line == "" {
			s.mu.Unlock()
			return codec, nil
		}
		s.codec = ch
		err := s.write([]string{line})
		s.mu.Unlock()
		if err != nil {
			return 0, err
		}
		var got int
		select {
		case got = <-ch:
		case <-time.After(timeout):
			s.mu.Lock()
			s.ag.CancelProposal()
			s.codec = nil
			s.mu.Unlock()
			s.logf("headset did not answer the codec proposal %d", codec)
		}
		switch {
		case got == codec:
			return codec, nil
		case codec == hfp.CodecCVSD:
			return hfp.CodecCVSD, nil
		}
		allowed = []int{hfp.CodecCVSD}
	}
}

// SetState passes the softphone's call state to the headset (+CIEV) and
// starts or stops repeating RING.
func (s *Session) SetState(st hfp.CallState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.write(s.ag.SetState(st))
	if st == hfp.Incoming && s.ringing == nil {
		s.ringing = make(chan struct{})
		go s.ring(s.ringing)
	} else if st != hfp.Incoming && s.ringing != nil {
		close(s.ringing)
		s.ringing = nil
	}
	return err
}

// SpeakerGain sets the headset volume (0..15).
func (s *Session) SpeakerGain(n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write([]string{hfp.SpeakerGainLine(n)})
}

func (s *Session) ring(stop chan struct{}) {
	t := time.NewTicker(RingInterval)
	defer t.Stop()
	for {
		s.mu.Lock()
		select {
		case <-stop:
			s.mu.Unlock()
			return
		default:
		}
		var err error
		if s.ag.Connected() {
			err = s.write([]string{hfp.RING})
		}
		s.mu.Unlock()
		if err != nil {
			return
		}
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

// Close stops ringing and closes the connection.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.ringing != nil {
		close(s.ringing)
		s.ringing = nil
	}
	s.mu.Unlock()
	return s.conn.Close()
}
