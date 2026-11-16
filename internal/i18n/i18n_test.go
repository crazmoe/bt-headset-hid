package i18n

import "testing"

func TestGerman(t *testing.T) {
	for _, c := range []struct {
		env  map[string]string
		want bool
	}{
		{nil, false},
		{map[string]string{"LANG": "de_CH.UTF-8"}, true},
		{map[string]string{"LANG": "en_US.UTF-8"}, false},
		// LC_ALL wins over LC_MESSAGES, which wins over LANG.
		{map[string]string{"LC_ALL": "en_GB.UTF-8", "LANG": "de_DE.UTF-8"}, false},
		{map[string]string{"LC_MESSAGES": "de_AT.UTF-8", "LANG": "en_US.UTF-8"}, true},
		{map[string]string{"LC_ALL": "de_DE.UTF-8", "LC_MESSAGES": "C"}, true},
	} {
		if got := German(func(k string) string { return c.env[k] }); got != c.want {
			t.Errorf("German(%v) = %v, want %v", c.env, got, c.want)
		}
	}
}

func TestPick(t *testing.T) {
	m := New("Akku", "leer", "Battery", "empty")
	if s, b := m.Pick(true); s != "Akku" || b != "leer" {
		t.Errorf("Pick(true) = %q, %q", s, b)
	}
	if s, b := m.Pick(false); s != "Battery" || b != "empty" {
		t.Errorf("Pick(false) = %q, %q", s, b)
	}
}
