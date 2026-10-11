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
	KindCursor      = "cursor"
	KindPi          = "pi"
	KindGeneric     = "generic"
)

var kindAliases = map[string]string{
	"agy":             KindAntigravity,
	"claude-code":     KindClaude,
	"cursor-agent":    KindCursor,
	"cursor-cli":      KindCursor,
	"pi-coding-agent": KindPi,
	"open-code":       KindOpenCode,
	"openai":          KindCodex,
	"custom":          KindGeneric,
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
	ks := []string{KindCodex, KindClaude, KindAntigravity, KindOpenCode, KindCursor, KindPi}
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
//   - Cursor CLI (`cursor-agent`, also installed as `agent`): -p/--print is
//     the non-interactive mode; with no prompt argument and piped stdin it
//     reads the whole of stdin as the prompt. --output-format json prints one
//     {"type":"result","subtype":"success","is_error":false,"result":"..."}
//     object on success; failures exit non-zero with a message and no JSON.
//     --mode ask is the read-only Q&A mode (no edits; without --force print
//     mode only proposes changes anyway). Headless runs refuse an untrusted
//     workspace unless --trust is given. --model selects the model. Auth:
//     `cursor-agent login` (browser; NO_OPEN_BROWSER=1 prints the URL) or
//     CURSOR_API_KEY. `cursor-agent status` exits 0 even when signed out and
//     ignores CURSOR_API_KEY, so status uses `cursor-agent models`, which
//     needs a working session or key: it exits 1 with "Authentication
//     required" when signed out or "The provided API key is invalid".
//   - Pi (`pi`, Mario Zechner's coding agent, npm
//     @earendil-works/pi-coding-agent, formerly @mariozechner/...):
//     -p/--print runs once and prints the final assistant text; piped stdin
//     becomes the prompt, and a failed response exits non-zero with the error
//     on stderr. (--mode json does not exit non-zero on a failed response,
//     so text print mode is used.) --tools read,grep,find,ls is the
//     documented read-only tool set and --no-mcp drops MCP tools;
//     --no-session keeps runs out of the session list. --model takes a
//     pattern or provider/id. Auth: /login inside interactive pi (OAuth or
//     API key, saved in ~/.pi/agent/auth.json) or provider env vars such as
//     ANTHROPIC_API_KEY. `pi --list-models` exits 0 either way, printing a
//     table of usable models or "No models available. Use /login ...".
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
	case KindCursor:
		return Spec{
			Kind:        KindCursor,
			Command:     "cursor-agent",
			Args:        []string{"-p", "--output-format", "json", "--mode", "ask", "--trust", "{{model_args}}", "{{extra_args}}"},
			ModelFlag:   "--model",
			PromptMode:  PromptStdin,
			Output:      OutputJSON,
			JSONPath:    "result",
			ErrorPath:   "is_error",
			Timeout:     10 * time.Minute,
			LoginArgs:   []string{"login"},
			LoginNote:   "Opens the Cursor sign-in in your browser. On a headless machine run `NO_OPEN_BROWSER=1 council login cursor` and open the printed URL. For CI, set CURSOR_API_KEY instead.",
			StatusArgs:  []string{"models"},
			statusParse: cursorStatus,
		}, true
	case KindPi:
		return Spec{
			Kind:        KindPi,
			Command:     "pi",
			Args:        []string{"-p", "--no-session", "--tools", "read,grep,find,ls", "--no-mcp", "{{model_args}}", "{{extra_args}}"},
			ModelFlag:   "--model",
			PromptMode:  PromptStdin,
			Output:      OutputText,
			Timeout:     10 * time.Minute,
			LoginArgs:   []string{},
			LoginNote:   "Starts pi interactively: type /login, pick a provider (subscription sign-in or API key, saved in ~/.pi/agent/auth.json), then /quit. Provider environment variables such as ANTHROPIC_API_KEY or OPENAI_API_KEY work without logging in.",
			StatusArgs:  []string{"--list-models"},
			statusParse: piStatus,
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

// cursorStatus interprets `cursor-agent models`, which needs a stored login
// or a valid CURSOR_API_KEY. Signed out it exits 1 with "Error:
// Authentication required. Run 'agent login', ..."; with a bad key it
// prints "⚠ Warning: The provided API key is invalid." Anything else that
// fails (no network, say) is reported as unknown with the CLI's message.
func cursorStatus(out string, err error) (AuthState, string) {
	out = ansiRE.ReplaceAllString(out, "")
	if err == nil {
		return AuthOK, "signed in (`cursor-agent models` succeeded)"
	}
	msg := firstLine(out)
	msg = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(msg, "⚠"), " "))
	msg = strings.TrimPrefix(strings.TrimPrefix(msg, "Warning: "), "Error: ")
	if msg == "" {
		return AuthUnknown, err.Error()
	}
	low := strings.ToLower(msg)
	if strings.Contains(low, "authentication required") || strings.Contains(low, "api key is invalid") ||
		strings.Contains(low, "not logged in") || strings.Contains(low, "log in again") {
		return AuthMissing, msg
	}
	return AuthUnknown, msg
}

// piStatus interprets `pi --list-models`, which exits 0 whether or not a
// provider is set up. With none it prints "No models available. Use /login
// to log into a provider via OAuth or API key. ..."; otherwise a table with
// a "provider model context ..." header and one model per line. Pi checks
// only that a credential is present, not that it works.
func piStatus(out string, err error) (AuthState, string) {
	out = ansiRE.ReplaceAllString(out, "")
	if err != nil {
		if l := firstLine(out); l != "" {
			return AuthUnknown, l
		}
		return AuthUnknown, err.Error()
	}
	if strings.Contains(out, "No models available") {
		return AuthMissing, "no provider configured (run `council login pi` and use /login, or set a provider API key such as ANTHROPIC_API_KEY)"
	}
	models, providers, table := 0, map[string]bool{}, false
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		switch {
		case len(f) >= 2 && f[0] == "provider" && f[1] == "model":
			table = true
		case table && len(f) >= 2:
			models++
			providers[f[0]] = true
		}
	}
	if models == 0 {
		return AuthUnknown, firstLine(out)
	}
	names := make([]string, 0, len(providers))
	for p := range providers {
		names = append(names, p)
	}
	sort.Strings(names)
	noun := "models"
	if models == 1 {
		noun = "model"
	}
	return AuthOK, fmt.Sprintf("%d %s from %s", models, noun, strings.Join(names, ", "))
}
