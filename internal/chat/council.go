package chat

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Parsa2820/council/internal/consensus"
	"github.com/Parsa2820/council/internal/harness"
)

// EventKind identifies a live progress event.
type EventKind string

const (
	EventRoundStart     EventKind = "round_start"
	EventAgentMessage   EventKind = "agent_message"
	EventConsensus      EventKind = "consensus"
	EventNoConsensus    EventKind = "no_consensus"
	EventModeratorStart EventKind = "moderator_start"
	EventModerator      EventKind = "moderator_message"
)

// Event is emitted as the chat progresses (used by --verbose).
type Event struct {
	Kind    EventKind
	Round   int
	Message *Message
	Text    string
}

// Options configure a council run.
type Options struct {
	// Rounds is the maximum number of rounds (>= 1).
	Rounds int
	// Moderator synthesizes the final answer; nil means majority vote.
	Moderator harness.Harness
	// RequireExplicitAgreement: from round 2 on, consensus needs every agent
	// to vote agree=true in addition to naming the same answer.
	RequireExplicitAgreement bool
	// MaxTranscriptChars truncates each earlier message shown to agents.
	MaxTranscriptChars int
	// TurnTimeout bounds each agent turn (0 = rely on the harness timeout).
	TurnTimeout time.Duration
	// MinVoters is the quorum for consensus; default min(2, #agents).
	MinVoters int
	// OnEvent receives live progress events. Calls are serialized.
	OnEvent func(Event)
}

// Result is the outcome of a council run.
type Result struct {
	Question   string                    `json:"question"`
	Answer     string                    `json:"answer"`
	Details    string                    `json:"details,omitempty"`
	Method     consensus.Method          `json:"method"`
	Converged  bool                      `json:"converged"`
	Rounds     int                       `json:"rounds"`
	Supporters []string                  `json:"supporters,omitempty"`
	Dissenters []string                  `json:"dissenters,omitempty"`
	Moderator  string                    `json:"moderator,omitempty"`
	Notes      []string                  `json:"notes,omitempty"`
	Votes      map[string]consensus.Vote `json:"votes"`
	Transcript []Message                 `json:"transcript"`
}

// Council orchestrates a group chat among harnesses.
type Council struct {
	agents []harness.Harness
	opts   Options
	emitMu sync.Mutex
}

// New creates a council. Agent names must be unique.
func New(agents []harness.Harness, opts Options) (*Council, error) {
	if len(agents) == 0 {
		return nil, errors.New("council needs at least one agent")
	}
	seen := map[string]bool{}
	for _, a := range agents {
		if seen[a.Name()] {
			return nil, fmt.Errorf("duplicate agent %q", a.Name())
		}
		seen[a.Name()] = true
	}
	if opts.Rounds < 1 {
		opts.Rounds = 1
	}
	if opts.MinVoters <= 0 {
		opts.MinVoters = min(2, len(agents))
	}
	return &Council{agents: agents, opts: opts}, nil
}

func (c *Council) emit(e Event) {
	if c.opts.OnEvent == nil {
		return
	}
	c.emitMu.Lock()
	defer c.emitMu.Unlock()
	c.opts.OnEvent(e)
}

func (c *Council) names() []string {
	n := make([]string, len(c.agents))
	for i, a := range c.agents {
		n[i] = a.Name()
	}
	return n
}

// Run executes the discussion and returns the consensus result. The returned
// Result is non-nil whenever the transcript has content, even on error.
func (c *Council) Run(ctx context.Context, question string) (*Result, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, errors.New("empty question")
	}
	tr := NewTranscript(question)
	names := c.names()
	latest := map[string]consensus.Vote{} // most recent vote per agent
	res := &Result{Question: question}

	var agreed *consensus.Decision
	for round := 1; round <= c.opts.Rounds; round++ {
		if err := ctx.Err(); err != nil {
			return c.finish(res, tr, latest), err
		}
		res.Rounds = round
		c.emit(Event{Kind: EventRoundStart, Round: round})
		discussion := tr.Discussion(round-1, c.opts.MaxTranscriptChars)

		msgs := c.runRound(ctx, question, names, round, discussion)
		var ballots []consensus.Ballot
		ok := 0
		for _, m := range msgs {
			tr.Add(m)
			if m.OK() {
				ok++
			}
			if m.Vote != nil {
				latest[m.Agent] = *m.Vote
				ballots = append(ballots, consensus.Ballot{Agent: m.Agent, Vote: *m.Vote})
			}
		}
		if ok == 0 {
			return c.finish(res, tr, latest), fmt.Errorf("round %d: every agent failed (first error: %s)", round, msgs[0].Error)
		}
		explicit := c.opts.RequireExplicitAgreement && round > 1
		// Consensus needs every agent that is still responding to have voted.
		if len(ballots) == ok {
			if d, yes := consensus.Agreement(ballots, c.opts.MinVoters, explicit); yes {
				agreed = &d
				c.emit(Event{Kind: EventConsensus, Round: round, Text: d.Answer})
				break
			}
		}
		if round == c.opts.Rounds {
			c.emit(Event{Kind: EventNoConsensus, Round: round})
		}
	}

	res.Converged = agreed != nil
	// Tally the latest vote of every agent, in agent order.
	var final []consensus.Ballot
	for _, n := range names {
		if v, ok := latest[n]; ok {
			final = append(final, consensus.Ballot{Agent: n, Vote: v})
		}
	}
	if len(final) == 0 && c.opts.Moderator == nil {
		return c.finish(res, tr, latest), errors.New("no agent produced a parsable vote block")
	}

	decision := consensus.Majority(final)
	if agreed != nil {
		decision = *agreed
	}
	res.Answer, res.Method = decision.Answer, decision.Method
	res.Supporters, res.Dissenters = decision.Supporters, decision.Dissenters
	res.Details = c.supporterDetails(tr, res.Rounds, decision.Supporters)

	if c.opts.Moderator != nil {
		c.moderate(ctx, res, tr, final)
	}
	return c.finish(res, tr, latest), nil
}

