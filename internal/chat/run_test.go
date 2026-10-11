package chat

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Parsa2820/council/internal/consensus"
	"github.com/Parsa2820/council/internal/harness"
)

func TestNewOptions(t *testing.T) {
	cases := []struct {
		agents        int
		in            Options
		wantRounds    int
		wantMinVoters int
	}{
		{1, Options{}, 1, 1},
		{2, Options{Rounds: -5}, 1, 2},
		{3, Options{Rounds: 4}, 4, 2},
		{3, Options{MinVoters: 3}, 1, 3},
	}
	for _, c := range cases {
		var agents []harness.Harness
		for i := 0; i < c.agents; i++ {
			agents = append(agents, stubborn(string(rune('a'+i)), "x"))
		}
		co, err := New(agents, c.in)
		if err != nil {
			t.Fatal(err)
		}
		if co.opts.Rounds != c.wantRounds || co.opts.MinVoters != c.wantMinVoters {
			t.Errorf("%d agents %+v: rounds %d minVoters %d", c.agents, c.in, co.opts.Rounds, co.opts.MinVoters)
		}
		if !reflect.DeepEqual(co.names(), []string{"a", "b", "c"}[:c.agents]) {
			t.Errorf("names() = %q", co.names())
		}
	}
	if _, err := New([]harness.Harness{stubborn("a", "x"), stubborn("a", "y")}, Options{}); err == nil || !strings.Contains(err.Error(), `duplicate agent "a"`) {
		t.Errorf("duplicate: %v", err)
	}
	if _, err := New(nil, Options{}); err == nil || !strings.Contains(err.Error(), "at least one agent") {
		t.Errorf("empty: %v", err)
	}
}

func TestRunCancelledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := New([]harness.Harness{stubborn("a", "x")}, Options{Rounds: 2})
	res, err := c.Run(ctx, "q")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if res == nil || res.Rounds != 0 || len(res.Transcript) != 1 || res.Votes == nil {
		t.Fatalf("result %+v", res)
	}
}

func TestRunCancelledBetweenRounds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := 0
	// Cancel once both round-1 replies are in, as if the user hit Ctrl-C
	// just before round 2.
	onEvent := func(e Event) {
		if e.Kind == EventAgentMessage {
			if n++; n == 2 {
				cancel()
			}
		}
	}
	c, _ := New([]harness.Harness{stubborn("a", "x"), stubborn("b", "y")}, Options{Rounds: 3, OnEvent: onEvent})
	res, err := c.Run(ctx, "q")
	if !errors.Is(err, context.Canceled) || res.Rounds != 1 || len(res.Votes) != 2 || len(res.Transcript) != 3 {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestCancelledMidRoundFailsTheRound(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := fake("a", func(int, string) (string, error) {
		cancel()
		return reply("", "x", false, 0.5), nil
	})
	c, _ := New([]harness.Harness{a}, Options{Rounds: 3})
	_, err := c.Run(ctx, "q")
	if err == nil || err.Error() != "round 1: every agent failed (first error: context canceled)" {
		t.Fatalf("got %v", err)
	}
}

func TestTurnSucceedsAfterDeadlineIsAnError(t *testing.T) {
	// A harness that ignores its context and returns "successfully" after
	// the turn deadline must not be counted.
	late := &harness.Func{AgentName: "late", Fn: func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return reply("", "x", true, 1), nil
	}}
	c, _ := New([]harness.Harness{late}, Options{Rounds: 1, TurnTimeout: 20 * time.Millisecond})
	m := c.turn(context.Background(), late, "p", 1, RoleAgent)
	if m.OK() || !strings.Contains(m.Error, "deadline exceeded") || m.Vote != nil || m.Content != "" {
		t.Fatalf("got %+v", m)
	}
	if m.Agent != "late" || m.Round != 1 || m.Role != RoleAgent || m.Time.IsZero() {
		t.Fatalf("metadata %+v", m)
	}
}

func TestTurnParsesVote(t *testing.T) {
	c, _ := New([]harness.Harness{stubborn("a", "x")}, Options{})
	cases := []struct {
		out      string
		wantVote bool
	}{
		{reply("text", "42", true, 0.5), true},
		{"no vote", false},
	}
	for _, tc := range cases {
		h := &harness.Func{AgentName: "a", Fn: func(context.Context, string) (string, error) { return tc.out, nil }}
		m := c.turn(context.Background(), h, "p", 2, RoleModerator)
		if !m.OK() || m.Content != tc.out || (m.Vote != nil) != tc.wantVote || m.Role != RoleModerator {
			t.Errorf("turn(%q) = %+v", tc.out, m)
		}
	}
}

