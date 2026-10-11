package chat

import (
	"strings"
	"testing"
)

func TestHeader(t *testing.T) {
	cases := []struct {
		self          string
		agents        []string
		round, max    int
		want, notWant []string
	}{
		{"a", []string{"a"}, 1, 1, []string{`You are "a", one of 1 AI agents`, "You are working alone.", "ROUND 1 of at most 1."}, []string{"FINAL round"}},
		{"b", []string{"a", "b", "c"}, 1, 3, []string{"The other participants are: a, c.", "ROUND 1 of at most 3."}, []string{"FINAL round"}},
		{"b", []string{"a", "b"}, 3, 3, []string{"ROUND 3 of at most 3. This is the FINAL round"}, nil},
		{"b", []string{"a", "b"}, 2, 3, nil, []string{"FINAL round"}},
	}
	for _, c := range cases {
		h := header(c.self, c.agents, c.round, c.max)
		for _, w := range c.want {
			if !strings.Contains(h, w) {
				t.Errorf("header(%s, %v, %d, %d) missing %q:\n%s", c.self, c.agents, c.round, c.max, w, h)
			}
		}
		for _, w := range c.notWant {
			if strings.Contains(h, w) {
				t.Errorf("header(%s, %v, %d, %d) should not contain %q", c.self, c.agents, c.round, c.max, w)
			}
		}
		if !strings.Contains(h, "do not modify files") {
			t.Error("header must tell agents not to modify files")
		}
	}
}

func TestRoundPrompt(t *testing.T) {
	cases := []struct {
		name          string
		round         int
		discussion    string
		want, notWant []string
	}{
		{"round 1", 1, "", []string{"## User request\n\nQ?\n\n## Instructions\n\nAnswer independently"}, []string{"Discussion so far"}},
		{"round 1 ignores discussion", 1, "old", []string{"Answer independently"}, []string{"old"}},
		{"later round without discussion", 2, "", []string{"Answer independently"}, []string{"Discussion so far"}},
		{"later round", 2, "### Round 1\n...", []string{"## Discussion so far\n\n### Round 1\n...", "Critically review", `"agree": true only if`}, []string{"Answer independently"}},
	}
	for _, c := range cases {
		p := RoundPrompt("  Q?  ", "a", []string{"a", "b"}, c.round, 3, c.discussion)
		for _, w := range append(c.want, "## Response format", voteFormat) {
			if !strings.Contains(p, w) {
				t.Errorf("%s: missing %q", c.name, w)
			}
		}
		for _, w := range c.notWant {
			if strings.Contains(p, w) {
				t.Errorf("%s: should not contain %q", c.name, w)
			}
		}
	}
}

func TestModeratorPrompt(t *testing.T) {
	p := ModeratorPrompt(" Q? ", "judge", []string{"a", "b"}, 2, true, "DISCUSSION", "- a: \"x\"\n")
	for _, w := range []string{`You are "judge", the MODERATOR`, "(a, b)", "after 2 round(s) with consensus.", "## User request\n\nQ?\n", "## Discussion\n\nDISCUSSION", "## Latest votes\n\n- a: \"x\"", voteFormat} {
		if !strings.Contains(p, w) {
			t.Errorf("missing %q:\n%s", w, p)
		}
	}
	if p := ModeratorPrompt("q", "j", nil, 3, false, "", ""); !strings.Contains(p, "without reaching full consensus") {
		t.Errorf("non-converged state missing:\n%s", p)
	}
}
