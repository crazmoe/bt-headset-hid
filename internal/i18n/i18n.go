// Package i18n picks the language of the user's messages: German where the
// locale says so, English otherwise.
package i18n

import "strings"

// German reports whether the user's language, from the locale variables
// read by getenv, is German.
func German(getenv func(string) string) bool {
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if l := getenv(v); l != "" {
			return strings.HasPrefix(l, "de")
		}
	}
	return false
}

// Message is a desktop notification in German and English.
type Message struct {
	SummaryDE, BodyDE string
	SummaryEN, BodyEN string
}

// Pick returns the summary and body in German if de, else in English.
func (m Message) Pick(de bool) (summary, body string) {
	if de {
		return m.SummaryDE, m.BodyDE
	}
	return m.SummaryEN, m.BodyEN
}

// New returns the message with summary and body in German and English.
func New(summaryDE, bodyDE, summaryEN, bodyEN string) Message {
	return Message{summaryDE, bodyDE, summaryEN, bodyEN}
}
