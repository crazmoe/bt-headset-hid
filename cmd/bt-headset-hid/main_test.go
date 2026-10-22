package main

import (
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	if version == "" {
		t.Fatal("version must not be empty")
	}
}

func TestAskMode(t *testing.T) {
	for in, want := range map[string]string{"\n": "pipewire", "1\n": "pipewire", "2\n": "hfp", "x\nhfp\n": "hfp", "2": "hfp"} {
		got, err := askMode(strings.NewReader(in))
		if err != nil || got != want {
			t.Errorf("askMode(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "x"} {
		if got, err := askMode(strings.NewReader(in)); err == nil {
			t.Errorf("askMode(%q) = %q, want error", in, got)
		}
	}
}

func TestDroppedHeadsets(t *testing.T) {
	before := []string{"50:C2:75:0D:4E:03", "84:D3:52:8C:B9:D0"}
	got := droppedHeadsets(before, []string{"84:d3:52:8c:b9:d0"})
	if len(got) != 1 || got[0] != "50:C2:75:0D:4E:03" {
		t.Errorf("droppedHeadsets = %q", got)
	}
	if got := droppedHeadsets(before, []string{"84:D3:52:8C:B9:D0", "50:c2:75:0d:4e:03"}); got != nil {
		t.Errorf("droppedHeadsets with both = %q", got)
	}
}
