package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Parsa2820/council/internal/chat"
	"github.com/Parsa2820/council/internal/config"
	"github.com/Parsa2820/council/internal/consensus"
	"github.com/Parsa2820/council/internal/harness"
)

// Limits bound what a run request may ask for.
type Limits struct {
	MaxPromptBytes int           `json:"max_prompt_bytes"`
	MaxAgents      int           `json:"max_agents"`
	MaxRounds      int           `json:"max_rounds"`
	MaxTimeout     time.Duration `json:"max_timeout_ns"`
}

var limits = Limits{MaxPromptBytes: 200_000, MaxAgents: 12, MaxRounds: 10, MaxTimeout: time.Hour}

// RunRequest is the JSON body of POST /api/run. Zero values mean "use the
// config default".
type RunRequest struct {
	Prompt    string   `json:"prompt"`
	Agents    []string `json:"agents"`
	Rounds    int      `json:"rounds"`
	Moderator string   `json:"moderator"`
	// Timeout is a per-turn limit such as "90s" or "5m"; "" uses each
	// agent's own timeout.
	Timeout string `json:"timeout"`
	// Loose accepts identical answers as consensus without agree=true.
	Loose bool `json:"loose"`
}

// build validates req against cfg and returns the agents and chat options.
func (req *RunRequest) build(cfg *config.Config) ([]harness.Harness, chat.Options, error) {
	var opts chat.Options
	req.Prompt = strings.TrimSpace(req.Prompt)
	switch {
	case req.Prompt == "":
		return nil, opts, errors.New("prompt is empty")
	case len(req.Prompt) > limits.MaxPromptBytes:
		return nil, opts, fmt.Errorf("prompt is %d bytes; the limit is %d", len(req.Prompt), limits.MaxPromptBytes)
	case req.Rounds < 0 || req.Rounds > limits.MaxRounds:
		return nil, opts, fmt.Errorf("rounds must be between 1 and %d", limits.MaxRounds)
	}
	names := req.Agents
	if len(names) == 0 {
		names = cfg.Defaults.Agents
	}
	if len(names) > limits.MaxAgents {
		return nil, opts, fmt.Errorf("at most %d agents per run", limits.MaxAgents)
	}
	var agents []harness.Harness
	for _, n := range names {
		h, err := cfg.Harness(strings.TrimSpace(n))
		if err != nil {
			return nil, opts, err
		}
		agents = append(agents, h)
	}
	opts = chat.Options{
		Rounds:                   cfg.Defaults.Rounds,
		RequireExplicitAgreement: *cfg.Defaults.RequireExplicitAgreement && !req.Loose,
		MaxTranscriptChars:       cfg.Defaults.MaxTranscriptChars,
	}
	if req.Rounds > 0 {
		opts.Rounds = req.Rounds
	}
	if req.Timeout != "" && req.Timeout != "0" {
		d, err := time.ParseDuration(req.Timeout)
		if err != nil || d < 0 || d > limits.MaxTimeout {
			return nil, opts, fmt.Errorf("timeout %q must be a duration such as 90s or 5m, at most %v", req.Timeout, limits.MaxTimeout)
		}
		opts.TurnTimeout = d
	}
	mod := req.Moderator
	if mod == "" {
		mod = cfg.Defaults.Moderator
	}
	if mod != config.ModeratorMajority && mod != "vote" && mod != "none" {
		h, err := cfg.Harness(mod)
		if err != nil {
			return nil, opts, fmt.Errorf("moderator: %w", err)
		}
		opts.Moderator = h
	}
	return agents, opts, nil
}

// VoteJSON is an agent's vote as sent to the browser.
type VoteJSON struct {
	Answer     string  `json:"answer"`
	Agree      bool    `json:"agree"`
	Confidence float64 `json:"confidence"`
}

// MessageJSON is one chat message as sent to the browser. Text is the reply
// without its vote block.
type MessageJSON struct {
	Round      int       `json:"round"`
	Agent      string    `json:"agent"`
	Role       string    `json:"role"`
	Text       string    `json:"text,omitempty"`
	Vote       *VoteJSON `json:"vote,omitempty"`
	Error      string    `json:"error,omitempty"`
	DurationMS int64     `json:"duration_ms"`
}

// ResultJSON is the final outcome sent in the "result" event.
type ResultJSON struct {
	Answer     string   `json:"answer"`
	Details    string   `json:"details,omitempty"`
	Method     string   `json:"method"`
	Converged  bool     `json:"converged"`
	Rounds     int      `json:"rounds"`
	Supporters []string `json:"supporters"`
	Dissenters []string `json:"dissenters"`
	Moderator  string   `json:"moderator,omitempty"`
	Notes      []string `json:"notes,omitempty"`
}

