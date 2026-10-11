package cli

import (
	"flag"
	"io"
	"testing"
	"unicode/utf8"
)

func TestOneLine(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"a\n  b\tc", 100, "a b c"},
		{"abcdef", 3, "abc…"},
		{"", 3, ""},
		{"ééé", 3, "é…"},
		{"日本", 2, "…"},
	}
	for _, c := range cases {
		got := oneLine(c.in, c.n)
		if got != c.want || !utf8.ValidString(got) {
			t.Errorf("oneLine(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func newTestFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}
