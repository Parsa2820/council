package chat

import (
	"strings"
	"testing"
	"time"
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

func TestTranscriptQuestionAndMessagesCopy(t *testing.T) {
	tr := NewTranscript("why?")
	if tr.Question() != "why?" {
		t.Fatalf("Question() = %q", tr.Question())
	}
	msgs := tr.Messages()
	if len(msgs) != 1 || msgs[0].Role != RoleUser || msgs[0].Content != "why?" || msgs[0].Agent != "user" {
		t.Fatalf("first message %+v", msgs)
	}
	msgs[0].Content = "mutated"
	if tr.Messages()[0].Content != "why?" {
		t.Fatal("Messages must return a copy")
	}
	if !(Message{}).OK() || (Message{Error: "x"}).OK() {
		t.Fatal("OK()")
	}
}

func transcriptFixture() *Transcript {
	tr := NewTranscript("  Q?  ")
	tr.Add(Message{Round: 1, Agent: "a", Role: RoleAgent, Content: " answer A1 ", Duration: 1500 * time.Millisecond})
	tr.Add(Message{Round: 1, Agent: "b", Role: RoleAgent, Error: "b: timed out\nafter 10m"})
	tr.Add(Message{Round: 2, Agent: "a", Role: RoleAgent, Content: "answer A2"})
	tr.Add(Message{Round: 3, Agent: "m", Role: RoleModerator, Content: "synthesis"})
	return tr
}

func TestDiscussionTable(t *testing.T) {
	tr := transcriptFixture()
	cases := []struct {
		upTo, max int
		want      string
	}{
		{0, 0, ""},
		{1, 0, "### Round 1\n\n#### a\nanswer A1\n\n#### b\n(no response this round: b: timed out after 10m)"},
		{2, 0, "### Round 1\n\n#### a\nanswer A1\n\n#### b\n(no response this round: b: timed out after 10m)\n\n### Round 2\n\n#### a\nanswer A2"},
		{9, 0, "### Round 1\n\n#### a\nanswer A1\n\n#### b\n(no response this round: b: timed out after 10m)\n\n### Round 2\n\n#### a\nanswer A2"}, // moderator excluded
		{1, 4, "### Round 1\n\n#### a\nans\n…[truncated]\n\n#### b\n(no response this round: b: timed out after 10m)"},
	}
	for _, c := range cases {
		if got := tr.Discussion(c.upTo, c.max); got != c.want {
			t.Errorf("Discussion(%d, %d):\n got %q\nwant %q", c.upTo, c.max, got, c.want)
		}
	}
}

func TestMarkdown(t *testing.T) {
	tr := transcriptFixture()
	md := tr.Markdown(nil)
	for _, want := range []string{
		"# Council transcript\n\n## Question\n\nQ?\n",
		"\n## Round 1\n\n### a _(1.5s)_\n\nanswer A1\n",
		"\n### b\n\n> **error:** b: timed out after 10m\n",
		"\n## Round 2\n\n### a\n\nanswer A2\n",
		"\n## Moderator: m\n\nsynthesis\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "## Result") {
		t.Error("no result section without a result")
	}
	full := tr.Markdown(&Result{Answer: "A2", Method: "moderator", Converged: false, Rounds: 2, Supporters: []string{"a"}, Dissenters: []string{"b", "c"}})
	if !strings.HasSuffix(full, "\n## Result\n\n- **Answer:** A2\n- **Method:** moderator\n- **Converged:** false after 2 round(s)\n- **Supporters:** a\n- **Dissenters:** b, c\n") {
		t.Errorf("result section:\n%s", full)
	}
	short := tr.Markdown(&Result{Answer: "x", Method: "majority", Rounds: 1})
	if strings.Contains(short, "Supporters") || strings.Contains(short, "Dissenters") {
		t.Error("empty supporter lists should be omitted")
	}
}

func TestNewTranscriptFrom(t *testing.T) {
	res := &Result{Question: "q", Transcript: transcriptFixture().Messages()}
	tr := NewTranscriptFrom(res)
	if tr.Question() != "q" || len(tr.Messages()) != 5 {
		t.Fatalf("got %q %d", tr.Question(), len(tr.Messages()))
	}
	tr.Add(Message{Round: 9})
	if len(res.Transcript) != 5 {
		t.Fatal("NewTranscriptFrom must not alias the result's slice")
	}
}
