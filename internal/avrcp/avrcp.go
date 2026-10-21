// Package avrcp takes over the media keys of Bluetooth headsets. BlueZ turns
// the AVRCP buttons of a headset into an input device named "<name> (AVRCP)";
// outside a call (and with some headsets also during one) the call button
// arrives there as Play/Pause instead of as an HFP AT command. In the mode
// without our own HFP connection, grabbing that device makes the button
// reach the softphone instead of the media player.
//
// This file is the pure part: device matching and key mapping.
package avrcp

import (
	"encoding/binary"
	"errors"
	"strings"
	"time"
	"unsafe"

	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

// Linux input constants (linux/input-event-codes.h).
const (
	evKey = 0x01

	keyPause     = 119
	keyPlayPause = 164
	keyPlayCD    = 200
	keyPauseCD   = 201
	keyPlay      = 207

	busBluetooth = 0x05
)

// Match reports whether an input device is the AVRCP device BlueZ creates
// for a headset.
func Match(name string, bus uint16) bool {
	return bus == busBluetooth && strings.HasSuffix(name, " (AVRCP)")
}

// eventSize is sizeof(struct input_event): a struct timeval, then
// __u16 type, __u16 code, __s32 value.
var eventSize = int(unsafe.Sizeof(timeval{})) + 8

type timeval struct{ sec, usec int } // long, long

// ErrShortEvent means a read returned less than one input event.
var ErrShortEvent = errors.New("avrcp: short input event")

// Decoder turns input events into headset events. A key press within
// Debounce of the previous one is dropped, because some headsets send
// Play and Pause for a single press.
type Decoder struct {
	Debounce time.Duration
	last     time.Time
}

// Decode parses one struct input_event read at now. It returns an
// hfp.Button event for a press of a play/pause key.
func (d *Decoder) Decode(b []byte, now time.Time) (hfp.Event, bool, error) {
	if len(b) < eventSize {
		return hfp.Event{}, false, ErrShortEvent
	}
	o := eventSize - 8
	typ := binary.NativeEndian.Uint16(b[o:])
	code := binary.NativeEndian.Uint16(b[o+2:])
	value := int32(binary.NativeEndian.Uint32(b[o+4:]))
	if typ != evKey || value != 1 { // 1 = press, 0 = release, 2 = repeat
		return hfp.Event{}, false, nil
	}
	switch code {
	case keyPlayPause, keyPlayCD, keyPauseCD, keyPlay, keyPause:
	default:
		return hfp.Event{}, false, nil
	}
	if !d.last.IsZero() && now.Sub(d.last) < d.Debounce {
		return hfp.Event{}, false, nil
	}
	d.last = now
	return hfp.Event{Kind: hfp.Button}, true, nil
}