type sse struct {
	name string
	data any
}

func messageJSON(m *chat.Message) MessageJSON {
	out := MessageJSON{Round: m.Round, Agent: m.Agent, Role: string(m.Role), Error: m.Error, DurationMS: m.Duration.Milliseconds()}
	if m.OK() {
		out.Text = consensus.StripVote(m.Content)
	}
	if m.Vote != nil {
		out.Vote = &VoteJSON{Answer: m.Vote.FinalAnswer, Agree: m.Vote.Agree, Confidence: m.Vote.Confidence}
	}
	return out
}

func eventJSON(e chat.Event) sse {
	switch e.Kind {
	case chat.EventAgentMessage, chat.EventModerator:
		return sse{"message", messageJSON(e.Message)}
	case chat.EventConsensus:
		return sse{"consensus", map[string]any{"round": e.Round, "answer": e.Text}}
	default: // round_start, no_consensus, moderator_start
		return sse{string(e.Kind), map[string]any{"round": e.Round}}
	}
}

func resultJSON(r *chat.Result) ResultJSON {
	out := ResultJSON{Answer: r.Answer, Details: r.Details, Method: string(r.Method), Converged: r.Converged, Rounds: r.Rounds,
		Supporters: r.Supporters, Dissenters: r.Dissenters, Moderator: r.Moderator, Notes: r.Notes}
	if out.Supporters == nil {
		out.Supporters = []string{}
	}
	if out.Dissenters == nil {
		out.Dissenters = []string{}
	}
	return out
}

var newRunID = func() string { t, _ := newToken(); return t[:12] }

// handleRun validates the request, then runs the council and streams its
// events as Server-Sent Events: start, round_start, message, consensus,
// no_consensus, moderator_start, then exactly one of result, error or
// cancelled, each followed by a "transcript" event carrying the Markdown
// transcript. Closing the connection cancels the run.
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, int64(limits.MaxPromptBytes)+64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: %v", err)
		return
	}
	cfg, err := s.opts.LoadConfig()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "loading config: %v", err)
		return
	}
	agents, opts, err := req.build(cfg)
	if err != nil {
		httpError(w, http.StatusBadRequest, "%v", err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpError(w, http.StatusInternalServerError, "streaming is not supported by this connection")
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		httpError(w, http.StatusTooManyRequests, "%d runs are already in progress; wait for one to finish or cancel it", cap(s.slots))
		return
	}

	ctx, cancel := context.WithCancel(r.Context()) // a closed connection cancels the run
	defer cancel()
	events := make(chan sse, 64)
	opts.OnEvent = func(e chat.Event) {
		select {
		case events <- eventJSON(e):
		case <-ctx.Done():
		}
	}
	council, err := chat.New(agents, opts)
	if err != nil {
		httpError(w, http.StatusBadRequest, "%v", err)
		return
	}
	id := newRunID()
	s.mu.Lock()
	s.runs[id] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.runs, id)
		s.mu.Unlock()
	}()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name()
	}
	send := func(ev sse) {
		b, _ := json.Marshal(ev.data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.name, b)
		flusher.Flush()
	}
	send(sse{"start", map[string]any{"id": id, "agents": names, "rounds": opts.Rounds, "moderator": moderatorName(opts)}})

	type outcome struct {
		res *chat.Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := council.Run(ctx, req.Prompt)
		done <- outcome{res, err}
	}()
	tick := time.NewTicker(s.opts.Heartbeat)
	defer tick.Stop()
	for {
		select {
		case ev := <-events:
			send(ev)
		case <-tick.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case o := <-done:
			for len(events) > 0 { // flush events emitted just before Run returned
				send(<-events)
			}
			switch {
			case o.err == nil:
				send(sse{"result", resultJSON(o.res)})
			case ctx.Err() != nil:
				send(sse{"cancelled", map[string]string{"error": o.err.Error()}})
			default:
				send(sse{"error", map[string]string{"error": o.err.Error()}})
			}
			if o.res != nil {
				var final *chat.Result
				if o.err == nil {
					final = o.res
				}
				send(sse{"transcript", map[string]string{"markdown": chat.NewTranscriptFrom(o.res).Markdown(final)}})
			}
			return
		}
	}
}

func moderatorName(o chat.Options) string {
	if o.Moderator == nil {
		return ""
	}
	return o.Moderator.Name()
}
