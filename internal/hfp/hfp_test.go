package hfp

import (
	"reflect"
	"testing"

	"github.com/crazmoe/bt-headset-hid/internal/atparser"
)

func handle(t *testing.T, ag *AG, line string) Reply {
	t.Helper()
	c, err := atparser.Parse(line)
	if err != nil {
		t.Fatalf("Parse(%q): %v", line, err)
	}
	return ag.Handle(c)
}

func expectLines(t *testing.T, ag *AG, line string, want ...string) Reply {
	t.Helper()
	r := handle(t, ag, line)
	if !reflect.DeepEqual(r.Lines, want) {
		t.Fatalf("%s -> %q, want %q", line, r.Lines, want)
	}
	return r
}

// handshake runs the service level connection setup a typical HFP 1.7 headset sends.
func handshake(t *testing.T, ag *AG) {
	t.Helper()
	expectLines(t, ag, "AT+BRSF=959", "+BRSF: 36", "OK")
	expectLines(t, ag, "AT+BAC=1,2", "OK")
	expectLines(t, ag, "AT+CIND=?", cindTest, "OK")
	expectLines(t, ag, "AT+CIND?", "+CIND: 1,0,0,0,5,0,5", "OK")
	if ag.Connected() {
		t.Fatal("connected before AT+CMER")
	}
	expectLines(t, ag, "AT+CMER=3,0,0,1", "OK")
	if !ag.Connected() {
		t.Fatal("not connected after AT+CMER")
	}
}

func TestHandshake(t *testing.T) {
	ag := New(HFP, DefaultFeatures)
	handshake(t, ag)
	if ag.HFFeatures() != 959 {
		t.Errorf("HFFeatures = %d", ag.HFFeatures())
	}
	// Commands commonly sent right after the handshake.
	for _, l := range []string{"AT+CMEE=1", "AT+CLIP=1", "AT+CCWA=1", "AT+NREC=0", "AT+COPS=3,0", "AT+CLCC", "AT+XAPL=0B0E-1234-0100,10"} {
		expectLines(t, ag, l, "OK")
	}
	expectLines(t, ag, "AT+COPS?", `+COPS: 0,0,"bt-headset-hid"`, "OK")
	expectLines(t, ag, "AT+BTRH?", "OK")
}

func TestHandshakeThreeWay(t *testing.T) {
	ag := New(HFP, DefaultFeatures|FeatureThreeWay)
	expectLines(t, ag, "AT+BRSF=1", "+BRSF: 37", "OK")
	expectLines(t, ag, "AT+CMER=3,0,0,1", "OK")
	if ag.Connected() {
		t.Fatal("connected before AT+CHLD=? although both sides support three-way calling")
	}
	expectLines(t, ag, "AT+CHLD=?", "+CHLD: (0,1,2,3)", "OK")
	if !ag.Connected() {
		t.Fatal("not connected after AT+CHLD=?")
	}
}

func TestHSPConnectedImmediately(t *testing.T) {
	ag := New(HSP, 0)
	if !ag.Connected() {
		t.Fatal("HSP must not need a handshake")
	}
	r := expectLines(t, ag, "AT+CKPD=200", "OK")
	if r.Event == nil || r.Event.Kind != Button {
		t.Fatalf("event = %+v", r.Event)
	}
	if lines := ag.SetState(Incoming); lines != nil {
		t.Fatalf("HSP must not send +CIEV, got %q", lines)
	}
}