func TestQuorumBlocksConsensus(t *testing.T) {
	broken := func(n string) harness.Harness {
		return fake(n, func(int, string) (string, error) { return "", errors.New("down") })
	}
	c, _ := New([]harness.Harness{stubborn("a", "x"), broken("b"), broken("c")}, Options{Rounds: 2})
	res, err := c.Run(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	// One voter out of three is below the default quorum of two.
	if res.Converged || res.Rounds != 2 || res.Answer != "x" || res.Method != consensus.MethodUnanimous {
		t.Fatalf("got %+v", res)
	}
}

func TestEventsSequence(t *testing.T) {
	var mu sync.Mutex
	var kinds []string
	mod := fake("mod", func(int, string) (string, error) { return reply("final", "x", true, 1), nil })
	c, _ := New([]harness.Harness{stubborn("a", "x"), stubborn("b", "x")}, Options{Rounds: 2, Moderator: mod, OnEvent: func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		k := string(e.Kind)
		if e.Message != nil {
			k += ":" + e.Message.Agent
		}
		kinds = append(kinds, k)
	}})
	if _, err := c.Run(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(kinds, " ")
	// Round-1 messages may arrive in either order.
	if got != "round_start agent_message:a agent_message:b consensus moderator_start moderator_message:mod" &&
		got != "round_start agent_message:b agent_message:a consensus moderator_start moderator_message:mod" {
		t.Fatalf("events: %s", got)
	}
	// emit without a handler is a no-op.
	(&Council{}).emit(Event{Kind: EventRoundStart})
}

func TestModeratorWithoutVoteBlock(t *testing.T) {
	mod := fake("mod", func(int, string) (string, error) { return "  Use tabs, with spaces for alignment.  ", nil })
	c, _ := New([]harness.Harness{stubborn("a", "tabs"), stubborn("b", "spaces")}, Options{Rounds: 1, Moderator: mod})
	res, err := c.Run(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != consensus.MethodModerator || res.Answer != "Use tabs, with spaces for alignment." || res.Details != res.Answer {
		t.Fatalf("got %+v", res)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "no vote block") {
		t.Fatalf("notes %q", res.Notes)
	}
	if len(res.Supporters) != 0 || !reflect.DeepEqual(res.Dissenters, []string{"a", "b"}) {
		t.Fatalf("supporters %v dissenters %v", res.Supporters, res.Dissenters)
	}
}

func TestModeratorWithNoVotesSeesEmptyTally(t *testing.T) {
	var prompt string
	mod := &harness.Func{AgentName: "mod", Fn: func(_ context.Context, p string) (string, error) {
		prompt = p
		return reply("decided", "z", true, 1), nil
	}}
	novote := fake("a", func(int, string) (string, error) { return "musing", nil })
	c, _ := New([]harness.Harness{novote}, Options{Rounds: 1, Moderator: mod})
	res, err := c.Run(context.Background(), "q")
	if err != nil || res.Answer != "z" || res.Method != consensus.MethodModerator {
		t.Fatalf("got %+v %v", res, err)
	}
	if !strings.Contains(prompt, "(no parsable votes)") || !strings.Contains(prompt, "without reaching full consensus") {
		t.Fatalf("moderator prompt:\n%s", prompt)
	}
}

func TestNoVotesWithoutModerator(t *testing.T) {
	novote := fake("a", func(int, string) (string, error) { return "musing", nil })
	c, _ := New([]harness.Harness{novote}, Options{Rounds: 1})
	res, err := c.Run(context.Background(), "q")
	if err == nil || !strings.Contains(err.Error(), "no agent produced a parsable vote block") || res == nil {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestSupporterDetails(t *testing.T) {
	c := &Council{}
	v := func(a string) *consensus.Vote { return &consensus.Vote{FinalAnswer: a, Confidence: 0.5} }
	hi := &consensus.Vote{FinalAnswer: "x", Confidence: 0.9}
	tr := NewTranscript("q")
	tr.Add(Message{Round: 1, Agent: "a", Role: RoleAgent, Content: "round1 a\n```json\n{\"final_answer\":\"x\"}\n```", Vote: v("x")})
	tr.Add(Message{Round: 2, Agent: "a", Role: RoleAgent, Content: "round2 a", Vote: v("x")})
	tr.Add(Message{Round: 2, Agent: "b", Role: RoleAgent, Content: "round2 b (confident)", Vote: hi})
	tr.Add(Message{Round: 2, Agent: "c", Role: RoleAgent, Error: "failed"})
	tr.Add(Message{Round: 2, Agent: "d", Role: RoleAgent, Content: "no vote"})
	tr.Add(Message{Round: 3, Agent: "m", Role: RoleModerator, Content: "mod", Vote: hi})
	cases := []struct {
		supporters []string
		want       string
	}{
		{nil, ""},
		{[]string{"a"}, "round2 a"},
		{[]string{"a", "b"}, "round2 b (confident)"},
		{[]string{"c", "d"}, ""},
		{[]string{"m"}, ""}, // moderator messages are not agent answers
		{[]string{"zzz"}, ""},
	}
	for _, tc := range cases {
		if got := c.supporterDetails(tr, 2, tc.supporters); got != tc.want {
			t.Errorf("supporterDetails(%v) = %q, want %q", tc.supporters, got, tc.want)
		}
	}
	// Only the newest round counts: a's round-1 message is never chosen once
	// a round-2 message exists, and its vote block is stripped otherwise.
	tr2 := NewTranscript("q")
	tr2.Add(tr.Messages()[1])
	if got := c.supporterDetails(tr2, 1, []string{"a"}); got != "round1 a" {
		t.Errorf("got %q", got)
	}
}
