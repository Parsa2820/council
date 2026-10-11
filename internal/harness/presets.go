package harness

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Built-in harness kinds.
const (
	KindCodex       = "codex"
	KindClaude      = "claude"
	KindAntigravity = "antigravity"
	KindOpenCode    = "opencode"
	KindGeneric     = "generic"
)

var kindAliases = map[string]string{
	"agy":         KindAntigravity,
	"claude-code": KindClaude,
	"open-code":   KindOpenCode,
	"openai":      KindCodex,
	"custom":      KindGeneric,
}

// CanonicalKind resolves aliases such as "agy" -> "antigravity".
func CanonicalKind(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	if c, ok := kindAliases[k]; ok {
		return c
	}
	return k
}

// BuiltinKinds lists the kinds with presets, sorted.
func BuiltinKinds() []string {
	ks := []string{KindCodex, KindClaude, KindAntigravity, KindOpenCode}
	sort.Strings(ks)
	return ks
}

// Preset returns the built-in Spec for kind. ok is false for unknown kinds
// (including "generic", which has no defaults beyond what the user supplies).
//
// Flags are taken from each CLI's documentation (October 2026):
//
//   - Codex CLI: `codex exec` is the non-interactive mode. The prompt is read
//     from stdin when the PROMPT argument is "-"; --output-last-message (-o)
//     writes only the final agent message to a file; --skip-git-repo-check
//     allows running outside a git repo; --ephemeral avoids persisting session
//     files; --sandbox read-only keeps the agent from editing files.
//     Auth: `codex login`, status via `codex login status` (exit 0 = signed in).
//   - Claude Code: `claude -p` (print mode) reads the prompt from stdin;
//     --output-format json returns {"type":"result","is_error":bool,"result":"..."}.
//     --no-session-persistence keeps runs out of the resume list.
//     Auth: `claude auth login`, status via `claude auth status` (exit 0/1).
//   - Antigravity CLI (`agy`): `agy -p` is headless/print mode with
//     --output-format text|json|stream-json. We use the documented
//     `--input-format stream-json --output-format stream-json` session mode,
//     which reads {"event":"user","message":{"content":...}} lines on stdin
//     (avoiding OS argument-length limits on long transcripts) and ends each
//     turn with {"event":"result","result":{"status":..,"response":..}}.
//     Auth: interactive `agy` (Google sign-in stored in the OS keyring), or
//     GEMINI_API_KEY with "modelProvider": "gemini" in settings.json. There
//     is no auth or status subcommand, but `agy models` needs a session: it
//     exits 1 with "Please sign in to view available models" when signed out
//     (or with a GEMINI_API_KEY error in API-key mode) and lists the models
//     with exit 0 otherwise. It costs no tokens, so it is the status check.
//   - OpenCode (`opencode`, sst/opencode): `opencode run` is the
//     non-interactive mode. With no message arguments it reads the message
//     from stdin; --format json prints NDJSON events, the reply in
//     {"type":"text","part":{"text":...}} and failures as
//     {"type":"error","error":{"data":{"message":...}}}. --agent plan is the
//     built-in read-only agent (no file edits or shell commands). -m/--model
//     takes provider/model. Auth: `opencode auth login` stores provider keys
//     in ~/.local/share/opencode/auth.json; `opencode auth list` lists them
//     and provider env vars (it always exits 0, so its output is parsed).
//     With no credentials OpenCode still works with its free Zen models.
func Preset(kind string) (Spec, bool) {
	switch CanonicalKind(kind) {
	case KindCodex:
		return Spec{
			Kind:    KindCodex,
			Command: "codex",
			Args: []string{"exec", "--skip-git-repo-check", "--ephemeral",
				"--sandbox", "read-only", "--color", "never",
				"--output-last-message", "{{output_file}}",
				"{{model_args}}", "{{extra_args}}", "-"},
			ModelFlag:    "--model",
			PromptMode:   PromptStdin,
			Output:       OutputText,
			OutputSource: SourceFile,
			Timeout:      10 * time.Minute,
			LoginArgs:    []string{"login"},
			LoginNote:    "Opens the ChatGPT sign-in flow. Use `council login codex -- --device-auth` on a headless machine, or `-- --with-api-key` to pipe an API key.",
			StatusArgs:   []string{"login", "status"},
		}, true
	case KindClaude:
		return Spec{
			Kind:    KindClaude,
			Command: "claude",
			Args: []string{"-p", "--output-format", "json", "--no-session-persistence",
				"{{model_args}}", "{{extra_args}}"},
			ModelFlag:  "--model",
			PromptMode: PromptStdin,
			Output:     OutputJSON,
			JSONPath:   "result",
			ErrorPath:  "is_error",
			Timeout:    10 * time.Minute,
			LoginArgs:  []string{"auth", "login"},
			LoginNote:  "Signs in to your Anthropic account. Pass `-- --console` for API billing or `-- --sso` for SSO.",
			StatusArgs: []string{"auth", "status"},
		}, true
	case KindAntigravity:
		return Spec{
			Kind:          KindAntigravity,
			Command:       "agy",
			Args:          []string{"--input-format", "stream-json", "--output-format", "stream-json", "{{model_args}}", "{{extra_args}}"},
			ModelFlag:     "--model",
			PromptMode:    PromptStdin,
			StdinTemplate: `{"event":"user","message":{"content":{{prompt_json}}}}` + "\n",
			Output:        OutputJSONL,
			JSONPath:      "result.response",
			ErrorPath:     "result.error",
			Timeout:       10 * time.Minute,
			LoginArgs:     []string{},
			LoginNote:     "Starts Antigravity CLI interactively: sign in with Google (over SSH it prints a URL), then type /quit. The session is kept in the OS keyring. For CI, export GEMINI_API_KEY and set \"modelProvider\": \"gemini\" in ~/.gemini/antigravity-cli/settings.json.",
			StatusArgs:    []string{"models"},
			statusParse:   antigravityStatus,
		}, true
	case KindOpenCode:
		return Spec{
			Kind:        KindOpenCode,
			Command:     "opencode",
			Args:        []string{"run", "--format", "json", "--agent", "plan", "{{model_args}}", "{{extra_args}}"},
			ModelFlag:   "--model",
			PromptMode:  PromptStdin,
			Output:      OutputJSONL,
			JSONPath:    "part.text",
			ErrorPath:   "error.data.message",
			Timeout:     10 * time.Minute,
			LoginArgs:   []string{"auth", "login"},
			LoginNote:   "Adds a provider API key or sign-in, stored in ~/.local/share/opencode/auth.json. Pass `-- --provider anthropic` to skip the provider list. Without any provider OpenCode uses its free OpenCode Zen models.",
			StatusArgs:  []string{"auth", "list"},
			statusParse: openCodeStatus,
		}, true
	}
	return Spec{}, false
}

