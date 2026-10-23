package callswitch

import (
	"reflect"
	"testing"
)

// routeDump is pw-dump output during a call on the JBL: the fixed streams
// (37, 40), the JBL (99) with its call audio (106, 105), the Jabra (98) with
// call audio named by older headset rules (120, 121), and a target set
// before. A second array removes a node and changes the metadata.
const routeDump = `[
 {"id": 43, "type": "PipeWire:Interface:Metadata", "props": {"metadata.name": "default"},
  "metadata": [
   {"subject": 0, "key": "default.audio.sink", "type": "Spa:String:JSON", "value": {"name": "alsa_output.x"}},
   {"subject": 37, "key": "target.object", "type": "Spa:Id", "value": "999"},
   {"subject": 112, "key": "target.node", "type": "Spa:Id", "value": 106}
  ]},
 {"id": 37, "type": "PipeWire:Interface:Node", "info": {"props": {"node.name": "bt-headset-hid.speaker.out", "object.serial": 38}}},
 {"id": 40, "type": "PipeWire:Interface:Node", "info": {"props": {"node.name": "bt-headset-hid.microphone.in", "object.serial": 41}}},
 {"id": 99, "type": "PipeWire:Interface:Device", "info": {"props": {"device.name": "bluez_card.84_D3_52_8C_B9_D0"}}},
 {"id": 98, "type": "PipeWire:Interface:Device", "info": {"props": {"device.name": "bluez_card.50_C2_75_0D_4E_03"}}},
 {"id": 106, "type": "PipeWire:Interface:Node", "info": {"props": {"node.name": "bluez_output.84_D3_52_8C_B9_D0.1", "factory.name": "api.bluez5.sco.sink", "device.id": 99, "object.serial": 110}}},
 {"id": 105, "type": "PipeWire:Interface:Node", "info": {"props": {"node.name": "bluez_input.84_D3_52_8C_B9_D0.0", "factory.name": "api.bluez5.sco.source", "device.id": 99, "object.serial": 109}}},
 {"id": 107, "type": "PipeWire:Interface:Node", "info": {"props": {"node.name": "bluez_output.84_D3_52_8C_B9_D0.1", "factory.name": "api.bluez5.a2dp.sink", "device.id": 99, "object.serial": 111}}},
 {"id": 120, "type": "PipeWire:Interface:Node", "info": {"props": {"node.name": "bluez_output.bt-headset-hid.1", "factory.name": "api.bluez5.sco.sink", "device.id": 98, "object.serial": 130}}},
 {"id": 121, "type": "PipeWire:Interface:Node", "info": {"props": {"node.name": "bluez_input.bt-headset-hid.0", "factory.name": "api.bluez5.sco.source", "device.id": 98, "object.serial": 131}}},
 {"id": 122, "type": "PipeWire:Interface:Node", "info": {"props": {"node.name": "gone", "factory.name": "api.bluez5.sco.sink", "device.id": 99, "object.serial": 132}}}
]
[
 {"id": 122, "info": null},
 {"id": 43, "type": "PipeWire:Interface:Metadata", "props": {"metadata.name": "default"},
  "metadata": [{"subject": 40, "key": "target.object", "type": "Spa:Id", "value": 109}]}
]`

func TestParseRouting(t *testing.T) {
	r, err := ParseRouting([]byte(routeDump))
	if err != nil {
		t.Fatal(err)
	}
	if r.Speaker != (Node{37, "38"}) || r.Mic != (Node{40, "41"}) {
		t.Errorf("streams = %v, %v", r.Speaker, r.Mic)
	}
	want := map[string]Audio{
		"bluez_card.84_D3_52_8C_B9_D0": {Output: Node{106, "110"}, Input: Node{105, "109"}},
		"bluez_card.50_C2_75_0D_4E_03": {Output: Node{120, "130"}, Input: Node{121, "131"}},
	}
	if !reflect.DeepEqual(r.Headsets, want) {
		t.Errorf("headsets = %v", r.Headsets)
	}
	if !reflect.DeepEqual(r.Target, map[uint32]string{37: "999", 40: "109"}) {
		t.Errorf("targets = %v", r.Target)
	}
	if _, err := ParseRouting([]byte("not json")); err == nil {
		t.Error("no error for bad input")
	}
}

func TestRouteFor(t *testing.T) {
	r, err := ParseRouting([]byte(routeDump))
	if err != nil {
		t.Fatal(err)
	}
	const jbl, jabra = "84:D3:52:8C:B9:D0", "50:C2:75:0D:4E:03"
	for _, c := range []struct {
		name   string
		addrs  []string
		device string
		want   []Change
	}{
		{"first in order", []string{jabra, jbl}, "", []Change{{37, "130"}, {40, "131"}}},
		{"the one that took the call", []string{jabra, jbl}, jbl, []Change{{37, "110"}}},
		{"call device unknown", []string{jbl}, "00:11:22:33:44:55", []Change{{37, "110"}}},
		{"not set up", []string{"00:11:22:33:44:55"}, "", []Change{{37, ""}, {40, ""}}},
	} {
		if got := RouteFor(r, c.addrs, c.device); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: RouteFor = %v, want %v", c.name, got, c.want)
		}
	}
	r.Target = map[uint32]string{}
	if got := RouteFor(r, nil, ""); got != nil {
		t.Errorf("nothing set and no headset: %v", got)
	}
	r.Speaker = Node{}
	if got := RouteFor(r, []string{jbl}, ""); !reflect.DeepEqual(got, []Change{{40, "109"}}) {
		t.Errorf("without speaker stream: %v", got)
	}
	if got := Describe([]Change{{40, "109"}, {37, ""}}, Routing{Speaker: Node{37, ""}, Mic: Node{40, ""}}); got != "microphone -> node 109, speaker -> default device" {
		t.Errorf("Describe = %q", got)
	}
}
