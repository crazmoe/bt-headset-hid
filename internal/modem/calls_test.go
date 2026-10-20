package modem

import (
	"errors"
	"reflect"
	"testing"

	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

const prefix = "/mm/Call"

func mkCall(n string, st CallState, r StateReason, d Direction) Call {
	return Call{Path: prefix + "/" + n, State: st, Reason: r, Direction: d}
}

func expect(t *testing.T, got, want []Signal) {
	t.Helper()
	if len(want) == 0 {
		want = nil
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("signals\n got %+v\nwant %+v", got, want)
	}
}

func TestIncomingAnsweredOnHeadset(t *testing.T) {
	c := NewCalls(prefix)
	ringing := mkCall("1", CallRingingIn, ReasonIncomingNew, DirectionIncoming)
	expect(t, c.SetState(hfp.Incoming), []Signal{{Kind: Added, Call: ringing}})
	expect(t, c.SetState(hfp.Incoming), nil)

	sigs, ev, err := c.Accept(ringing.Path)
	if err != nil || ev == nil || ev.Kind != hfp.Answer {
		t.Fatalf("Accept: %v, %v", ev, err)
	}
	active := mkCall("1", CallActive, ReasonAccepted, DirectionIncoming)
	expect(t, sigs, []Signal{{Kind: StateChanged, Call: active, OldState: CallRingingIn}})
	// The softphone confirms with the Off-Hook LED: nothing new.
	expect(t, c.SetState(hfp.Active), nil)

	sigs, ev, err = c.Hangup(active.Path)
	if err != nil || ev == nil || ev.Kind != hfp.HangUp {
		t.Fatalf("Hangup: %v, %v", ev, err)
	}
	ended := mkCall("1", CallTerminated, ReasonTerminated, DirectionIncoming)
	expect(t, sigs, []Signal{{Kind: StateChanged, Call: ended, OldState: CallActive}, {Kind: Deleted, Call: ended}})
	// Off-Hook still lit (e.g. only Mute changed) must not revive the call.
	expect(t, c.SetState(hfp.Active), nil)
	expect(t, c.SetState(hfp.Idle), nil)
	if _, ok := c.Current(); ok {
		t.Fatal("call left over")
	}
}

func TestIncomingAnsweredInSoftphone(t *testing.T) {
	c := NewCalls(prefix)
	c.SetState(hfp.Incoming)
	active := mkCall("1", CallActive, ReasonAccepted, DirectionIncoming)
	expect(t, c.SetState(hfp.Active), []Signal{{Kind: StateChanged, Call: active, OldState: CallRingingIn}})
	ended := mkCall("1", CallTerminated, ReasonTerminated, DirectionIncoming)
	expect(t, c.SetState(hfp.Idle), []Signal{{Kind: StateChanged, Call: ended, OldState: CallActive}, {Kind: Deleted, Call: ended}})
}

func TestRejectOnHeadset(t *testing.T) {
	c := NewCalls(prefix)
	c.SetState(hfp.Incoming)
	sigs, ev, err := c.Hangup(prefix + "/1")
	if err != nil || ev.Kind != hfp.HangUp || len(sigs) != 2 || sigs[1].Kind != Deleted {
		t.Fatalf("Hangup: %+v, %v, %v", sigs, ev, err)
	}
	// A second ringing phase (new call) after the softphone went idle.
	c.SetState(hfp.Idle)
	ringing := mkCall("2", CallRingingIn, ReasonIncomingNew, DirectionIncoming)
	expect(t, c.SetState(hfp.Incoming), []Signal{{Kind: Added, Call: ringing}})
}

func TestOutgoingFromSoftphone(t *testing.T) {
	c := NewCalls(prefix)
	active := mkCall("1", CallActive, ReasonOutgoingStarted, DirectionOutgoing)
	expect(t, c.SetState(hfp.Active), []Signal{{Kind: Added, Call: active}})
	// Accept only applies to ringing calls.
	if _, _, err := c.Accept(active.Path); !errors.Is(err, ErrWrongState) {
		t.Fatalf("Accept on active call: %v", err)
	}
}

func TestActiveThenRinging(t *testing.T) {
	// Off-Hook goes off and Ring comes on in the same report: a new call.
	c := NewCalls(prefix)
	c.SetState(hfp.Active)
	ended := mkCall("1", CallTerminated, ReasonTerminated, DirectionOutgoing)
	ringing := mkCall("2", CallRingingIn, ReasonIncomingNew, DirectionIncoming)
	expect(t, c.SetState(hfp.Incoming), []Signal{
		{Kind: StateChanged, Call: ended, OldState: CallActive},
		{Kind: Deleted, Call: ended},
		{Kind: Added, Call: ringing},
	})
}

func TestUnknownCall(t *testing.T) {
	c := NewCalls(prefix)
	if _, _, err := c.Accept(prefix + "/1"); !errors.Is(err, ErrNoCall) {
		t.Errorf("Accept without call: %v", err)
	}
	c.SetState(hfp.Incoming)
	if _, _, err := c.Hangup(prefix + "/9"); !errors.Is(err, ErrNoCall) {
		t.Errorf("Hangup of wrong path: %v", err)
	}
}
