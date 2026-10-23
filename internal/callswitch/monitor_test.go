package callswitch

import "testing"

func TestMonitorFilter(t *testing.T) {
	var f MonitorFilter
	steps := []struct {
		name, data string
		want       bool
	}{
		{"first dump", `[{"id":0,"type":"PipeWire:Interface:Core"},{"id":45,"type":"PipeWire:Interface:Node"},{"id":60,"type":"PipeWire:Interface:Port"}]`, true},
		{"pw-dump connects", `[{"id":127,"type":"PipeWire:Interface:Client","info":{"change-mask":["props"]}}]`, false},
		{"pw-dump leaves", `[{"id":127,"info":null}]`, false},
		{"port changes", `[{"id":60,"type":"PipeWire:Interface:Port","info":{}}]`, false},
		{"port removed", `[{"id":60,"info":null}]`, false},
		{"node changes", `[{"id":45,"type":"PipeWire:Interface:Node","info":{}}]`, true},
		{"default device", `[{"id":31,"type":"PipeWire:Interface:Metadata","metadata":[]}]`, true},
		{"headset connects", `[{"id":80,"type":"PipeWire:Interface:Device","info":{}},{"id":128,"type":"PipeWire:Interface:Client"}]`, true},
		{"node removed", `[{"id":45,"info":null}]`, true},
		{"unknown removed", `[{"id":99,"info":null}]`, true},
		{"id reused by a client", `[{"id":45,"type":"PipeWire:Interface:Client"}]`, false},
		{"empty", `[]`, false},
	}
	for _, s := range steps {
		got, err := f.Relevant([]byte(s.data))
		if err != nil || got != s.want {
			t.Errorf("%s: %v, %v; want %v", s.name, got, err, s.want)
		}
	}
	if _, err := f.Relevant([]byte(`{"id":1}`)); err == nil {
		t.Error("no error for an object instead of an array")
	}
}