func (c *Council) finish(res *Result, tr *Transcript, latest map[string]consensus.Vote) *Result {
	res.Transcript = tr.Messages()
	res.Votes = latest
	return res
}

// runRound runs every agent concurrently and returns messages in agent order.
func (c *Council) runRound(ctx context.Context, question string, names []string, round int, discussion string) []Message {
	msgs := make([]Message, len(c.agents))
	var wg sync.WaitGroup
	for i, a := range c.agents {
		wg.Add(1)
		go func(i int, a harness.Harness) {
			defer wg.Done()
			prompt := RoundPrompt(question, a.Name(), names, round, c.opts.Rounds, discussion)
			m := c.turn(ctx, a, prompt, round, RoleAgent)
			msgs[i] = m
			c.emit(Event{Kind: EventAgentMessage, Round: round, Message: &m})
		}(i, a)
	}
	wg.Wait()
	return msgs
}

func (c *Council) turn(ctx context.Context, a harness.Harness, prompt string, round int, role Role) Message {
	if c.opts.TurnTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.TurnTimeout)
		defer cancel()
	}
	start := time.Now()
	out, err := a.Run(ctx, prompt)
	m := Message{Round: round, Agent: a.Name(), Role: role, Duration: time.Since(start), Time: start}
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		m.Error = err.Error()
		return m
	}
	m.Content = out
	if v, verr := consensus.ParseVote(out); verr == nil {
		m.Vote = &v
	}
	return m
}

func (c *Council) supporterDetails(tr *Transcript, round int, supporters []string) string {
	if len(supporters) == 0 {
		return ""
	}
	want := map[string]bool{}
	for _, s := range supporters {
		want[s] = true
	}
	var best *Message
	msgs := tr.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != RoleAgent || !m.OK() || m.Vote == nil || !want[m.Agent] {
			continue
		}
		if best == nil || (m.Round == best.Round && m.Vote.Confidence > best.Vote.Confidence) {
			mm := m
			best = &mm
		}
		if m.Round < best.Round {
			break
		}
	}
	if best == nil {
		return ""
	}
	return consensus.StripVote(best.Content)
}

func (c *Council) moderate(ctx context.Context, res *Result, tr *Transcript, ballots []consensus.Ballot) {
	mod := c.opts.Moderator
	res.Moderator = mod.Name()
	var tally strings.Builder
	sorted := append([]consensus.Ballot(nil), ballots...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Agent < sorted[j].Agent })
	for _, b := range sorted {
		fmt.Fprintf(&tally, "- %s: %q (agree=%v, confidence=%.2f)\n", b.Agent, b.Vote.FinalAnswer, b.Vote.Agree, b.Vote.Confidence)
	}
	if tally.Len() == 0 {
		tally.WriteString("(no parsable votes)\n")
	}
	prompt := ModeratorPrompt(res.Question, mod.Name(), c.names(), res.Rounds, res.Converged,
		tr.Discussion(res.Rounds, c.opts.MaxTranscriptChars), tally.String())
	c.emit(Event{Kind: EventModeratorStart, Round: res.Rounds})
	m := c.turn(ctx, mod, prompt, res.Rounds+1, RoleModerator)
	tr.Add(m)
	c.emit(Event{Kind: EventModerator, Round: res.Rounds + 1, Message: &m})
	if !m.OK() {
		res.Notes = append(res.Notes, fmt.Sprintf("moderator %s failed (%s); fell back to majority vote", mod.Name(), oneLine(m.Error, 200)))
		return
	}
	res.Method = consensus.MethodModerator
	res.Details = consensus.StripVote(m.Content)
	if m.Vote != nil && m.Vote.FinalAnswer != "" {
		res.Answer = m.Vote.FinalAnswer
	} else {
		res.Answer = res.Details
		res.Notes = append(res.Notes, "moderator gave no vote block; using its full reply as the answer")
	}
	// Recompute who supports the moderator's answer.
	key := consensus.Normalize(res.Answer)
	res.Supporters, res.Dissenters = nil, nil
	for _, b := range ballots {
		if consensus.Normalize(b.Vote.FinalAnswer) == key {
			res.Supporters = append(res.Supporters, b.Agent)
		} else {
			res.Dissenters = append(res.Dissenters, b.Agent)
		}
	}
}
