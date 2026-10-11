package consensus

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseVoteTable(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want Vote
		err  error
	}{
		{"JSON fence", "```JSON\n{\"final_answer\":\"a\",\"agree\":true}\n```", Vote{FinalAnswer: "a", Agree: true}, nil},
		{"vote fence", "```vote\n{\"final_answer\":\"a\"}\n```", Vote{FinalAnswer: "a"}, nil},
		{"council-vote fence", "```council-vote  \n{\"final_answer\":\"a\"}\n```", Vote{FinalAnswer: "a"}, nil},
		{"bare fence", "```\n{\"final_answer\":\"a\"}\n```", Vote{FinalAnswer: "a"}, nil},
		{"CRLF", "```json\r\n{\"final_answer\":\"a\"}\r\n```", Vote{FinalAnswer: "a"}, nil},
		{"answer trimmed", "```json\n{\"final_answer\":\"  a  \"}\n```", Vote{FinalAnswer: "a"}, nil},
		{"object answer verbatim", "```json\n{\"final_answer\":{\"x\":1}}\n```", Vote{FinalAnswer: `{"x":1}`}, nil},
		{"null final_answer uses answer", "```json\n{\"final_answer\":null,\"answer\":\"b\"}\n```", Vote{FinalAnswer: "b"}, nil},
		{"agree beats agreement", "```json\n{\"final_answer\":\"a\",\"agree\":false,\"agreement\":true}\n```", Vote{FinalAnswer: "a"}, nil},
		{"negative confidence clamped", "```json\n{\"final_answer\":\"a\",\"confidence\":-3}\n```", Vote{FinalAnswer: "a"}, nil},
		{"agrees_with", "```json\n{\"final_answer\":\"a\",\"agrees_with\":[\"x\",\"y\"],\"confidence\":0.25}\n```", Vote{FinalAnswer: "a", AgreesWith: []string{"x", "y"}, Confidence: 0.25}, nil},
		{"broken fence falls back to bare JSON", "```json\n{\"final_answer\": oops}\n```\nCorrected: {\"final_answer\": \"c\"}", Vote{FinalAnswer: "c"}, nil},
		{"fence without answer key falls back", "```json\n{\"x\":1}\n```\n{\"final_answer\":\"d\"}", Vote{FinalAnswer: "d"}, nil},
		{"newest bare object wins", `{"final_answer":"old"} then {"final_answer":"new"}`, Vote{FinalAnswer: "new"}, nil},
		{"bare object with escaped quote and brace", `{"final_answer":"say \"}\" ok"}`, Vote{FinalAnswer: `say "}" ok`}, nil},
		{"nested bare object", `{"final_answer":"n","meta":{"k":{"z":1}}}`, Vote{FinalAnswer: "n"}, nil},
		{"bare objects without answer skipped", `{"a":1} {"b":2}`, Vote{}, ErrNoVote},
		{"answer key but invalid JSON", `{"answer": nope}`, Vote{}, ErrNoVote},
		{"empty", "", Vote{}, ErrNoVote},
		{"unbalanced", `{"final_answer":"x"`, Vote{}, ErrNoVote},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := ParseVote(c.msg)
			if !errors.Is(err, c.err) || (err == nil) != (c.err == nil) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
			if !reflect.DeepEqual(v, c.want) {
				t.Fatalf("got %+v, want %+v", v, c.want)
			}
		})
	}
}

