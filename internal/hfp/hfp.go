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
	// FeatureCodecNegotiation lets the AG choose the audio codec with +BCS
	// before it opens the audio link; needed for wideband speech (mSBC).
	FeatureCodecNegotiation = 1 << 9
	// FeatureHFIndicators lets the headset report HF indicators with
	// AT+BIEV, here its battery level.
	FeatureHFIndicators = 1 << 10
)

// hfCodecNegotiation is the HF feature bit (AT+BRSF) for codec negotiation.
const hfCodecNegotiation = 1 << 7

// Audio codec IDs (AT+BAC, +BCS).
const (
	CodecCVSD = 1 // narrowband, 8 kHz
	CodecMSBC = 2 // wideband, 16 kHz
)

// maxCodecs bounds the codec list a headset may send with AT+BAC.
const maxCodecs = 8

// DefaultFeatures are the AG features we announce: the headset may start voice
// recognition (often bound to a long press), reject calls and report its
// battery level. Three-way calling is not announced so the handshake ends
// after AT+CMER.
const DefaultFeatures = FeatureVoiceRecognition | FeatureRejectCall | FeatureHFIndicators

// HF indicators (AT+BIND, AT+BIEV).
const (
	indicatorSafety  = 1 // enhanced safety, accepted and ignored
	indicatorBattery = 2 // battery level 0..100
)

// xaplFeatures are the Apple accessory features we accept (AT+XAPL): bit 1,
// battery level reports with AT+IPHONEACCEV.
const xaplFeatures = 2

// maxAccEvents bounds the key/value pairs of one AT+IPHONEACCEV.
const maxAccEvents = 8

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
	// CodecSelected answers a codec proposal (see ProposeCodec): Value is
	// the codec the headset confirmed with AT+BCS, or 0 if it sent its
	// codec list again with AT+BAC instead, which means no.
	CodecSelected
	// Battery: the headset's battery level, Value is 0..100 (AT+BIEV,
	// AT+IPHONEACCEV or AT+XEVENT=BATTERY).
	Battery
)

var eventNames = map[EventKind]string{
	Answer: "answer", HangUp: "hangup", Button: "button", Redial: "redial",
	Dial: "dial", VoiceRecognition: "voice-recognition",
	SpeakerGain: "speaker-gain", MicGain: "mic-gain", CodecSelected: "codec-selected",
	Battery: "battery",
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

var stateNames = map[CallState]string{Idle: "idle", Incoming: "incoming", Outgoing: "outgoing", Active: "active"}

func (s CallState) String() string {
	if n, ok := stateNames[s]; ok {
		return n
	}
	return "unknown"
}

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
	hfCodecs   []int // from AT+BAC
	proposed   int   // codec sent with +BCS and not yet answered, or 0
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
			// Only the battery level is wanted.
			return ok("+BIND: 1,0", "+BIND: 2,1")
		}
		return errReply()
	case "+BIEV":
		p := c.Params()
		if c.Type != atparser.Set || len(p) != 2 {
			return errReply()
		}
		id, err1 := strconv.Atoi(p[0])
		v, err2 := strconv.Atoi(p[1])
		if err1 != nil || err2 != nil {
			return errReply()
		}
		switch {
		case id == indicatorBattery && v >= 0 && v <= 100:
			return event(Event{Kind: Battery, Value: v})
		case id == indicatorSafety && (v == 0 || v == 1):
			return ok()
		}
		return errReply()
	case "+XAPL":
		// AT+XAPL=vendor-product-version,features: an accessory that
		// speaks Apple's extensions; it reports its battery level with
		// AT+IPHONEACCEV if we say we take it.
		if c.Type != atparser.Set || len(c.Params()) != 2 {
			return errReply()
		}
		return ok(fmt.Sprintf("+XAPL=iPhone,%d", xaplFeatures))
	case "+IPHONEACCEV":
		return ag.accessoryEvents(c)
	case "+XEVENT":
		return ag.xevent(c)
	case "+COPS":
		switch c.Type {
		case atparser.Set:
			return ok()
		case atparser.Read:
			return ok(`+COPS: 0,0,"bt-headset-hid"`)
		}
		return errReply()

	// Settings the headset may send; accepted without effect.
	case "+BAC":
		return ag.codecList(c)
	case "+BCS":
		if c.Type != atparser.Set {
			return errReply()
		}
		n, err := strconv.Atoi(c.Args)
		if err != nil || ag.proposed == 0 || n != ag.proposed {
			return errReply()
		}
		ag.proposed = 0
		return event(Event{Kind: CodecSelected, Value: n})

	case "+CMEE", "+CLIP", "+CCWA", "+NREC", "+BIA", "+BCC", "+CSRSF", "+CSRBATT":
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

