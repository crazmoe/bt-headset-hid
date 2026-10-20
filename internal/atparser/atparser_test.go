package atparser

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func readAll(t *testing.T, r *Reader) ([]string, int) {
	t.Helper()
	var lines []string
	tooLong := 0
	for {
		line, err := r.ReadLine()
		switch {
		case err == io.EOF:
			return lines, tooLong
		case errors.Is(err, ErrLineTooLong):
			tooLong++
		case err != nil:
			t.Fatalf("ReadLine: %v", err)
		default:
			lines = append(lines, line)
		}
	}
}

func TestReadLine(t *testing.T) {
	in := "AT+BRSF=191\r\r\nAT+CIND=?\r  ATA \nAT+CHUP"
	// OneByteReader makes sure lines split across reads are reassembled.
	r := NewReader(iotest.OneByteReader(strings.NewReader(in)))
	got, tooLong := readAll(t, r)
	want := []string{"AT+BRSF=191", "AT+CIND=?", "ATA", "AT+CHUP"}
	if !reflect.DeepEqual(got, want) || tooLong != 0 {
		t.Fatalf("got %q (tooLong=%d), want %q", got, tooLong, want)
	}
}

func TestReadLineTooLong(t *testing.T) {
	in := "ATA\r" + strings.Repeat("X", 20) + "\rAT+CHUP\r" + strings.Repeat("Y", 11)
	got, tooLong := readAll(t, NewReaderSize(strings.NewReader(in), 10))
	want := []string{"ATA", "AT+CHUP"}
	if !reflect.DeepEqual(got, want) || tooLong != 2 {
		t.Fatalf("got %q (tooLong=%d), want %q and 2", got, tooLong, want)
	}
}

func TestReadLineExactLimit(t *testing.T) {
	got, tooLong := readAll(t, NewReaderSize(strings.NewReader("AT+VGS=15\r"), 9))
	if !reflect.DeepEqual(got, []string{"AT+VGS=15"}) || tooLong != 0 {
		t.Fatalf("got %q (tooLong=%d)", got, tooLong)
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want Command
	}{
		{"ATA", Command{Name: "A", Type: Exec}},
		{"ata", Command{Name: "A", Type: Exec}},
		{"ATD0123456789;", Command{Name: "D", Type: Exec, Args: "0123456789;"}},
		{"AT+CHUP", Command{Name: "+CHUP", Type: Exec}},
		{"AT+BLDN", Command{Name: "+BLDN", Type: Exec}},
		{"AT+CKPD=200", Command{Name: "+CKPD", Type: Set, Args: "200"}},
		{"AT+BRSF=191", Command{Name: "+BRSF", Type: Set, Args: "191"}},
		{"AT+CIND=?", Command{Name: "+CIND", Type: Test}},
		{"AT+CIND?", Command{Name: "+CIND", Type: Read}},
		{"AT+CMER=3,0,0,1", Command{Name: "+CMER", Type: Set, Args: "3,0,0,1"}},
		{"at+vgs=7", Command{Name: "+VGS", Type: Set, Args: "7"}},
		{"AT+XAPL=0B0E-1234-0100,10", Command{Name: "+XAPL", Type: Set, Args: "0B0E-1234-0100,10"}},
		{"AT+BAC=", Command{Name: "+BAC", Type: Set}},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Parse(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{
		"", "A", "AT", "OK", "XTA", "AT+", "AT+=1", "AT+CIND!",
		"AT1", "AT+VGS=\x00", "ATD\x7f", "AT+CHUP?x",
	} {
		if _, err := Parse(in); !errors.Is(err, ErrSyntax) {
			t.Errorf("Parse(%q) error = %v, want ErrSyntax", in, err)
		}
	}
}

func TestParams(t *testing.T) {
	tests := []struct {
		args string
		want []string
	}{
		{"", nil},
		{"7", []string{"7"}},
		{"3, 0,0 ,1", []string{"3", "0", "0", "1"}},
		{`1,"a,b",2`, []string{"1", `"a,b"`, "2"}},
		{"1,", []string{"1", ""}},
	}
	for _, tt := range tests {
		if got := (Command{Args: tt.args}).Params(); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Params(%q) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"ATA", "AT+CIND=?", "AT+VGS=7", "ATD1;", `AT+X="a,b"`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		cmd, err := Parse(s)
		if err != nil {
			return
		}
		if cmd.Name == "" || strings.ToUpper(cmd.Name) != cmd.Name {
			t.Fatalf("Parse(%q) gave bad name %q", s, cmd.Name)
		}
		_ = cmd.Params()
	})
}
