package chat

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Parsa2820/council/internal/consensus"
	"github.com/Parsa2820/council/internal/harness"
)

var roundRE = regexp.MustCompile(`ROUND (\d+) of`)

func roundOf(prompt string) int {
	m := roundRE.FindStringSubmatch(prompt)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func reply(text, answer string, agree bool, conf float64) string {
	return fmt.Sprintf("%s\n\n```json\n{\"final_answer\": %q, \"agree\": %v, \"confidence\": %v}\n```", text, answer, agree, conf)
}

// fake builds an in-process agent whose behaviour depends on the round.
func fake(name string, fn func(round int, prompt string) (string, error)) harness.Harness {
	return &harness.Func{AgentName: name, Fn: func(ctx context.Context, p string) (string, error) {
		return fn(roundOf(p), p)
	}}
}

// stubborn always answers the same thing.
func stubborn(name, answer string) harness.Harness {
	return fake(name, func(r int, _ string) (string, error) {
		return reply("I am sure.", answer, r > 1, 0.9), nil
	})
}

// follower answers `initial` in round 1, then adopts `adopt` and agrees.
func follower(name, initial, adopt string) harness.Harness {
	return fake(name, func(r int, _ string) (string, error) {
		if r == 1 {
			return reply("My first guess.", initial, false, 0.4), nil
		}
		return reply("The others convinced me.", adopt, true, 0.8), nil
	})
}

func TestConvergesInRoundTwo(t *testing.T) {
	agents := []harness.Harness{stubborn("a", "42"), stubborn("b", "42"), follower("c", "41", "42")}
	var events []EventKind
	c, _ := New(agents, Options{Rounds: 3, RequireExplicitAgreement: true, OnEvent: func(e Event) { events = append(events, e.Kind) }})
	res, err := c.Run(context.Background(), "What is 6*7?")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Converged || res.Rounds != 2 || res.Answer != "42" || res.Method != consensus.MethodUnanimous {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(res.Supporters) != 3 {
		t.Fatalf("supporters: %v", res.Supporters)
	}
	// user + 3 agents * 2 rounds
	if len(res.Transcript) != 7 {
		t.Fatalf("transcript has %d messages", len(res.Transcript))
	}
	if events[len(events)-1] != EventConsensus {
		t.Fatalf("last event %v", events[len(events)-1])
	}
	if res.Details != "I am sure." && res.Details != "The others convinced me." {
		t.Fatalf("details not taken from a supporter: %q", res.Details)
	}
}

func TestRoundOneUnanimousStopsEarly(t *testing.T) {
	agents := []harness.Harness{stubborn("a", "Paris"), stubborn("b", "paris.")}
	c, _ := New(agents, Options{Rounds: 3, RequireExplicitAgreement: true})
	res, err := c.Run(context.Background(), "Capital of France?")
	if err != nil || !res.Converged || res.Rounds != 1 {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestLaterRoundsSeeEarlierMessages(t *testing.T) {
	var seen atomic.Value
	spy := fake("spy", func(r int, p string) (string, error) {
		if r == 2 {
			seen.Store(p)
		}
		return reply("spy says", "x", r > 1, 0.5), nil
	})
	other := fake("other", func(r int, _ string) (string, error) {
		if r == 1 {
			return reply("UNIQUE-OTHER-ROUND1-TEXT", "y", false, 0.5), nil
		}
		return reply("ok", "x", true, 0.5), nil
	})
	c, _ := New([]harness.Harness{spy, other}, Options{Rounds: 2, RequireExplicitAgreement: true})
	if _, err := c.Run(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	p, _ := seen.Load().(string)
	for _, want := range []string{"## Discussion so far", "### Round 1", "#### other", "UNIQUE-OTHER-ROUND1-TEXT", "#### spy", "FINAL round"} {
		if !strings.Contains(p, want) {
			t.Errorf("round-2 prompt missing %q:\n%s", want, p)
		}
	}
}

func TestNoConsensusFallsBackToMajority(t *testing.T) {
	agents := []harness.Harness{stubborn("a", "tabs"), stubborn("b", "spaces"), stubborn("c", "tabs")}
	var last EventKind
	c, _ := New(agents, Options{Rounds: 2, OnEvent: func(e Event) { last = e.Kind }})
	res, err := c.Run(context.Background(), "tabs or spaces?")
	if err != nil {
		t.Fatal(err)
	}
	if res.Converged || res.Rounds != 2 || res.Answer != "tabs" || res.Method != consensus.MethodMajority {
		t.Fatalf("got %+v", res)
	}
	if strings.Join(res.Dissenters, ",") != "b" || last != EventNoConsensus {
		t.Fatalf("dissenters %v last %v", res.Dissenters, last)
	}
}

func TestExplicitAgreementRequired(t *testing.T) {
	// Same answer but b refuses to say agree=true: no consensus when required.
	b := fake("b", func(r int, _ string) (string, error) { return reply("hmm", "42", false, 0.5), nil })
	a := fake("a", func(r int, _ string) (string, error) {
		if r == 1 {
			return reply("", "41", false, 0.5), nil
		}
		return reply("", "42", true, 0.5), nil
	})
	c, _ := New([]harness.Harness{a, b}, Options{Rounds: 2, RequireExplicitAgreement: true})
	res, _ := c.Run(context.Background(), "q")
	if res.Converged {
		t.Fatal("should not converge without explicit agreement")
	}
	c, _ = New([]harness.Harness{a, b}, Options{Rounds: 2, RequireExplicitAgreement: false})
	res, _ = c.Run(context.Background(), "q")
	if !res.Converged || res.Rounds != 2 {
		t.Fatalf("should converge on matching answers: %+v", res)
	}
}

func TestModeratorSynthesis(t *testing.T) {
	var modPrompt string
	mod := &harness.Func{AgentName: "mod", Fn: func(_ context.Context, p string) (string, error) {
		modPrompt = p
		return reply("Synthesis: both have merit, tabs win.", "tabs", true, 1), nil
	}}
	agents := []harness.Harness{stubborn("a", "tabs"), stubborn("b", "spaces")}
	c, _ := New(agents, Options{Rounds: 1, Moderator: mod})
	res, err := c.Run(context.Background(), "tabs or spaces?")
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != consensus.MethodModerator || res.Answer != "tabs" || res.Moderator != "mod" {
		t.Fatalf("got %+v", res)
	}
	if res.Details != "Synthesis: both have merit, tabs win." {
		t.Fatalf("details %q", res.Details)
	}
	if strings.Join(res.Supporters, ",") != "a" || strings.Join(res.Dissenters, ",") != "b" {
		t.Fatalf("supporters %v dissenters %v", res.Supporters, res.Dissenters)
	}
	if !strings.Contains(modPrompt, "MODERATOR") || !strings.Contains(modPrompt, `- b: "spaces"`) {
		t.Fatalf("moderator prompt lacks tally:\n%s", modPrompt)
	}
	last := res.Transcript[len(res.Transcript)-1]
	if last.Role != RoleModerator {
		t.Fatalf("moderator message not in transcript: %+v", last)
	}
}

func TestModeratorFailureFallsBack(t *testing.T) {
	mod := &harness.Func{AgentName: "mod", Fn: func(context.Context, string) (string, error) { return "", errors.New("not signed in") }}
	c, _ := New([]harness.Harness{stubborn("a", "x"), stubborn("b", "x")}, Options{Rounds: 1, Moderator: mod})
	res, err := c.Run(context.Background(), "q")
	if err != nil || res.Answer != "x" || res.Method != consensus.MethodUnanimous || len(res.Notes) != 1 {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestAgentFailureIsTolerated(t *testing.T) {
	broken := fake("broken", func(int, string) (string, error) { return "", errors.New("codex: not logged in") })
	c, _ := New([]harness.Harness{stubborn("a", "x"), stubborn("b", "x"), broken}, Options{Rounds: 2})
	res, err := c.Run(context.Background(), "q")
	if err != nil || !res.Converged || res.Answer != "x" {
		t.Fatalf("got %+v %v", res, err)
	}
	md := NewTranscriptFrom(res).Markdown(res)
	if !strings.Contains(md, "**error:** codex: not logged in") {
		t.Fatalf("markdown lacks error:\n%s", md)
	}
}

func TestAllAgentsFail(t *testing.T) {
	broken := fake("broken", func(int, string) (string, error) { return "", errors.New("boom") })
	c, _ := New([]harness.Harness{broken}, Options{Rounds: 2})
	if _, err := c.Run(context.Background(), "q"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want error, got %v", err)
	}
}

func TestMissingVoteIsAbstention(t *testing.T) {
	chatty := fake("chatty", func(int, string) (string, error) { return "I forgot the vote block", nil })
	c, _ := New([]harness.Harness{stubborn("a", "x"), stubborn("b", "x"), chatty}, Options{Rounds: 2})
	res, err := c.Run(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	// Everyone responding must vote for consensus, so no early stop; majority wins.
	if res.Converged || res.Answer != "x" || res.Rounds != 2 {
		t.Fatalf("got %+v", res)
	}
}

func TestAgentsRunConcurrently(t *testing.T) {
	const n = 4
	var started sync.WaitGroup
	started.Add(n)
	var agents []harness.Harness
	for i := 0; i < n; i++ {
		agents = append(agents, &harness.Func{AgentName: fmt.Sprintf("p%d", i), Fn: func(ctx context.Context, p string) (string, error) {
			if roundOf(p) == 1 {
				started.Done()
				done := make(chan struct{})
				go func() { started.Wait(); close(done) }()
				select { // only returns once all n agents are running at the same time
				case <-done:
				case <-time.After(5 * time.Second):
					return "", errors.New("agents did not run concurrently")
				}
			}
			return reply("", "same", true, 1), nil
		}})
	}
	c, _ := New(agents, Options{Rounds: 1})
	res, err := c.Run(context.Background(), "q")
	if err != nil || !res.Converged {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestTurnTimeout(t *testing.T) {
	slow := &harness.Func{AgentName: "slow", Fn: func(ctx context.Context, _ string) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(10 * time.Second):
			return reply("", "late", true, 1), nil
		}
	}}
	c, _ := New([]harness.Harness{stubborn("a", "x"), stubborn("b", "x"), slow}, Options{Rounds: 1, TurnTimeout: 100 * time.Millisecond})
	start := time.Now()
	res, err := c.Run(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("turn timeout not enforced")
	}
	if res.Transcript[3].Error == "" || !strings.Contains(res.Transcript[3].Error, "deadline") {
		t.Fatalf("slow agent should have timed out: %+v", res.Transcript[3])
	}
}

func TestValidation(t *testing.T) {
	if _, err := New(nil, Options{}); err == nil {
		t.Fatal("no agents should fail")
	}
	if _, err := New([]harness.Harness{stubborn("a", "x"), stubborn("a", "y")}, Options{}); err == nil {
		t.Fatal("duplicate names should fail")
	}
	c, _ := New([]harness.Harness{stubborn("a", "x")}, Options{})
	if _, err := c.Run(context.Background(), "   "); err == nil {
		t.Fatal("empty question should fail")
	}
}
