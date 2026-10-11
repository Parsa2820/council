package harness

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFirstLine(t *testing.T) {
	long := strings.Repeat("x", 250)
	cases := []struct{ in, want string }{
		{"", ""},
		{"  one  ", "one"},
		{"one\ntwo\nthree", "one"},
		{"\n\n  first\nsecond", "first"},
		{long, strings.Repeat("x", 200) + "…"},
		{strings.Repeat("x", 199) + "é", strings.Repeat("x", 199) + "…"},
	}
	for _, c := range cases {
		got := firstLine(c.in)
		if got != c.want || !utf8.ValidString(got) {
			t.Errorf("firstLine(%.20q…) = %.30q…, want %.30q…", c.in, got, c.want)
		}
	}
}

func TestTail(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "…llo"},
		{"héllo", 4, "…llo"}, // the last 4 bytes start inside é
		{"日本語", 3, "…語"},
		{"日本語", 1, "…"},
	}
	for _, c := range cases {
		got := tail(c.in, c.n)
		if got != c.want || !utf8.ValidString(got) {
			t.Errorf("tail(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
