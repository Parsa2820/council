package consensus

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseVoteFenced(t *testing.T) {
	msg := "I think it's 42.\n\n```json\n{\"final_answer\": \"42\", \"agree\": true, \"agrees_with\": [\"b\"], \"confidence\": 0.8}\n```\n"
	v, err := ParseVote(msg)
	if err != nil {
		t.Fatal(err)
	}
	want := Vote{FinalAnswer: "42", Agree: true, AgreesWith: []string{"b"}, Confidence: 0.8}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("got %+v want %+v", v, want)
	}
}

func TestParseVoteTakesLastBlock(t *testing.T) {
	msg := "Example:\n```json\n{\"final_answer\": \"old\"}\n```\nmore\n```json\n{\"final_answer\": \"new\", \"agree\": false}\n```"
	v, err := ParseVote(msg)
	if err != nil || v.FinalAnswer != "new" {
		t.Fatalf("got %+v, %v", v, err)
	}
}

func TestParseVoteSkipsNonVoteFences(t *testing.T) {
	msg := "```json\n{\"final_answer\": \"x\", \"agree\": true}\n```\n```go\nfunc main() {}\n```"
	v, err := ParseVote(msg)
	if err != nil || v.FinalAnswer != "x" || !v.Agree {
		t.Fatalf("got %+v, %v", v, err)
	}
}

func TestParseVoteBareJSONAndAliases(t *testing.T) {
	msg := `Final: {"answer": 7, "agreement": true, "note": "has } brace"}`
	v, err := ParseVote(msg)
	if err != nil {
		t.Fatal(err)
	}
	if v.FinalAnswer != "7" || !v.Agree {
		t.Fatalf("got %+v", v)
	}
}

func TestParseVoteMissing(t *testing.T) {
	if _, err := ParseVote("no structure here {not json}"); !errors.Is(err, ErrNoVote) {
		t.Fatalf("want ErrNoVote, got %v", err)
	}
}

func TestParseVoteClampsConfidence(t *testing.T) {
	v, _ := ParseVote("```json\n{\"final_answer\":\"a\",\"confidence\":7}\n```")
	if v.Confidence != 1 {
		t.Fatalf("confidence not clamped: %v", v.Confidence)
	}
}

func TestStripVote(t *testing.T) {
	msg := "Reasoning here.\n```json\n{\"final_answer\":\"a\"}\n```"
	if got := StripVote(msg); got != "Reasoning here." {
		t.Fatalf("got %q", got)
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"  Hello   World. ": "hello world",
		"`42`":              "42",
		"\"Use a Mutex!\"":  "use a mutex",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q)=%q want %q", in, got, want)
		}
	}
}

func b(agent, ans string, agree bool, conf float64) Ballot {
	return Ballot{Agent: agent, Vote: Vote{FinalAnswer: ans, Agree: agree, Confidence: conf}}
}

func TestAgreement(t *testing.T) {
	ballots := []Ballot{b("a", "42", true, 0), b("b", "42.", true, 0), b("c", " 42", true, 0)}
	d, ok := Agreement(ballots, 2, true)
	if !ok || d.Method != MethodUnanimous || len(d.Supporters) != 3 {
		t.Fatalf("expected unanimous, got %+v %v", d, ok)
	}
	ballots[1].Vote.Agree = false
	if _, ok := Agreement(ballots, 2, true); ok {
		t.Fatal("explicit agreement required but one agent disagreed")
	}
	if _, ok := Agreement(ballots, 2, false); !ok {
		t.Fatal("identical answers should agree when explicit flag not required")
	}
	if _, ok := Agreement(ballots[:1], 2, false); ok {
		t.Fatal("quorum not met")
	}
	ballots[2].Vote.FinalAnswer = "41"
	if _, ok := Agreement(ballots, 2, false); ok {
		t.Fatal("different answers must not agree")
	}
}

func TestMajority(t *testing.T) {
	d := Majority([]Ballot{b("a", "x", false, 0.2), b("b", "y", false, 0.9), b("c", "X", false, 0.5)})
	if Normalize(d.Answer) != "x" || d.Method != MethodMajority {
		t.Fatalf("got %+v", d)
	}
	if !reflect.DeepEqual(d.Supporters, []string{"a", "c"}) || !reflect.DeepEqual(d.Dissenters, []string{"b"}) {
		t.Fatalf("supporters/dissenters wrong: %+v", d)
	}
	if d.Answer != "X" { // text from the most confident supporter
		t.Fatalf("answer text should come from most confident supporter, got %q", d.Answer)
	}
}

func TestMajorityTieBreaks(t *testing.T) {
	d := Majority([]Ballot{b("a", "x", false, 0.1), b("b", "y", false, 0.9)})
	if d.Answer != "y" {
		t.Fatalf("confidence tiebreak failed: %+v", d)
	}
	d = Majority([]Ballot{b("a", "x", false, 0), b("b", "y", false, 0)})
	if d.Answer != "x" {
		t.Fatalf("order tiebreak failed: %+v", d)
	}
	if d := Majority(nil); d.Method != MethodNone {
		t.Fatalf("empty ballots: %+v", d)
	}
}
