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

	mu      sync.Mutex // guards ag, writes to conn and ringing
	ag      *hfp.AG
	ringing chan struct{} // closed to stop the ring loop; nil when not ringing
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

// Run reads AT commands until the connection ends, answers them and reports
// events. It returns nil when the headset disconnects.
func (s *Session) Run() error {
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
		s.mu.Unlock()
		if err != nil {
			return err
		}
		if perr != nil {
			s.logf("unparsable AT line %q", line)
		}
		if reply.Event != nil && s.onEvent != nil {
			s.onEvent(*reply.Event)
		}
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
