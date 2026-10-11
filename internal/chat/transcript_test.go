package chat

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel"},
		{"hello", 0, ""},
		{"héllo", 2, "h"}, // é is 2 bytes: never cut it in half
		{"héllo", 3, "hé"},
		{"日本語", 4, "日"},
		{"日本語", 2, ""},
		{"😀x", 3, ""},
	}
	for _, c := range cases {
		got := truncate(c.in, c.n)
		if got != c.want || !utf8.ValidString(got) || len(got) > c.n && len(c.in) > c.n {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestOneLine(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"a\n  b\tc", 100, "a b c"},
		{"abcdef", 3, "abc…"},
		{"  ", 3, ""},
		{"ééé", 3, "é…"},
	}
	for _, c := range cases {
		if got := oneLine(c.in, c.n); got != c.want {
			t.Errorf("oneLine(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestDiscussionTruncatesOnRuneBoundary(t *testing.T) {
	tr := NewTranscript("q")
	tr.Add(Message{Round: 1, Agent: "a", Role: RoleAgent, Content: "héllo wörld"})
	d := tr.Discussion(1, 2)
	if !utf8.ValidString(d) {
		t.Fatalf("discussion is not valid UTF-8: %q", d)
	}
	if !strings.Contains(d, "#### a\nh\n…[truncated]") {
		t.Fatalf("got %q", d)
	}
}