const geminiRemoved = "the gemini harness was removed in council v0.2.1 because Gemini CLI is deprecated; use `harness: antigravity` (Google's Antigravity CLI, agy) or `harness: generic` instead"

// Removed reports whether kind names a built-in harness that no longer
// exists, and what to use instead.
func Removed(kind string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "gemini", "gemini-cli":
		return geminiRemoved, true
	}
	return "", false
}

// antigravityStatus interprets `agy models`: exit 0 lists the models
// available to the signed-in account (or API key), one "slug<TAB>name" per
// line; otherwise the last line says why, e.g. "Error: Please sign in to
// view available models. Launch the CLI without arguments to sign in."
func antigravityStatus(out string, err error) (AuthState, string) {
	models, last := 0, ""
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case l == "" || strings.HasPrefix(l, "Fetching "):
		case strings.Contains(l, "\t"):
			models++
		default:
			last = l
		}
	}
	if err != nil {
		if last == "" {
			last = err.Error()
		}
		return AuthMissing, strings.TrimPrefix(last, "Error: ")
	}
	if models == 0 {
		return AuthOK, "signed in"
	}
	return AuthOK, fmt.Sprintf("signed in (%d models available)", models)
}

var (
	ansiRE  = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	countRE = regexp.MustCompile(`(\d+) (credentials?|environment variables?)\b`)
)

// openCodeStatus interprets `opencode auth list`, which exits 0 whether or
// not anything is configured and ends its sections with "N credentials" and
// "N environment variable(s)". OpenCode works without any provider (its free
// OpenCode Zen models), so having none is reported as usable, with a hint.
func openCodeStatus(out string, err error) (AuthState, string) {
	out = ansiRE.ReplaceAllString(out, "")
	if err != nil {
		if l := firstLine(out); l != "" {
			return AuthMissing, l
		}
		return AuthMissing, err.Error()
	}
	creds, envs := 0, 0
	for _, m := range countRE.FindAllStringSubmatch(out, -1) {
		n, _ := strconv.Atoi(m[1])
		if strings.HasPrefix(m[2], "credential") {
			creds += n
		} else {
			envs += n
		}
	}
	if creds+envs == 0 {
		return AuthOK, "no provider configured: only OpenCode's free models (run `council login opencode` to add one)"
	}
	return AuthOK, fmt.Sprintf("%d stored credential(s), %d provider environment variable(s)", creds, envs)
}
