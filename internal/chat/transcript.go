package chat

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Parsa2820/council/internal/consensus"
)

// Role of a transcript message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAgent     Role = "agent"
	RoleModerator Role = "moderator"
)

// Message is one entry in the shared transcript.
type Message struct {
	Round    int             `json:"round"`
	Agent    string          `json:"agent"`
	Role     Role            `json:"role"`
	Content  string          `json:"content,omitempty"`
	Vote     *consensus.Vote `json:"vote,omitempty"`
	Error    string          `json:"error,omitempty"`
	Duration time.Duration   `json:"duration_ns"`
	Time     time.Time       `json:"time"`
}

// OK reports whether the agent produced a reply.
func (m Message) OK() bool { return m.Error == "" }

// Transcript is the shared, append-only group chat log. Safe for concurrent use.
type Transcript struct {
	mu       sync.Mutex
	question string
	msgs     []Message
}

// NewTranscript starts a transcript with the user's question.
func NewTranscript(question string) *Transcript {
	t := &Transcript{question: question}
	t.msgs = append(t.msgs, Message{Round: 0, Agent: "user", Role: RoleUser, Content: question, Time: time.Now()})
	return t
}

// Question returns the original user prompt.
func (t *Transcript) Question() string { return t.question }

// Add appends a message.
func (t *Transcript) Add(m Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.msgs = append(t.msgs, m)
}

// Messages returns a copy of all messages.
func (t *Transcript) Messages() []Message {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Message(nil), t.msgs...)
}

// Discussion renders agent messages (rounds 1..upTo) for inclusion in a
// prompt. maxChars > 0 truncates each message body.
func (t *Transcript) Discussion(upTo, maxChars int) string {
	var sb strings.Builder
	cur := 0
	for _, m := range t.Messages() {
		if m.Role != RoleAgent || m.Round > upTo {
			continue
		}
		if m.Round != cur {
			cur = m.Round
			fmt.Fprintf(&sb, "\n### Round %d\n", cur)
		}
		fmt.Fprintf(&sb, "\n#### %s\n", m.Agent)
		if !m.OK() {
			fmt.Fprintf(&sb, "(no response this round: %s)\n", oneLine(m.Error, 200))
			continue
		}
		body := m.Content
		if maxChars > 0 && len(body) > maxChars {
			body = body[:maxChars] + "\n…[truncated]"
		}
		sb.WriteString(strings.TrimSpace(body))
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

// Markdown renders the full transcript, optionally with the final result.
func (t *Transcript) Markdown(res *Result) string {
	var sb strings.Builder
	sb.WriteString("# Council transcript\n\n")
	fmt.Fprintf(&sb, "## Question\n\n%s\n", strings.TrimSpace(t.question))
	cur := -1
	for _, m := range t.Messages() {
		switch m.Role {
		case RoleUser:
			continue
		case RoleModerator:
			fmt.Fprintf(&sb, "\n## Moderator: %s\n\n", m.Agent)
		default:
			if m.Round != cur {
				cur = m.Round
				fmt.Fprintf(&sb, "\n## Round %d\n", cur)
			}
			fmt.Fprintf(&sb, "\n### %s", m.Agent)
			if m.Duration > 0 {
				fmt.Fprintf(&sb, " _(%s)_", m.Duration.Round(time.Millisecond))
			}
			sb.WriteString("\n\n")
		}
		if !m.OK() {
			fmt.Fprintf(&sb, "> **error:** %s\n", oneLine(m.Error, 500))
			continue
		}
		sb.WriteString(strings.TrimSpace(m.Content))
		sb.WriteString("\n")
	}
	if res != nil {
		sb.WriteString("\n## Result\n\n")
		fmt.Fprintf(&sb, "- **Answer:** %s\n", res.Answer)
		fmt.Fprintf(&sb, "- **Method:** %s\n", res.Method)
		fmt.Fprintf(&sb, "- **Converged:** %v after %d round(s)\n", res.Converged, res.Rounds)
		if len(res.Supporters) > 0 {
			fmt.Fprintf(&sb, "- **Supporters:** %s\n", strings.Join(res.Supporters, ", "))
		}
		if len(res.Dissenters) > 0 {
			fmt.Fprintf(&sb, "- **Dissenters:** %s\n", strings.Join(res.Dissenters, ", "))
		}
	}
	return sb.String()
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}

// NewTranscriptFrom rebuilds a Transcript from a Result (e.g. for rendering).
func NewTranscriptFrom(res *Result) *Transcript {
	t := &Transcript{question: res.Question}
	t.msgs = append(t.msgs, res.Transcript...)
	return t
}
