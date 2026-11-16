package wpsetup

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/crazmoe/bt-headset-hid/internal/bluez"
)

// DroppedHeadsets returns the headsets of before that are not in addrs.
func DroppedHeadsets(before, addrs []string) []string {
	var dropped []string
	for _, b := range before {
		found := false
		for _, a := range addrs {
			if strings.EqualFold(a, b) {
				found = true
			}
		}
		if !found {
			dropped = append(dropped, b)
		}
	}
	return dropped
}

// Addresses returns the addresses of hs.
func Addresses(hs []bluez.Headset) []string {
	var addrs []string
	for _, h := range hs {
		addrs = append(addrs, h.Address)
	}
	return addrs
}

// ChooseHeadsets picks the headsets to set up from the paired ones hs. With
// several and interactive, it asks on in and out; the default is the
// connected ones, or all if none is.
func ChooseHeadsets(hs []bluez.Headset, in io.Reader, interactive bool, out io.Writer) []bluez.Headset {
	if len(hs) <= 1 {
		return hs
	}
	var def []bluez.Headset
	var defNums []string
	for i, h := range hs {
		if h.Connected {
			def = append(def, h)
			defNums = append(defNums, strconv.Itoa(i+1))
		}
	}
	if len(def) == 0 {
		def, defNums = hs, []string{"all"}
	}
	if !interactive {
		fmt.Fprintf(out, "several headsets are paired, setting up %s; to pick others: -headset XX:XX:XX:XX:XX:XX,...\n",
			strings.Join(Addresses(def), ", "))
		return def
	}
	fmt.Fprintln(out, "Several headsets are paired. Which ones do you use for calls?")
	for i, h := range hs {
		state := ""
		if h.Connected {
			state = ", connected"
		}
		fmt.Fprintf(out, "  %d) %s (%s%s)\n", i+1, h.Name, h.Address, state)
	}
	r := bufio.NewReader(in)
	for {
		fmt.Fprintf(out, "Numbers separated by commas, \"all\" for all [%s]: ", strings.Join(defNums, ","))
		line, err := r.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer == "" {
			if err != nil {
				fmt.Fprintln(out)
			}
			return def
		}
		if answer == "all" || answer == "a" {
			return hs
		}
		if picked, ok := pickNumbers(answer, hs); ok {
			return picked
		}
		if err != nil {
			fmt.Fprintln(out)
			return def
		}
	}
}

// pickNumbers returns the headsets numbered (from 1) in answer, such as
// "1,3" or "1 3".
func pickNumbers(answer string, hs []bluez.Headset) ([]bluez.Headset, bool) {
	var picked []bluez.Headset
	seen := map[int]bool{}
	for _, f := range strings.FieldsFunc(answer, func(r rune) bool { return r == ',' || r == ' ' }) {
		n, err := strconv.Atoi(f)
		if err != nil || n < 1 || n > len(hs) {
			return nil, false
		}
		if !seen[n] {
			seen[n] = true
			picked = append(picked, hs[n-1])
		}
	}
	return picked, len(picked) > 0
}
