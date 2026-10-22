package callswitch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ParseCards returns the devices named in names from the output of pw-dump.
// pw-dump (at least 1.0) may print more than one array, later ones updating
// or removing (info null) objects of earlier ones; they are merged in order.
func ParseCards(dump []byte, names map[string]bool) ([]Card, error) {
	type object struct {
		ID   uint32 `json:"id"`
		Type string `json:"type"`
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
	for _, id := range order {
		if c := byID[id]; c != nil && names[c.Name] {
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
