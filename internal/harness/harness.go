// Package harness adapts LLM coding CLIs ("harnesses") such as Codex CLI,
// Claude Code, Gemini CLI and Antigravity CLI to a single interface: send a
// prompt non-interactively, get the agent's final text back.
//
// Every adapter, built-in or user-defined, is a CommandHarness configured by a
// Spec. Built-in harnesses are just Spec presets (see presets.go) that encode
// each CLI's documented headless flags.
package harness

import (
	"context"
	"time"
)

// Harness is anything that can answer a prompt.
type Harness interface {
	// Name is the agent name used in the chat (e.g. "claude" or "claude-opus").
	Name() string
	// Kind is the harness type (codex, claude, gemini, antigravity, generic, ...).
	Kind() string
	// Run sends prompt to the harness and returns its final answer text.
	Run(ctx context.Context, prompt string) (string, error)
}

// Checker is implemented by harnesses that can report install/auth status.
type Checker interface {
	Check(ctx context.Context) Status
}

// AuthState describes whether a harness appears signed in.
type AuthState string

const (
	AuthOK      AuthState = "ok"
	AuthMissing AuthState = "missing"
	AuthUnknown AuthState = "unknown"
)

// Status is the result of a doctor check.
type Status struct {
	Agent     string    `json:"agent"`
	Kind      string    `json:"kind"`
	Command   string    `json:"command"`
	Installed bool      `json:"installed"`
	Path      string    `json:"path,omitempty"`
	Version   string    `json:"version,omitempty"`
	Auth      AuthState `json:"auth"`
	Detail    string    `json:"detail,omitempty"`
}

// PromptMode controls how the prompt reaches the child process.
type PromptMode string

const (
	PromptArg   PromptMode = "arg"   // substituted into {{prompt}} or appended as last argument
	PromptStdin PromptMode = "stdin" // written to stdin (optionally through StdinTemplate)
	PromptFile  PromptMode = "file"  // written to a temp file; path substituted into {{prompt_file}}
)

// OutputFormat controls how stdout (or the output file) is parsed.
type OutputFormat string

const (
	OutputText  OutputFormat = "text"  // raw text
	OutputJSON  OutputFormat = "json"  // single JSON document; JSONPath selects the answer
	OutputJSONL OutputFormat = "jsonl" // NDJSON stream; last line containing JSONPath wins
)

// OutputSource says where the answer is read from.
type OutputSource string

const (
	SourceStdout OutputSource = "stdout"
	SourceFile   OutputSource = "file" // a temp file whose path is substituted into {{output_file}}
)

// Spec fully describes how to drive a CLI non-interactively.
type Spec struct {
	Kind    string
	Command string
	// Args is the argument template. Placeholders:
	//   {{prompt}} {{prompt_file}} {{output_file}} {{model}} {{workdir}}  (substring substitution)
	//   {{model_args}} {{extra_args}}  (whole-argument; expand to zero or more args)
	Args      []string
	ExtraArgs []string
	Model     string
	ModelFlag string // e.g. "--model"; used by {{model_args}}

	PromptMode PromptMode
	// StdinTemplate, if set, is written to stdin instead of the raw prompt.
	// {{prompt}} and {{prompt_json}} (a JSON string literal) are substituted.
	StdinTemplate string

	Output       OutputFormat
	OutputSource OutputSource
	JSONPath     string // dot path, e.g. "result" or "result.response" or "choices.0.text"
	ErrorPath    string // dot path; a truthy value marks the run as failed

	Timeout time.Duration
	Env     map[string]string
	Dir     string

	// LoginArgs are appended to Command for `council login`. A nil slice means
	// "no login command"; an empty non-nil slice means "run Command bare".
	LoginArgs []string
	LoginNote string
	// StatusArgs are appended to Command; exit code 0 means signed in.
	StatusArgs []string
	// statusFunc is a built-in auth probe used when StatusArgs is empty.
	statusFunc func(ctx context.Context) (AuthState, string)
}
