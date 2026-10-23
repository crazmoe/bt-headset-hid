package wpconf

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// KnownPath is the list of headsets that were paired when the headsets were
// last set up, below the configuration directory dir. A headset paired later
// is not on it, and "bt-headset-hid session" sets it up by itself.
func KnownPath(dir string) string {
	return filepath.Join(dir, "bt-headset-hid", "known-headsets")
}

// Known returns the headsets on the known list, and false if there is no
// list yet.
func Known(dir string) ([]string, bool) {
	data, err := os.ReadFile(KnownPath(dir))
	if err != nil {
		return nil, false
	}
	var addrs []string
	for _, l := range strings.Split(string(data), "\n") {
		if l = strings.ToUpper(strings.TrimSpace(l)); addressRE.MatchString(l) {
			addrs = append(addrs, l)
		}
	}
	return addrs, true
}

// SetKnown writes the known list: the headsets in addrs.
func SetKnown(dir string, addrs []string) error {
	seen := map[string]bool{}
	var list []string
	for _, a := range addrs {
		if a = strings.ToUpper(strings.TrimSpace(a)); addressRE.MatchString(a) && !seen[a] {
			seen[a] = true
			list = append(list, a)
		}
	}
	sort.Strings(list)
	var b strings.Builder
	b.WriteString("# Headsets that were paired when bt-headset-hid last set them up; written by \"bt-headset-hid\".\n")
	b.WriteString("# A headset paired later is not listed, and \"bt-headset-hid session\" sets it up by itself.\n")
	for _, a := range list {
		b.WriteString(a + "\n")
	}
	p := KnownPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(b.String()), 0o644)
}

// RemoveKnown deletes the known list and reports whether it existed.
func RemoveKnown(dir string) (bool, error) {
	err := os.Remove(KnownPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	os.Remove(filepath.Dir(KnownPath(dir))) // only if empty
	return err == nil, err
}

// NewHeadsets returns the paired headsets that are neither set up nor on the
// known list, the ones to set up by themselves. Without a known list, from
// before there was one, there are none: the headsets paired then were left
// out on purpose or not.
func NewHeadsets(paired, configured, known []string, haveKnown bool) []string {
	if !haveKnown {
		return nil
	}
	skip := map[string]bool{}
	for _, a := range append(append([]string{}, configured...), known...) {
		skip[strings.ToUpper(a)] = true
	}
	var add []string
	for _, a := range paired {
		if u := strings.ToUpper(a); !skip[u] {
			skip[u] = true
			add = append(add, u)
		}
	}
	return add
}
