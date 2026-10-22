package callswitch

import (
	"encoding/json"
	"fmt"
)

// ParseCards returns the devices named in names from the output of pw-dump.
func ParseCards(dump []byte, names map[string]bool) ([]Card, error) {
	var objs []struct {
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
	if err := json.Unmarshal(dump, &objs); err != nil {
		return nil, fmt.Errorf("pw-dump: %w", err)
	}
	var cards []Card
	for _, o := range objs {
		if o.Type != "PipeWire:Interface:Device" || o.Info == nil {
			continue
		}
		name, _ := o.Info.Props["device.name"].(string)
		if !names[name] {
			continue
		}
		c := Card{ID: o.ID, Name: name, Profiles: map[string]int{}}
		for _, p := range o.Info.Params.EnumProfile {
			c.Profiles[p.Name] = p.Index
		}
		if len(o.Info.Params.Profile) > 0 {
			c.Profile = o.Info.Params.Profile[0].Name
		}
		cards = append(cards, c)
	}
	return cards, nil
}

type profileParam struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
}