func TestButtonEvents(t *testing.T) {
	tests := []struct {
		line string
		want Event
	}{
		{"ATA", Event{Kind: Answer}},
		{"AT+CHUP", Event{Kind: HangUp}},
		{"AT+CKPD=200", Event{Kind: Button}},
		{"AT+BLDN", Event{Kind: Redial}},
		{"ATD0441234567;", Event{Kind: Dial, Number: "0441234567"}},
		{"ATD>1;", Event{Kind: Dial, Number: ">1"}},
		{"AT+BVRA=1", Event{Kind: VoiceRecognition, Value: 1}},
		{"AT+BVRA=0", Event{Kind: VoiceRecognition, Value: 0}},
		{"AT+VGS=12", Event{Kind: SpeakerGain, Value: 12}},
		{"AT+VGM=0", Event{Kind: MicGain, Value: 0}},
	}
	ag := New(HFP, DefaultFeatures)
	handshake(t, ag)
	for _, tt := range tests {
		r := expectLines(t, ag, tt.line, "OK")
		if r.Event == nil || *r.Event != tt.want {
			t.Errorf("%s -> event %+v, want %+v", tt.line, r.Event, tt.want)
		}
	}
	if s, m := ag.Gains(); s != 12 || m != 0 {
		t.Errorf("Gains = %d,%d", s, m)
	}
}

func TestErrors(t *testing.T) {
	ag := New(HFP, DefaultFeatures)
	for _, l := range []string{
		"AT+BRSF", "AT+BRSF=x", "AT+BRSF=-1", "AT+BRSF=1,2", "AT+CIND=1", "AT+CMER=3",
		"AT+CHLD=1", "AT+CKPD=100", "AT+CKPD", "ATA1", "AT+CHUP=1", "AT+BLDN?",
		"ATD;", "ATDabc;", "AT+BVRA=2", "AT+VGS=16", "AT+VGS=-1", "AT+VGM",
		"AT+UNKNOWN", "ATZ", "AT+CMEE?",
	} {
		r := expectLines(t, ag, l, "ERROR")
		if r.Event != nil {
			t.Errorf("%s produced event %+v", l, r.Event)
		}
	}
}

func TestSetState(t *testing.T) {
	ag := New(HFP, DefaultFeatures)
	if lines := ag.SetState(Incoming); lines != nil {
		t.Fatalf("+CIEV before handshake: %q", lines)
	}
	ag.SetState(Idle)
	handshake(t, ag)

	steps := []struct {
		s    CallState
		want []string
		cind string
	}{
		{Incoming, []string{"+CIEV: 3,1"}, "+CIND: 1,0,1,0,5,0,5"},
		{Active, []string{"+CIEV: 2,1", "+CIEV: 3,0"}, "+CIND: 1,1,0,0,5,0,5"},
		{Active, nil, "+CIND: 1,1,0,0,5,0,5"},
		{Idle, []string{"+CIEV: 2,0"}, "+CIND: 1,0,0,0,5,0,5"},
		{Outgoing, []string{"+CIEV: 3,3"}, "+CIND: 1,0,3,0,5,0,5"},
		{Idle, []string{"+CIEV: 3,0"}, "+CIND: 1,0,0,0,5,0,5"},
	}
	for _, st := range steps {
		if got := ag.SetState(st.s); !reflect.DeepEqual(got, st.want) {
			t.Errorf("SetState(%d) = %q, want %q", st.s, got, st.want)
		}
		if ag.State() != st.s {
			t.Errorf("State = %d, want %d", ag.State(), st.s)
		}
		expectLines(t, ag, "AT+CIND?", st.cind, "OK")
	}
}

func TestSetStateReportingDisabled(t *testing.T) {
	ag := New(HFP, DefaultFeatures)
	expectLines(t, ag, "AT+BRSF=0", "+BRSF: 36", "OK")
	expectLines(t, ag, "AT+CMER=3,0,0,0", "OK")
	if lines := ag.SetState(Active); lines != nil {
		t.Fatalf("+CIEV with reporting disabled: %q", lines)
	}
}

