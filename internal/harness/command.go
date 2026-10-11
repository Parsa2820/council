package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CommandHarness runs a CLI as a subprocess according to a Spec.
type CommandHarness struct {
	name string
	spec Spec
}

// New returns a CommandHarness for the agent name and spec. The spec is
// validated and defaults are filled in.
func New(name string, spec Spec) (*CommandHarness, error) {
	if spec.Command == "" {
		return nil, fmt.Errorf("agent %q: harness command is empty", name)
	}
	if spec.Kind == "" {
		spec.Kind = KindGeneric
	}
	if spec.PromptMode == "" {
		spec.PromptMode = PromptArg
	}
	if spec.Output == "" {
		spec.Output = OutputText
	}
	if spec.OutputSource == "" {
		spec.OutputSource = SourceStdout
	}
	switch spec.PromptMode {
	case PromptArg, PromptStdin, PromptFile:
	default:
		return nil, fmt.Errorf("agent %q: unknown prompt_mode %q (want arg, stdin or file)", name, spec.PromptMode)
	}
	switch spec.Output {
	case OutputText, OutputJSON, OutputJSONL:
	default:
		return nil, fmt.Errorf("agent %q: unknown output %q (want text, json or jsonl)", name, spec.Output)
	}
	switch spec.OutputSource {
	case SourceStdout:
	case SourceFile:
		if !argsContain(spec.Args, "{{output_file}}") {
			return nil, fmt.Errorf("agent %q: output_source=file requires {{output_file}} in args", name)
		}
	default:
		return nil, fmt.Errorf("agent %q: unknown output_source %q (want stdout or file)", name, spec.OutputSource)
	}
	return &CommandHarness{name: name, spec: spec}, nil
}

// Name returns the agent name this harness was created for.
func (h *CommandHarness) Name() string { return h.name }

// Kind returns the harness kind (codex, claude, gemini, antigravity or generic).
func (h *CommandHarness) Kind() string { return h.spec.Kind }

// Spec returns the resolved spec, with defaults filled in by New.
func (h *CommandHarness) Spec() Spec { return h.spec }

// LoginCommand returns the argv for the harness's own login flow, or nil.
func (h *CommandHarness) LoginCommand() ([]string, string) {
	if h.spec.LoginArgs == nil {
		return nil, ""
	}
	return append([]string{h.spec.Command}, h.spec.LoginArgs...), h.spec.LoginNote
}

func argsContain(args []string, token string) bool {
	for _, a := range args {
		if strings.Contains(a, token) {
			return true
		}
	}
	return false
}

// BuildArgs expands the argument template. Exported for tests and --dry-run.
func (h *CommandHarness) BuildArgs(prompt, promptFile, outputFile string) []string {
	s := h.spec
	repl := strings.NewReplacer(
		"{{prompt_file}}", promptFile,
		"{{output_file}}", outputFile,
		"{{model}}", s.Model,
		"{{workdir}}", s.Dir,
		"{{prompt}}", prompt,
	)
	var out []string
	for _, a := range s.Args {
		switch strings.TrimSpace(a) {
		case "{{model_args}}":
			if s.Model != "" && s.ModelFlag != "" {
				out = append(out, s.ModelFlag, s.Model)
			}
			continue
		case "{{extra_args}}":
			out = append(out, s.ExtraArgs...)
			continue
		}
		out = append(out, repl.Replace(a))
	}
	if !argsContain(s.Args, "{{extra_args}}") {
		out = append(out, s.ExtraArgs...)
	}
	switch s.PromptMode {
	case PromptArg:
		if !argsContain(s.Args, "{{prompt}}") {
			out = append(out, prompt)
		}
	case PromptFile:
		if !argsContain(s.Args, "{{prompt_file}}") {
			out = append(out, promptFile)
		}
	}
	return out
}

func (h *CommandHarness) stdinPayload(prompt string) string {
	if h.spec.StdinTemplate == "" {
		return prompt
	}
	pj, _ := json.Marshal(prompt)
	return strings.NewReplacer("{{prompt_json}}", string(pj), "{{prompt}}", prompt).Replace(h.spec.StdinTemplate)
}

