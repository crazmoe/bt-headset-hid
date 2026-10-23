package callswitch

import "encoding/json"

// MonitorFilter tells which updates of "pw-dump --monitor" may concern the
// headsets, the speaker and microphone "bt-headset-hid" or the default
// devices: those of nodes, devices and metadata. The others are ignored,
// above all the client that every run of pw-dump adds and removes: answering
// it with pw-dump would cause the next one, round and round.
type MonitorFilter struct {
	types map[int]string // type of each object seen, by id
}

// Relevant reports whether the update data, one JSON array printed by
// "pw-dump --monitor", changes a node, device or metadata object. An
// object that is removed only carries its id, so the types of the objects
// seen before are remembered.
func (f *MonitorFilter) Relevant(data []byte) (bool, error) {
	var objs []struct {
		ID   int    `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &objs); err != nil {
		return false, err
	}
	if f.types == nil {
		f.types = map[int]string{}
	}
	relevant := false
	for _, o := range objs {
		t := o.Type
		if t == "" {
			// Removed. An object not seen before may be anything.
			var ok bool
			if t, ok = f.types[o.ID]; !ok {
				relevant = true
				continue
			}
			delete(f.types, o.ID)
		} else {
			f.types[o.ID] = t
		}
		switch t {
		case "PipeWire:Interface:Node", "PipeWire:Interface:Device", "PipeWire:Interface:Metadata":
			relevant = true
		}
	}
	return relevant, nil
}
