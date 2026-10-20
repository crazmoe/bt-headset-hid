// Package modem poses as a ModemManager modem on the D-Bus system bus so
// that PipeWire, which owns the HFP connection and its audio, reports the
// headset's answer and hang-up buttons to us and signals ringing and active
// calls to the headset.
//
// PipeWire's native HFP backend (bluez5.hfphsp-backend-native-modem) forwards
// ATA and AT+CHUP as Call.Accept and Call.Hangup to the first ModemManager
// modem and turns the modem's call states into +CIEV indicators. This package
// keeps one call that mirrors the softphone's state as derived from the HID
// LEDs; see Calls for the pure state logic and Server for the D-Bus side.
package modem

import (
	"errors"
	"fmt"

	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

// CallState is MMCallState.
type CallState int32

const (
	CallUnknown    CallState = 0
	CallDialing    CallState = 1
	CallRingingOut CallState = 2
	CallRingingIn  CallState = 3
	CallActive     CallState = 4
	CallHeld       CallState = 5
	CallWaiting    CallState = 6
	CallTerminated CallState = 7
)

// StateReason is MMCallStateReason.
type StateReason int32

const (
	ReasonUnknown         StateReason = 0
	ReasonOutgoingStarted StateReason = 1
	ReasonIncomingNew     StateReason = 2
	ReasonAccepted        StateReason = 3
	ReasonTerminated      StateReason = 4
)

// Direction is MMCallDirection.
type Direction int32

const (
	DirectionUnknown  Direction = 0
	DirectionIncoming Direction = 1
	DirectionOutgoing Direction = 2
)

// Call is the one call the modem exposes.
type Call struct {
	Path      string
	State     CallState
	Reason    StateReason
	Direction Direction
}

// SignalKind identifies a change the D-Bus side has to publish.
type SignalKind int

const (
	// Added: export the call object, then emit Voice.CallAdded.
	Added SignalKind = iota + 1
	// StateChanged: update the call properties and emit Call.StateChanged.
	StateChanged
	// Deleted: emit Voice.CallDeleted and remove the call object.
	Deleted
)

// Signal is one change to publish, in order.
type Signal struct {
	Kind     SignalKind
	Call     Call // the call after the change
	OldState CallState
}

// ErrNoCall is returned by Accept and Hangup for an unknown or finished call.
var ErrNoCall = errors.New("modem: no such call")

// ErrWrongState is returned by Accept when the call is not ringing.
var ErrWrongState = errors.New("modem: call is not ringing")

// Calls tracks the call state. It is not safe for concurrent use.
type Calls struct {
	prefix string
	next   int
	call   *Call
	host   hfp.CallState // last state reported by the host
}

// NewCalls returns an empty call list whose call objects live under prefix,
// e.g. "/org/freedesktop/ModemManager1/Call".
func NewCalls(prefix string) *Calls { return &Calls{prefix: prefix} }

// Current returns the current call, if any.
func (c *Calls) Current() (Call, bool) {
	if c.call == nil {
		return Call{}, false
	}
	return *c.call, true
}

func (c *Calls) add(state CallState, dir Direction, reason StateReason) Signal {
	c.next++
	c.call = &Call{Path: fmt.Sprintf("%s/%d", c.prefix, c.next), State: state, Reason: reason, Direction: dir}
	return Signal{Kind: Added, Call: *c.call}
}

func (c *Calls) transition(state CallState, reason StateReason) Signal {
	old := c.call.State
	c.call.State, c.call.Reason = state, reason
	return Signal{Kind: StateChanged, Call: *c.call, OldState: old}
}

func (c *Calls) terminate() []Signal {
	if c.call == nil {
		return nil
	}
	s := c.transition(CallTerminated, ReasonTerminated)
	d := Signal{Kind: Deleted, Call: *c.call}
	c.call = nil
	return []Signal{s, d}
}

// SetState follows the call state reported by the host (from the HID LEDs).
// Repeating the last reported state does nothing, so an LED report that only
// changes Mute does not bring back a call the headset just hung up.
func (c *Calls) SetState(s hfp.CallState) []Signal {
	if s == c.host {
		return nil
	}
	c.host = s
	var want CallState
	switch s {
	case hfp.Idle:
		return c.terminate()
	case hfp.Incoming:
		want = CallRingingIn
	case hfp.Outgoing:
		want = CallRingingOut
	case hfp.Active:
		want = CallActive
	default:
		return nil
	}
	if c.call != nil {
		switch {
		case c.call.State == want:
			return nil
		case want == CallActive && (c.call.State == CallRingingIn || c.call.State == CallRingingOut):
			return []Signal{c.transition(CallActive, ReasonAccepted)}
		}
	}
	// A different call: end the old one and start a new one.
	sigs := c.terminate()
	switch want {
	case CallRingingIn:
		return append(sigs, c.add(CallRingingIn, DirectionIncoming, ReasonIncomingNew))
	default:
		return append(sigs, c.add(want, DirectionOutgoing, ReasonOutgoingStarted))
	}
}

func (c *Calls) find(path string) error {
	if c.call == nil || c.call.Path != path {
		return ErrNoCall
	}
	return nil
}

// Accept handles Call.Accept from PipeWire (the headset sent ATA). The call
// becomes active right away; the host confirms it with the Off-Hook LED.
func (c *Calls) Accept(path string) ([]Signal, *hfp.Event, error) {
	if err := c.find(path); err != nil {
		return nil, nil, err
	}
	if c.call.State != CallRingingIn {
		return nil, nil, ErrWrongState
	}
	return []Signal{c.transition(CallActive, ReasonAccepted)}, &hfp.Event{Kind: hfp.Answer}, nil
}

// Hangup handles Call.Hangup from PipeWire (the headset sent AT+CHUP), which
// rejects a ringing call or ends an active one.
func (c *Calls) Hangup(path string) ([]Signal, *hfp.Event, error) {
	if err := c.find(path); err != nil {
		return nil, nil, err
	}
	return c.terminate(), &hfp.Event{Kind: hfp.HangUp}, nil
}