// Run executes the harness once.
func (h *CommandHarness) Run(ctx context.Context, prompt string) (string, error) {
	if h.spec.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.spec.Timeout)
		defer cancel()
	}
	tmp, err := os.MkdirTemp("", "council-"+sanitize(h.name)+"-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	promptFile := filepath.Join(tmp, "prompt.md")
	outputFile := filepath.Join(tmp, "output.txt")
	if h.spec.PromptMode == PromptFile {
		if err := os.WriteFile(promptFile, []byte(prompt), 0o600); err != nil {
			return "", err
		}
	}

	cmd := exec.CommandContext(ctx, h.spec.Command, h.BuildArgs(prompt, promptFile, outputFile)...)
	cmd.Dir = h.spec.Dir
	cmd.Env = os.Environ()
	for k, v := range h.spec.Env {
		cmd.Env = append(cmd.Env, k+"="+os.ExpandEnv(v))
	}
	if h.spec.PromptMode == PromptStdin {
		cmd.Stdin = strings.NewReader(h.stdinPayload(prompt))
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	configureProcess(cmd)
	cmd.WaitDelay = 3 * time.Second

	runErr := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return "", fmt.Errorf("%s: timed out: %w", h.name, ctxErr)
		}
		return "", fmt.Errorf("%s: %w", h.name, ctxErr)
	}

	raw := stdout.Bytes()
	if h.spec.OutputSource == SourceFile {
		if b, err := os.ReadFile(outputFile); err == nil {
			raw = b
		} else if runErr == nil {
			return "", fmt.Errorf("%s: expected output file was not written: %w", h.name, err)
		}
	}

	answer, parseErr := h.parse(raw)
	if runErr != nil {
		msg := tail(strings.TrimSpace(stderr.String()), 800)
		if parseErr != nil && !errors.Is(parseErr, errNoAnswer) {
			msg = parseErr.Error() + "; " + msg
		}
		if msg == "" {
			msg = tail(strings.TrimSpace(stdout.String()), 800)
		}
		return "", fmt.Errorf("%s: %s exited: %v: %s", h.name, h.spec.Command, runErr, msg)
	}
	if parseErr != nil {
		return "", fmt.Errorf("%s: %w", h.name, parseErr)
	}
	return answer, nil
}

var errNoAnswer = errors.New("no answer in output")

func (h *CommandHarness) parse(raw []byte) (string, error) {
	switch h.spec.Output {
	case OutputText:
		s := strings.TrimSpace(string(raw))
		if s == "" {
			return "", errNoAnswer
		}
		return s, nil
	case OutputJSON:
		var doc any
		if err := json.Unmarshal(bytes.TrimSpace(raw), &doc); err != nil {
			return "", fmt.Errorf("output is not valid JSON: %v (got %q)", err, tail(string(raw), 200))
		}
		return h.extract(doc)
	case OutputJSONL:
		lines := bytes.Split(raw, []byte("\n"))
		var lastErr error = errNoAnswer
		for i := len(lines) - 1; i >= 0; i-- {
			line := bytes.TrimSpace(lines[i])
			if len(line) == 0 || line[0] != '{' {
				continue
			}
			var doc any
			if json.Unmarshal(line, &doc) != nil {
				continue
			}
			if h.spec.ErrorPath != "" {
				if v, ok := Lookup(doc, h.spec.ErrorPath); ok && truthy(v) {
					return "", fmt.Errorf("harness reported error: %s", stringify(v))
				}
			}
			if _, ok := Lookup(doc, h.spec.JSONPath); ok {
				return h.extract(doc)
			}
		}
		return "", lastErr
	}
	return "", fmt.Errorf("unsupported output format %q", h.spec.Output)
}

func (h *CommandHarness) extract(doc any) (string, error) {
	if h.spec.ErrorPath != "" {
		if v, ok := Lookup(doc, h.spec.ErrorPath); ok && truthy(v) {
			detail := stringify(v)
			if b, isBool := v.(bool); isBool && b {
				if ans, ok := Lookup(doc, h.spec.JSONPath); ok {
					detail = stringify(ans)
				}
			}
			return "", fmt.Errorf("harness reported error: %s", detail)
		}
	}
	v, ok := Lookup(doc, h.spec.JSONPath)
	if !ok {
		return "", fmt.Errorf("%w: JSON path %q not found", errNoAnswer, h.spec.JSONPath)
	}
	s := strings.TrimSpace(stringify(v))
	if s == "" {
		return "", errNoAnswer
	}
	return s, nil
}

func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	case map[string]any:
		return len(t) > 0
	case []any:
		return len(t) > 0
	}
	return true
}

func stringify(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, s)
}
