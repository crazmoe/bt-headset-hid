package callswitch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ParseCards returns the devices named in names from the output of pw-dump,
// and marks the one whose node is the default output (default.audio.sink in
// the "default" metadata). pw-dump (at least 1.0) may print more than one
// array, later ones updating or removing (info null) objects of earlier ones;
// they are merged in order.
func ParseCards(dump []byte, names map[string]bool) ([]Card, error) {
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
			Props  map[string]any `json:"props"`
			Params struct {
				EnumProfile []profileParam `json:"EnumProfile"`
				Profile     []profileParam `json:"Profile"`
			} `json:"params"`
		} `json:"info"`
	}
	byID := map[uint32]*Card{}
	var order []uint32
	nodeDevice := map[string]uint32{} // node.name -> device.id
	defaultSink := ""
	dec := json.NewDecoder(bytes.NewReader(dump))
	for n := 0; ; n++ {
		var objs []object
		err := dec.Decode(&objs)
		if errors.Is(err, io.EOF) && n > 0 {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("pw-dump: %w", err)
		}
		for _, o := range objs {
			if o.Type == "PipeWire:Interface:Metadata" && o.Props["metadata.name"] == "default" {
				for _, m := range o.Metadata {
					if m.Subject != 0 || m.Key != "default.audio.sink" {
						continue
					}
					defaultSink = sinkName(m.Value)
				}
				continue
			}
			if o.Type == "PipeWire:Interface:Node" && o.Info != nil {
				name, _ := o.Info.Props["node.name"].(string)
				if dev, ok := o.Info.Props["device.id"].(float64); ok && name != "" {
					nodeDevice[name] = uint32(dev)
				}
				continue
			}
			c := byID[o.ID]
			if o.Info == nil {
				if o.Type == "" { // removed
					delete(byID, o.ID)
				}
				continue
			}
			if c == nil {
				if o.Type != "PipeWire:Interface:Device" {
					continue
				}
				c = &Card{ID: o.ID}
				byID[o.ID] = c
				order = append(order, o.ID)
			}
			if name, ok := o.Info.Props["device.name"].(string); ok {
				c.Name = name
			}
			if ps := o.Info.Params.EnumProfile; len(ps) > 0 {
				c.Profiles = map[string]int{}
				for _, p := range ps {
					c.Profiles[p.Name] = p.Index
				}
			}
			if ps := o.Info.Params.Profile; len(ps) > 0 {
				c.Profile = ps[0].Name
			}
		}
	}
	var cards []Card
	defDevice, hasDefault := nodeDevice[defaultSink]
	for _, id := range order {
		if c := byID[id]; c != nil && names[c.Name] {
			c.Default = hasDefault && defDevice == id
			if c.Profiles == nil {
				c.Profiles = map[string]int{}
			}
			cards = append(cards, *c)
			delete(byID, id) // an id listed twice in order
		}
	}
	return cards, nil
}

type profileParam struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
}

// sinkName reads the value of default.audio.sink, {"name": "..."}, which
// may also come as a string holding that JSON.
func sinkName(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		raw = json.RawMessage(str)
	}
	var v struct{ Name string }
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return v.Name
}

// ParseOutput returns the default output the user chose
// (default.configured.audio.sink) from the output of pw-dump, or "".
func ParseOutput(dump []byte) (string, error) {
	type object struct {
		Type     string         `json:"type"`
		Props    map[string]any `json:"props"`
		Metadata []struct {
			Subject uint32          `json:"subject"`
			Key     string          `json:"key"`
			Value   json.RawMessage `json:"value"`
		} `json:"metadata"`
	}
	out := ""
	dec := json.NewDecoder(bytes.NewReader(dump))
	for n := 0; ; n++ {
		var objs []object
		err := dec.Decode(&objs)
		if errors.Is(err, io.EOF) && n > 0 {
			return out, nil
		}
		if err != nil {
			return "", fmt.Errorf("pw-dump: %w", err)
		}
		for _, o := range objs {
			if o.Type != "PipeWire:Interface:Metadata" || o.Props["metadata.name"] != "default" {
				continue
			}
			for _, m := range o.Metadata {
				if m.Subject == 0 && m.Key == "default.configured.audio.sink" {
					out = sinkName(m.Value)
				}
			}
		}
	}
}
