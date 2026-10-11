// Package consensus parses the structured votes agents attach to their
// messages and decides whether, and on what, the group agrees.
package consensus

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// Vote is the structured block every agent ends its turn with.
//
// Agents are asked to finish with a fenced ```json block such as:
//
//	{"final_answer": "42", "agree": true, "agrees_with": ["claude"], "confidence": 0.9}
type Vote struct {
	// FinalAnswer is the agent's current, concise final answer.
	FinalAnswer string `json:"final_answer"`
	// Agree reports whether the agent believes the group has converged on
	// FinalAnswer (i.e. it accepts the answer the others are giving).
	Agree bool `json:"agree"`
	// AgreesWith optionally names the agents whose answers it endorses.
	AgreesWith []string `json:"agrees_with,omitempty"`
	// Confidence in [0,1]; optional, used to break majority-vote ties.
	Confidence float64 `json:"confidence,omitempty"`
}

// ErrNoVote is returned when a message carries no parsable vote block.
var ErrNoVote = errors.New("no vote block found")

var fenceRE = regexp.MustCompile("(?s)```(?:json|JSON|vote|council-vote)?[ \t]*\r?\n(.*?)```")

// rawVote accepts a few common spellings so slightly off-spec agents still count.
type rawVote struct {
	FinalAnswer *json.RawMessage `json:"final_answer"`
	Answer      *json.RawMessage `json:"answer"`
	Agree       *bool            `json:"agree"`
	Agreement   *bool            `json:"agreement"`
	AgreesWith  []string         `json:"agrees_with"`
	Confidence  float64          `json:"confidence"`
}

func (r rawVote) toVote() (Vote, bool) {
	ans := r.FinalAnswer
	if ans == nil {
		ans = r.Answer
	}
	if ans == nil {
		return Vote{}, false
	}
	var s string
	if err := json.Unmarshal(*ans, &s); err != nil {
		// Non-string answers (numbers, objects) are kept verbatim.
		s = strings.TrimSpace(string(*ans))
	}
	v := Vote{FinalAnswer: strings.TrimSpace(s), AgreesWith: r.AgreesWith, Confidence: r.Confidence}
	switch {
	case r.Agree != nil:
		v.Agree = *r.Agree
	case r.Agreement != nil:
		v.Agree = *r.Agreement
	}
	if v.Confidence < 0 {
		v.Confidence = 0
	}
	if v.Confidence > 1 {
		v.Confidence = 1
	}
	return v, true
}

func parseCandidate(s string) (Vote, bool) {
	var r rawVote
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &r); err != nil {
		return Vote{}, false
	}
	return r.toVote()
}

// ParseVote extracts the last vote block from an agent message. It prefers
// fenced code blocks and falls back to the last bare JSON object containing
// a "final_answer" key.
func ParseVote(msg string) (Vote, error) {
	matches := fenceRE.FindAllStringSubmatch(msg, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if v, ok := parseCandidate(matches[i][1]); ok {
			return v, nil
		}
	}
	// Fallback: scan for balanced {...} objects, newest first.
	objs := jsonObjects(msg)
	for i := len(objs) - 1; i >= 0; i-- {
		if !strings.Contains(objs[i], "answer") {
			continue
		}
		if v, ok := parseCandidate(objs[i]); ok {
			return v, nil
		}
	}
	return Vote{}, ErrNoVote
}

// StripVote removes the trailing vote block from a message, leaving the prose.
func StripVote(msg string) string {
	locs := fenceRE.FindAllStringSubmatchIndex(msg, -1)
	for i := len(locs) - 1; i >= 0; i-- {
		body := msg[locs[i][2]:locs[i][3]]
		if _, ok := parseCandidate(body); ok {
			return strings.TrimSpace(msg[:locs[i][0]] + msg[locs[i][1]:])
		}
	}
	return strings.TrimSpace(msg)
}

// jsonObjects returns every top-level balanced {...} substring, string-aware.
func jsonObjects(s string) []string {
	var out []string
	depth, start := 0, -1
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			if depth > 0 {
				inStr = true
			}
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 && start >= 0 {
					out = append(out, s[start:i+1])
					start = -1
				}
			}
		}
	}
	return out
}
