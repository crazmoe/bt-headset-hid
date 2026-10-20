// Package hfp implements the Audio Gateway (AG) side of the Hands-Free Profile
// and the Headset Profile as a pure state machine: it consumes parsed AT
// commands from the headset and returns the reply lines to send back plus the
// button events the headset reported. It performs no I/O.
package hfp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/crazmoe/bt-headset-hid/internal/atparser"
)

// Profile selects HFP or HSP behaviour.
type Profile int

const (
	// HFP is the Hands-Free Profile (service level connection handshake required).
	HFP Profile = iota
	// HSP is the Headset Profile (no handshake, button via AT+CKPD=200).
	HSP
)

// AG feature bits from the HFP specification (+BRSF).
const (
	FeatureThreeWay         = 1 << 0
	FeatureECNR             = 1 << 1
	FeatureVoiceRecognition = 1 << 2
	FeatureInBandRing       = 1 << 3
	FeatureVoiceTag         = 1 << 4
	FeatureRejectCall       = 1 << 5
)

// DefaultFeatures are the AG features we announce: the headset may start voice
// recognition (often bound to a long press) and reject calls. Three-way
// calling is not announced so the handshake ends after AT+CMER.
const DefaultFeatures = FeatureVoiceRecognition | FeatureRejectCall

// EventKind identifies a headset action.
type EventKind int

const (
	// Answer: ATA, the headset accepts an incoming call.
	Answer EventKind = iota + 1
	// HangUp: AT+CHUP, the headset ends or rejects a call.
	HangUp
	// Button: AT+CKPD=200, the HSP headset button.
	Button
	// Redial: AT+BLDN, redial the last number.
	Redial
	// Dial: ATD..., Number holds the digits (or ">mem" for memory dialing).
	Dial
	// VoiceRecognition: AT+BVRA, Value is 1 (start) or 0 (stop).
	VoiceRecognition
	// SpeakerGain: AT+VGS, Value is 0..15.
	SpeakerGain
	// MicGain: AT+VGM, Value is 0..15.
	MicGain
)

var eventNames = map[EventKind]string{
	Answer: "answer", HangUp: "hangup", Button: "button", Redial: "redial",
	Dial: "dial", VoiceRecognition: "voice-recognition",
	SpeakerGain: "speaker-gain", MicGain: "mic-gain",
}

func (k EventKind) String() string {
	if s, ok := eventNames[k]; ok {
		return s
	}
	return "unknown"
}

// Event is an action reported by the headset.
type Event struct {
	Kind   EventKind
	Value  int
	Number string
}

// Reply is the outcome of one AT command.
type Reply struct {
	// Lines to send back, without framing; see Encode.
	Lines []string
	// Event is set when the command was a button press or gain change.
	Event *Event
}

// CallState is the telephony state as seen by the headset.
type CallState int

const (
	Idle     CallState = iota
	Incoming           // ringing; the headset button answers
	Outgoing           // dialing/alerting; the headset button cancels
	Active             // call in progress; the headset button hangs up
)

// Indicator indices as announced in the +CIND test response (1-based).
const (
	indService   = 1
	indCall      = 2
	indCallSetup = 3
	indCallHeld  = 4
	indSignal    = 5
	indRoam      = 6
	indBattChg   = 7
)

const cindTest = `+CIND: ("service",(0,1)),("call",(0,1)),("callsetup",(0-3)),` +
	`("callheld",(0-2)),("signal",(0-5)),("roam",(0,1)),("battchg",(0-5))`

// RING is the unsolicited result code the AG repeats while a call is incoming.
const RING = "RING"

// AG is the Audio Gateway state for one headset connection.
type AG struct {
	profile    Profile
	features   int
	hfFeatures int
	reporting  bool // AT+CMER enabled indicator events
	connected  bool // service level connection established
	state      CallState
	speaker    int
	mic        int
}

// New returns an AG for the given profile announcing the given features.
func New(p Profile, features int) *AG {
	return &AG{profile: p, features: features, connected: p == HSP, speaker: -1, mic: -1}
}

// Connected reports whether the service level connection is established,
// i.e. the headset will now send button commands. Always true for HSP.
func (ag *AG) Connected() bool { return ag.connected }

// HFFeatures returns the feature bits the headset announced with AT+BRSF.
func (ag *AG) HFFeatures() int { return ag.hfFeatures }

// State returns the current call state.
func (ag *AG) State() CallState { return ag.state }

// Gains returns the last speaker and microphone gain reported by the headset,
// or -1 if none was reported yet.
func (ag *AG) Gains() (speaker, mic int) { return ag.speaker, ag.mic }

func ok(lines ...string) Reply    { return Reply{Lines: append(lines, "OK")} }
func errReply() Reply             { return Reply{Lines: []string{"ERROR"}} }
func event(e Event) Reply         { r := ok(); r.Event = &e; return r }
func one(c atparser.Command) bool { return len(c.Params()) == 1 }

