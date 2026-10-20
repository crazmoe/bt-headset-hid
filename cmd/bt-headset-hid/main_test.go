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