func TestStripVoteTable(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  plain text  ", "plain text"},
		{"Prose.\n```json\n{\"final_answer\":\"a\"}\n```\n", "Prose."},
		{"Code:\n```go\nx := 1\n```\nDone.", "Code:\n```go\nx := 1\n```\nDone."},
		{"Before\n```json\n{\"final_answer\":\"a\"}\n```\nAfter", "Before\n\nAfter"},
		{"```json\n{\"final_answer\":\"old\"}\n```\nmid\n```json\n{\"final_answer\":\"new\"}\n```", "```json\n{\"final_answer\":\"old\"}\n```\nmid"},
		{"```json\n{not json}\n```", "```json\n{not json}\n```"},
	}
	for _, c := range cases {
		if got := StripVote(c.in); got != c.want {
			t.Errorf("StripVote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestJSONObjects(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"no braces", nil},
		{`a {"x":1} b {"y":{"z":2}}`, []string{`{"x":1}`, `{"y":{"z":2}}`}},
		{`{"s":"}{"}`, []string{`{"s":"}{"}`}},
		{`{"s":"a\"}b"}`, []string{`{"s":"a\"}b"}`}},
		{`"quoted {not an object}" {"k":1}`, []string{`{not an object}`, `{"k":1}`}},
		{`}} {"a":1}`, []string{`{"a":1}`}},
		{`{"open":`, nil},
	}
	for _, c := range cases {
		if got := jsonObjects(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("jsonObjects(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAgreementTable(t *testing.T) {
	cases := []struct {
		name     string
		ballots  []Ballot
		min      int
		explicit bool
		ok       bool
		answer   string
	}{
		{"none", nil, 0, false, false, ""},
		{"below quorum", []Ballot{b("a", "x", true, 0)}, 2, false, false, ""},
		{"single agent quorum 1", []Ballot{b("a", "x", false, 0)}, 1, false, true, "x"},
		{"empty answer", []Ballot{b("a", "", true, 0), b("b", "", true, 0)}, 2, false, false, ""},
		{"normalized match keeps first text", []Ballot{b("a", "Use a Mutex", true, 0), b("b", "use a mutex.", true, 0)}, 2, true, true, "Use a Mutex"},
		{"explicit missing", []Ballot{b("a", "x", true, 0), b("b", "x", false, 0)}, 2, true, false, ""},
		{"mismatch", []Ballot{b("a", "x", true, 0), b("b", "y", true, 0)}, 2, false, false, ""},
	}
	for _, c := range cases {
		d, ok := Agreement(c.ballots, c.min, c.explicit)
		if ok != c.ok || d.Answer != c.answer {
			t.Errorf("%s: got %+v %v", c.name, d, ok)
		}
		if ok && (d.Method != MethodUnanimous || d.Share != 1 || len(d.Supporters) != len(c.ballots)) {
			t.Errorf("%s: bad decision %+v", c.name, d)
		}
		if !ok && d.Method != MethodNone {
			t.Errorf("%s: method %s", c.name, d.Method)
		}
	}
}

func TestMajorityTable(t *testing.T) {
	cases := []struct {
		name    string
		ballots []Ballot
		want    Decision
	}{
		{"all empty", []Ballot{b("a", "", false, 1), b("b", " ", false, 1)}, Decision{Method: MethodNone}},
		{"single group is unanimous", []Ballot{b("a", "x", false, 0.1), b("b", "X.", false, 0.9)},
			Decision{Answer: "X.", Method: MethodUnanimous, Supporters: []string{"a", "b"}, Share: 1}},
		{"abstention counts against share", []Ballot{b("a", "x", false, 0), b("b", "x", false, 0), b("c", "y", false, 0), b("d", "", false, 0)},
			Decision{Answer: "x", Method: MethodMajority, Supporters: []string{"a", "b"}, Dissenters: []string{"c"}, Share: 0.5}},
		{"count beats confidence", []Ballot{b("a", "y", false, 1), b("b", "x", false, 0), b("c", "x", false, 0)},
			Decision{Answer: "x", Method: MethodMajority, Supporters: []string{"b", "c"}, Dissenters: []string{"a"}, Share: 2.0 / 3}},
		{"summed confidence tie-break", []Ballot{b("a", "x", false, 0.5), b("b", "x", false, 0.5), b("c", "y", false, 0.6), b("d", "y", false, 0.6)},
			Decision{Answer: "y", Method: MethodMajority, Supporters: []string{"c", "d"}, Dissenters: []string{"a", "b"}, Share: 0.5}},
		{"three groups", []Ballot{b("a", "x", false, 0), b("b", "y", false, 0), b("c", "z", false, 0), b("d", "z", false, 0)},
			Decision{Answer: "z", Method: MethodMajority, Supporters: []string{"c", "d"}, Dissenters: []string{"a", "b"}, Share: 0.5}},
	}
	for _, c := range cases {
		if got := Majority(c.ballots); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}
