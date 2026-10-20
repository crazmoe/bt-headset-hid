// Package atparser splits the RFCOMM byte stream of a headset into AT command
// lines and parses them. It performs no I/O of its own beyond reading from the
// io.Reader it is given and treats all input as untrusted.
package atparser

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// MaxLineLen is the default upper bound for a single AT command line.
// HFP commands are short; anything longer is discarded.
const MaxLineLen = 256

// ErrLineTooLong is returned by ReadLine when a line exceeded the length limit.
// The offending line has been discarded and reading can continue.
var ErrLineTooLong = errors.New("atparser: line too long")

// ErrSyntax is returned by Parse for lines that are not well-formed AT commands.
var ErrSyntax = errors.New("atparser: syntax error")

// Reader yields AT command lines from a byte stream. Lines are terminated by
// CR or LF; empty lines are skipped.
type Reader struct {
	r   *bufio.Reader
	max int
}

// NewReader returns a Reader with a line limit of MaxLineLen.
func NewReader(r io.Reader) *Reader {
	return NewReaderSize(r, MaxLineLen)
}

// NewReaderSize returns a Reader with the given line limit (at least 1).
func NewReaderSize(r io.Reader, max int) *Reader {
	if max < 1 {
		max = 1
	}
	return &Reader{r: bufio.NewReader(r), max: max}
}

// ReadLine returns the next non-empty line without its terminator and with
// surrounding whitespace removed. At the end of the stream it returns io.EOF;
// a trailing unterminated line is returned before that. ErrLineTooLong is not
// fatal: the next call continues with the following line.
func (r *Reader) ReadLine() (string, error) {
	var buf []byte
	tooLong := false
	for {
		b, err := r.r.ReadByte()
		if err != nil {
			if err == io.EOF && len(buf) > 0 && !tooLong {
				if line := strings.TrimSpace(string(buf)); line != "" {
					return line, nil
				}
			}
			if err == io.EOF && tooLong {
				return "", ErrLineTooLong
			}
			return "", err
		}
		if b == '\r' || b == '\n' {
			if tooLong {
				return "", ErrLineTooLong
			}
			if line := strings.TrimSpace(string(buf)); line != "" {
				return line, nil
			}
			buf = buf[:0]
			continue
		}
		if tooLong {
			continue
		}
		if len(buf) >= r.max {
			tooLong = true
			buf = nil
			continue
		}
		buf = append(buf, b)
	}
}

// Type is the form of an AT command.
type Type int

const (
	// Exec is a plain command such as "ATA" or "AT+CHUP".
	Exec Type = iota
	// Set assigns values: "AT+VGS=7".
	Set
	// Read queries the current value: "AT+CIND?".
	Read
	// Test queries the supported values: "AT+CIND=?".
	Test
)

func (t Type) String() string {
	switch t {
	case Exec:
		return "exec"
	case Set:
		return "set"
	case Read:
		return "read"
	case Test:
		return "test"
	}
	return "unknown"
}

// Command is a parsed AT command line.
type Command struct {
	// Name is the upper-cased command name without the "AT" prefix,
	// e.g. "A", "D", "+CHUP", "+VGS".
	Name string
	Type Type
	// Args is the raw argument string after "=" (Set) or after a basic
	// command letter such as the number in "ATD123;".
	Args string
}

// Params splits Args at commas outside of double quotes and trims spaces.
// Quotes are kept. An empty Args yields nil.
func (c Command) Params() []string {
	if c.Args == "" {
		return nil
	}
	var out []string
	start, quoted := 0, false
	for i := 0; i < len(c.Args); i++ {
		switch c.Args[i] {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				out = append(out, strings.TrimSpace(c.Args[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(c.Args[start:]))
}

// Parse parses a single AT command line as returned by Reader.ReadLine.
// Only one command per line is supported, which is all HFP/HSP headsets send.
func Parse(line string) (Command, error) {
	line = strings.TrimSpace(line)
	if len(line) < 3 || !strings.EqualFold(line[:2], "AT") {
		return Command{}, ErrSyntax
	}
	rest := line[2:]

	// Extended command: "+NAME", optionally followed by "=?", "?", or "=args".
	if rest[0] == '+' {
		n := 1
		for n < len(rest) && isNameChar(rest[n]) {
			n++
		}
		if n == 1 {
			return Command{}, ErrSyntax
		}
		cmd := Command{Name: strings.ToUpper(rest[:n])}
		tail := rest[n:]
		switch {
		case tail == "":
			cmd.Type = Exec
		case tail == "?":
			cmd.Type = Read
		case tail == "=?":
			cmd.Type = Test
		case tail[0] == '=':
			cmd.Type = Set
			cmd.Args = tail[1:]
		default:
			return Command{}, ErrSyntax
		}
		if !isPrintable(cmd.Args) {
			return Command{}, ErrSyntax
		}
		return cmd, nil
	}

	// Basic command: a single letter, e.g. "ATA" or "ATD0123456789;".
	c := rest[0] | 0x20 // lower-case ASCII letters
	if c < 'a' || c > 'z' || !isPrintable(rest[1:]) {
		return Command{}, ErrSyntax
	}
	cmd := Command{Name: strings.ToUpper(rest[:1]), Type: Exec, Args: rest[1:]}
	if cmd.Args == "?" {
		cmd.Type, cmd.Args = Read, ""
	}
	return cmd, nil
}

func isNameChar(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_' || b == '%' || b == '-'
}

func isPrintable(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}
