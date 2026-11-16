package session

import (
	"log"

	"github.com/crazmoe/bt-headset-hid/internal/callswitch"
	"github.com/crazmoe/bt-headset-hid/internal/desktop"
)

// routeAudio points the speaker and microphone "bt-headset-hid" at the call
// audio of the headset in addrs that is connected, preferring device, the
// one that took the call, and with phone keeps the connected headsets in the
// phone profile.
func routeAudio(addrs []string, device string, phone bool, env callswitch.Env, logger *log.Logger) {
	if phone {
		cards, err := env.Cards()
		if err != nil {
			logger.Printf("headsets: %v", err)
		}
		for _, c := range cards {
			p := c.PhoneProfile()
			if callswitch.IsPhone(c.Profile) || p == "" {
				continue
			}
			if err := env.SetProfile(c, p); err != nil {
				logger.Printf("%s: profile %s: %v", c.Name, p, err)
				continue
			}
			logger.Printf("%s: %s -> %s", c.Name, c.Profile, p)
		}
	}
	r, err := desktop.Routing()
	if err != nil {
		logger.Printf("routing: %v", err)
		return
	}
	ch := callswitch.RouteFor(r, addrs, device)
	for _, c := range ch {
		if err := desktop.SetTarget(c); err != nil {
			logger.Printf("routing: %v", err)
			return
		}
	}
	if len(ch) > 0 {
		logger.Printf("bt-headset-hid: %s", callswitch.Describe(ch, r))
	}
}
