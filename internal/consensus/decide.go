package consensus

import (
	"sort"
	"strings"
	"unicode"
)

// Ballot is one agent's vote in one round.
type Ballot struct {
	Agent string
	Vote  Vote
}

// Method names how a decision was reached.
type Method string

const (
	MethodUnanimous Method = "unanimous"
	MethodMajority  Method = "majority"
	MethodModerator Method = "moderator"
	MethodNone      Method = "none"
)

// Decision is the outcome of tallying a round of ballots.
type Decision struct {
	Answer     string   `json:"answer"`
	Method     Method   `json:"method"`
	Supporters []string `json:"supporters"`
	Dissenters []string `json:"dissenters,omitempty"`
	// Share is the fraction of ballots supporting Answer.
	Share float64 `json:"share"`
}

// Normalize canonicalises an answer for comparison: case-folded, whitespace
// collapsed, surrounding quotes/backticks and trailing punctuation removed.
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, "\"'`")
	s = strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
	s = strings.TrimRightFunc(s, func(r rune) bool { return r == '.' || r == '!' || r == ';' || r == ',' })
	return strings.TrimSpace(s)
}

// Agreement reports whether the ballots constitute consensus: every ballot
// names the same normalized answer, and (unless independent is true) every
// agent explicitly sets agree=true. minVoters guards against declaring
// consensus when most agents failed.
func Agreement(ballots []Ballot, minVoters int, requireExplicit bool) (Decision, bool) {
	if len(ballots) == 0 || len(ballots) < minVoters {
		return Decision{Method: MethodNone}, false
	}
	first := Normalize(ballots[0].Vote.FinalAnswer)
	if first == "" {
		return Decision{Method: MethodNone}, false
	}
	names := make([]string, 0, len(ballots))
	for _, b := range ballots {
		if Normalize(b.Vote.FinalAnswer) != first {
			return Decision{Method: MethodNone}, false
		}
		if requireExplicit && !b.Vote.Agree {
			return Decision{Method: MethodNone}, false
		}
		names = append(names, b.Agent)
	}
	return Decision{Answer: ballots[0].Vote.FinalAnswer, Method: MethodUnanimous, Supporters: names, Share: 1}, true
}

// Majority picks the answer with the most supporters. Ties are broken by the
// summed confidence of supporters, then by the earliest ballot. The returned
// answer text is taken from the highest-confidence supporter.
func Majority(ballots []Ballot) Decision {
	type group struct {
		key      string
		answer   string
		bestConf float64
		conf     float64
		first    int
		agents   []string
	}
	groups := map[string]*group{}
	var order []*group
	for i, b := range ballots {
		k := Normalize(b.Vote.FinalAnswer)
		if k == "" {
			continue
		}
		g, ok := groups[k]
		if !ok {
			g = &group{key: k, answer: b.Vote.FinalAnswer, bestConf: -1, first: i}
			groups[k] = g
			order = append(order, g)
		}
		g.agents = append(g.agents, b.Agent)
		g.conf += b.Vote.Confidence
		if b.Vote.Confidence > g.bestConf {
			g.bestConf, g.answer = b.Vote.Confidence, b.Vote.FinalAnswer
		}
	}
	if len(order) == 0 {
		return Decision{Method: MethodNone}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if len(a.agents) != len(b.agents) {
			return len(a.agents) > len(b.agents)
		}
		if a.conf != b.conf {
			return a.conf > b.conf
		}
		return a.first < b.first
	})
	win := order[0]
	var dissent []string
	for _, g := range order[1:] {
		dissent = append(dissent, g.agents...)
	}
	m := MethodMajority
	if len(order) == 1 {
		m = MethodUnanimous
	}
	return Decision{
		Answer:     win.answer,
		Method:     m,
		Supporters: win.agents,
		Dissenters: dissent,
		Share:      float64(len(win.agents)) / float64(len(ballots)),
	}
}
