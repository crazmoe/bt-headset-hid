package callswitch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// The streams of the fixed "bt-headset-hid" speaker and microphone that
// face the headset (contrib: wpconf.DevicesContent). "bt-headset-hid
// session" points them at the call audio of the connected headset.
const (
	SpeakerStream = "bt-headset-hid.speaker.out"
	MicStream     = "bt-headset-hid.microphone.in"
)

// TargetKey is the key in PipeWire's "default" metadata that tells
// WirePlumber where to link a stream; WirePlumber 0.4 and 0.5 read it.
const TargetKey = "target.object"

// Node is a PipeWire node.
type Node struct {
	ID     uint32
	Serial string // object.serial, what TargetKey names
}

// Audio is the call audio of one headset: its SCO sink and source.
type Audio struct {
	Output, Input Node
}

// Routing is what RouteFor needs from PipeWire.
type Routing struct {
	Speaker, Mic Node              // the streams; ID 0 if missing
	Target       map[uint32]string // TargetKey of a stream in the metadata
	Headsets     map[string]Audio  // call audio by card name (device.name)
}

// ParseRouting reads the streams of the fixed devices, the call audio of
// the headsets and the current targets from the output of pw-dump.
func ParseRouting(dump []byte) (Routing, error) {
	type object struct {
		ID       uint32         `json:"id"`
		Type     string         `json:"type"`
		Props    map[string]any `json:"props"` // of metadata
		Metadata []struct {
			Subject uint32          `json:"subject"`
			Key     string          `json:"key"`
			Value   json.RawMessage `json:"value"`
		} `json:"metadata"`
		Info *struct {
			Props map[string]any `json:"props"`
		} `json:"info"`
	}
	type node struct {
		name, factory, serial string
		device                uint32
	}
	nodes := map[uint32]node{}
	devices := map[uint32]string{}
	r := Routing{Target: map[uint32]string{}, Headsets: map[string]Audio{}}
	dec := json.NewDecoder(bytes.NewReader(dump))
	for n := 0; ; n++ {
		var objs []object
		err := dec.Decode(&objs)
		if errors.Is(err, io.EOF) && n > 0 {
			break
		}
		if err != nil {
			return Routing{}, fmt.Errorf("pw-dump: %w", err)
		}
		for _, o := range objs {
			switch {
			case o.Type == "PipeWire:Interface:Metadata" && o.Props["metadata.name"] == "default":
				for _, m := range o.Metadata {
					if m.Key == TargetKey && m.Subject != 0 {
						if v := metaValue(m.Value); v != "" {
							r.Target[m.Subject] = v
						} else {
							delete(r.Target, m.Subject)
						}
					}
				}
			case o.Info == nil:
				delete(nodes, o.ID)
				delete(devices, o.ID)
			case o.Type == "PipeWire:Interface:Node":
				p := o.Info.Props
				nd := node{serial: propString(p["object.serial"])}
				nd.name, _ = p["node.name"].(string)
				nd.factory, _ = p["factory.name"].(string)
				if d, ok := p["device.id"].(float64); ok {
					nd.device = uint32(d)
				}
				nodes[o.ID] = nd
			case o.Type == "PipeWire:Interface:Device":
				if name, ok := o.Info.Props["device.name"].(string); ok {
					devices[o.ID] = name
				}
			}
		}
	}
	for id, nd := range nodes {
		switch nd.name {
		case SpeakerStream:
			r.Speaker = Node{id, nd.serial}
			continue
		case MicStream:
			r.Mic = Node{id, nd.serial}
			continue
		}
		card, ok := devices[nd.device]
		if !ok || nd.device == 0 {
			continue
		}
		a := r.Headsets[card]
		// By factory, not by name: older headset rules renamed these nodes.
		switch nd.factory {
		case "api.bluez5.sco.sink":
			a.Output = Node{id, nd.serial}
		case "api.bluez5.sco.source":
			a.Input = Node{id, nd.serial}
		default:
			continue
		}
		r.Headsets[card] = a
	}
	return r, nil
}

// metaValue returns a metadata value as text: a number or a string.
func metaValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

func propString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatUint(uint64(v), 10)
	}
	return ""
}

// Change is one metadata change: set Key of Subject to Value, or delete it
// if Value is "".
type Change struct {
	Subject uint32
	Value   string
}

// RouteFor returns the changes that point the speaker and microphone streams
// at the call audio of a headset in addrs, in this order of preference: the
// one that took the call (device), then the first in addrs whose call audio
// is there. Without one, the targets are deleted, so WirePlumber links the
// streams to the default devices. Streams already right need no change.
func RouteFor(r Routing, addrs []string, device string) []Change {
	var a Audio
	found := false
	pick := func(addr string) bool {
		if h, ok := r.Headsets[CardName(addr)]; ok && h.Output.ID != 0 && h.Input.ID != 0 {
			a, found = h, true
		}
		return found
	}
	if device == "" || !pick(device) {
		for _, addr := range addrs {
			if pick(addr) {
				break
			}
		}
	}
	var ch []Change
	set := func(stream, target Node) {
		if stream.ID == 0 {
			return
		}
		if r.Target[stream.ID] != target.Serial {
			ch = append(ch, Change{stream.ID, target.Serial})
		}
	}
	set(r.Speaker, a.Output)
	set(r.Mic, a.Input)
	return ch
}

// Describe describes the changes for the log.
func Describe(ch []Change, r Routing) string {
	var parts []string
	for _, c := range ch {
		what := "speaker"
		if c.Subject == r.Mic.ID {
			what = "microphone"
		}
		to := "default device"
		if c.Value != "" {
			to = "node " + c.Value
		}
		parts = append(parts, what+" -> "+to)
	}
	return strings.Join(parts, ", ")
}