// accessoryEvents handles AT+IPHONEACCEV=n,key,value,...: key 1 is the
// battery level 0..9, reported as 10..100 %; key 2 (docked) and others are
// ignored.
func (ag *AG) accessoryEvents(c atparser.Command) Reply {
	p := c.Params()
	if c.Type != atparser.Set || len(p) < 1 {
		return errReply()
	}
	n, err := strconv.Atoi(p[0])
	if err != nil || n < 1 || n > maxAccEvents || len(p) != 1+2*n {
		return errReply()
	}
	level := -1
	for i := 0; i < n; i++ {
		key, err1 := strconv.Atoi(p[1+2*i])
		v, err2 := strconv.Atoi(p[2+2*i])
		if err1 != nil || err2 != nil {
			return errReply()
		}
		if key == 1 && v >= 0 && v <= 9 {
			level = (v + 1) * 10
		}
	}
	if level < 0 {
		return ok()
	}
	return event(Event{Kind: Battery, Value: level})
}

// xevent handles AT+XEVENT, Plantronics' events. Only
// AT+XEVENT=BATTERY,level,levels,... is used, level as a percentage of
// levels, as PipeWire reads it; the other events are accepted and ignored.
func (ag *AG) xevent(c atparser.Command) Reply {
	p := c.Params()
	if c.Type != atparser.Set || len(p) == 0 {
		return errReply()
	}
	if p[0] != "BATTERY" {
		return ok()
	}
	if len(p) < 3 {
		return errReply()
	}
	level, err1 := strconv.Atoi(p[1])
	levels, err2 := strconv.Atoi(p[2])
	if err1 != nil || err2 != nil || levels < 1 || levels > 1000 || level < 0 || level > levels {
		return errReply()
	}
	return event(Event{Kind: Battery, Value: level * 100 / levels})
}

// codecList handles AT+BAC, the codecs the headset supports.
func (ag *AG) codecList(c atparser.Command) Reply {
	p := c.Params()
	if c.Type != atparser.Set || len(p) == 0 || len(p) > maxCodecs {
		return errReply()
	}
	codecs := make([]int, 0, len(p))
	for _, s := range p {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 255 {
			return errReply()
		}
		codecs = append(codecs, n)
	}
	ag.hfCodecs = codecs
	if ag.proposed != 0 {
		ag.proposed = 0
		return event(Event{Kind: CodecSelected})
	}
	return ok()
}

// CodecNegotiation reports whether both sides support codec negotiation.
func (ag *AG) CodecNegotiation() bool {
	return ag.profile == HFP && ag.features&FeatureCodecNegotiation != 0 && ag.hfFeatures&hfCodecNegotiation != 0
}

// ProposeCodec picks the audio codec for the next audio link from allowed
// (codec IDs the audio side can handle; CVSD is always possible). Without
// codec negotiation it returns CVSD and no line. Otherwise it returns the
// best codec both sides support and the +BCS line that proposes it; the
// headset answers with AT+BCS (Event CodecSelected) and the audio link may
// be opened only after that.
func (ag *AG) ProposeCodec(allowed ...int) (line string, codec int) {
	if !ag.CodecNegotiation() {
		return "", CodecCVSD
	}
	codec = CodecCVSD
	if contains(allowed, CodecMSBC) && contains(ag.hfCodecs, CodecMSBC) {
		codec = CodecMSBC
	}
	ag.proposed = codec
	return fmt.Sprintf("+BCS: %d", codec), codec
}

// CancelProposal forgets an unanswered codec proposal.
func (ag *AG) CancelProposal() { ag.proposed = 0 }

func contains(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
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