// Handle processes one command from the headset. Unknown or malformed
// commands get ERROR, which is what headsets expect for unsupported features.
func (ag *AG) Handle(c atparser.Command) Reply {
	switch c.Name {
	// Service level connection handshake (HFP 1.x, section 4.2).
	case "+BRSF":
		if c.Type != atparser.Set || !one(c) {
			return errReply()
		}
		n, err := strconv.Atoi(c.Args)
		if err != nil || n < 0 {
			return errReply()
		}
		ag.hfFeatures = n
		return ok(fmt.Sprintf("+BRSF: %d", ag.features))
	case "+CIND":
		switch c.Type {
		case atparser.Test:
			return ok(cindTest)
		case atparser.Read:
			call, setup := ag.indicators()
			return ok(fmt.Sprintf("+CIND: 1,%d,%d,0,5,0,5", call, setup))
		}
		return errReply()
	case "+CMER":
		p := c.Params()
		if c.Type != atparser.Set || len(p) < 4 {
			return errReply()
		}
		ag.reporting = p[0] == "3" && p[3] == "1"
		if ag.features&FeatureThreeWay == 0 || ag.hfFeatures&FeatureThreeWay == 0 {
			ag.connected = true
		}
		return ok()
	case "+CHLD":
		if c.Type == atparser.Test {
			ag.connected = true
			return ok("+CHLD: (0,1,2,3)")
		}
		return errReply()
	case "+BIND":
		switch c.Type {
		case atparser.Set:
			return ok()
		case atparser.Test:
			return ok("+BIND: (1,2)")
		case atparser.Read:
			return ok("+BIND: 1,0", "+BIND: 2,0")
		}
		return errReply()
	case "+COPS":
		switch c.Type {
		case atparser.Set:
			return ok()
		case atparser.Read:
			return ok(`+COPS: 0,0,"bt-headset-hid"`)
		}
		return errReply()

	// Settings the headset may send; accepted without effect.
	case "+CMEE", "+CLIP", "+CCWA", "+NREC", "+BIA", "+BAC", "+BIEV", "+BCS", "+BCC",
		"+XAPL", "+IPHONEACCEV", "+XEVENT", "+CSRSF", "+CSRBATT":
		if c.Type == atparser.Set || c.Type == atparser.Exec {
			return ok()
		}
		return errReply()
	case "+CLCC", "+CNUM":
		return ok()
	case "+BTRH":
		if c.Type == atparser.Read {
			return ok()
		}
		return errReply()

	// Buttons.
	case "A":
		if c.Args != "" {
			return errReply()
		}
		return event(Event{Kind: Answer})
	case "+CHUP":
		if c.Type != atparser.Exec {
			return errReply()
		}
		return event(Event{Kind: HangUp})
	case "+CKPD":
		if c.Type != atparser.Set || c.Args != "200" {
			return errReply()
		}
		return event(Event{Kind: Button})
	case "+BLDN":
		if c.Type != atparser.Exec {
			return errReply()
		}
		return event(Event{Kind: Redial})
	case "D":
		num := strings.TrimSuffix(c.Args, ";")
		if num == "" || len(num) > 32 || !validNumber(num) {
			return errReply()
		}
		return event(Event{Kind: Dial, Number: num})
	case "+BVRA":
		if c.Type != atparser.Set || (c.Args != "0" && c.Args != "1") {
			return errReply()
		}
		return event(Event{Kind: VoiceRecognition, Value: int(c.Args[0] - '0')})
	case "+VGS", "+VGM":
		if c.Type != atparser.Set {
			return errReply()
		}
		n, err := strconv.Atoi(c.Args)
		if err != nil || n < 0 || n > 15 {
			return errReply()
		}
		if c.Name == "+VGS" {
			ag.speaker = n
			return event(Event{Kind: SpeakerGain, Value: n})
		}
		ag.mic = n
		return event(Event{Kind: MicGain, Value: n})
	}
	return errReply()
}

func validNumber(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !(b >= '0' && b <= '9' || b == '+' || b == '*' || b == '#' || b == '>' || (i > 0 && b >= 'A' && b <= 'D')) {
			return false
		}
	}
	return true
}

func (ag *AG) indicators() (call, setup int) {
	switch ag.state {
	case Incoming:
		return 0, 1
	case Outgoing:
		return 0, 3
	case Active:
		return 1, 0
	}
	return 0, 0
}

// SetState changes the call state, e.g. because the softphone set the
// off-hook or ring LED. It returns the unsolicited +CIEV lines to send to the
// headset (none before the handshake or if reporting is disabled). While the
// state is Incoming the caller should additionally send RING periodically.
func (ag *AG) SetState(s CallState) []string {
	oldCall, oldSetup := ag.indicators()
	ag.state = s
	if ag.profile != HFP || !ag.connected || !ag.reporting {
		return nil
	}
	call, setup := ag.indicators()
	var lines []string
	// Report call before callsetup so the headset never sees "no call, no setup"
	// in between when an incoming call is answered.
	if call != oldCall {
		lines = append(lines, fmt.Sprintf("+CIEV: %d,%d", indCall, call))
	}
	if setup != oldSetup {
		lines = append(lines, fmt.Sprintf("+CIEV: %d,%d", indCallSetup, setup))
	}
	return lines
}

// SpeakerGainLine returns the unsolicited result code that sets the headset
// speaker volume (0..15), as sent when the host changes the volume.
func SpeakerGainLine(n int) string { return fmt.Sprintf("+VGS: %d", clampGain(n)) }

// MicGainLine is like SpeakerGainLine for the microphone.
func MicGainLine(n int) string { return fmt.Sprintf("+VGM: %d", clampGain(n)) }

func clampGain(n int) int { return min(max(n, 0), 15) }

// Encode frames reply lines for the RFCOMM channel ("\r\n<line>\r\n").
func Encode(lines []string) []byte {
	var b []byte
	for _, l := range lines {
		b = append(b, "\r\n"...)
		b = append(b, l...)
		b = append(b, "\r\n"...)
	}
	return b
}