func TestEncodeAndGainLines(t *testing.T) {
	got := string(Encode([]string{"+BRSF: 36", "OK"}))
	if want := "\r\n+BRSF: 36\r\n\r\nOK\r\n"; got != want {
		t.Errorf("Encode = %q, want %q", got, want)
	}
	if Encode(nil) != nil {
		t.Error("Encode(nil) != nil")
	}
	if SpeakerGainLine(20) != "+VGS: 15" || MicGainLine(-3) != "+VGM: 0" || SpeakerGainLine(7) != "+VGS: 7" {
		t.Error("gain lines not clamped")
	}
}

func TestCodecNegotiation(t *testing.T) {
	ag := New(HFP, DefaultFeatures|FeatureCodecNegotiation)
	expectLines(t, ag, "AT+BRSF=959", "+BRSF: 548", "OK")
	expectLines(t, ag, "AT+BAC=1,2", "OK")
	if !ag.CodecNegotiation() {
		t.Fatal("no codec negotiation")
	}
	// An answer before a proposal is an error.
	expectLines(t, ag, "AT+BCS=2", "ERROR")

	line, codec := ag.ProposeCodec(CodecCVSD, CodecMSBC)
	if line != "+BCS: 2" || codec != CodecMSBC {
		t.Fatalf("ProposeCodec = %q, %d", line, codec)
	}
	expectLines(t, ag, "AT+BCS=1", "ERROR") // not what was proposed
	r := expectLines(t, ag, "AT+BCS=2", "OK")
	if r.Event == nil || *r.Event != (Event{Kind: CodecSelected, Value: CodecMSBC}) {
		t.Fatalf("event = %+v", r.Event)
	}
	expectLines(t, ag, "AT+BCS=2", "ERROR") // already answered

	// The audio side cannot do mSBC.
	if line, codec := ag.ProposeCodec(CodecCVSD); line != "+BCS: 1" || codec != CodecCVSD {
		t.Fatalf("ProposeCodec(CVSD) = %q, %d", line, codec)
	}
	// The headset refuses by sending its list again.
	r = expectLines(t, ag, "AT+BAC=1", "OK")
	if r.Event == nil || *r.Event != (Event{Kind: CodecSelected}) {
		t.Fatalf("refusal event = %+v", r.Event)
	}
	// Now it only has CVSD.
	if line, codec := ag.ProposeCodec(CodecCVSD, CodecMSBC); line != "+BCS: 1" || codec != CodecCVSD {
		t.Fatalf("ProposeCodec after AT+BAC=1 = %q, %d", line, codec)
	}
	ag.CancelProposal()
	expectLines(t, ag, "AT+BCS=1", "ERROR")
}

func TestCodecWithoutNegotiation(t *testing.T) {
	// The headset does not announce codec negotiation (bit 7).
	ag := New(HFP, DefaultFeatures|FeatureCodecNegotiation)
	expectLines(t, ag, "AT+BRSF=31", "+BRSF: 548", "OK")
	if line, codec := ag.ProposeCodec(CodecCVSD, CodecMSBC); line != "" || codec != CodecCVSD {
		t.Fatalf("ProposeCodec = %q, %d", line, codec)
	}
	// Nor do we: mode hfp.
	ag = New(HFP, DefaultFeatures)
	expectLines(t, ag, "AT+BRSF=959", "+BRSF: 36", "OK")
	if ag.CodecNegotiation() {
		t.Fatal("codec negotiation without the AG feature")
	}
	if ag := New(HSP, DefaultFeatures|FeatureCodecNegotiation); ag.CodecNegotiation() {
		t.Fatal("codec negotiation with HSP")
	}
}

func TestCodecListBounds(t *testing.T) {
	ag := New(HFP, DefaultFeatures)
	for _, l := range []string{"AT+BAC=", "AT+BAC=0", "AT+BAC=1,x", "AT+BAC=256", "AT+BAC=1,2,3,4,5,6,7,8,9", "AT+BAC?"} {
		expectLines(t, ag, l, "ERROR")
	}
	expectLines(t, ag, "AT+BAC=1,2,3,4,5,6,7,8", "OK")
}
