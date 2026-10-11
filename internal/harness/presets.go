package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Built-in harness kinds.
const (
	KindCodex       = "codex"
	KindClaude      = "claude"
	KindGemini      = "gemini"
	KindAntigravity = "antigravity"
	KindGeneric     = "generic"
)

var kindAliases = map[string]string{
	"agy":         KindAntigravity,
	"claude-code": KindClaude,
	"gemini-cli":  KindGemini,
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
	ks := []string{KindCodex, KindClaude, KindGemini, KindAntigravity}
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
//   - Gemini CLI: `gemini -p` runs non-interactively; -p text is appended to
//     stdin input. --output-format json returns {"response": "...", "stats":
//     {...}} or {"error": {...}}. Auth: interactive `gemini` (Login with
//     Google) or GEMINI_API_KEY / Vertex AI env vars; there is no status
//     subcommand, so cached-credential files and env vars are inspected.
//   - Antigravity CLI (`agy`): `agy -p` is headless/print mode with
//     --output-format text|json|stream-json. We use the documented
//     `--input-format stream-json --output-format stream-json` session mode,
//     which reads {"event":"user","message":{"content":...}} lines on stdin
//     (avoiding OS argument-length limits on long transcripts) and ends each
//     turn with {"event":"result","result":{"status":..,"response":..}}.
//     Auth: interactive `agy` (OS keyring / Google sign-in), GEMINI_API_KEY
//     with modelProvider=gemini, or ADC with AGY_ADC_AUTH=true.
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
	case KindGemini:
		return Spec{
			Kind:    KindGemini,
			Command: "gemini",
			Args: []string{"--output-format", "json", "{{model_args}}", "{{extra_args}}",
				"-p", "Respond to the request above (provided on standard input)."},
			ModelFlag:  "--model",
			PromptMode: PromptStdin,
			Output:     OutputJSON,
			JSONPath:   "response",
			ErrorPath:  "error",
			Timeout:    10 * time.Minute,
			LoginArgs:  []string{},
			LoginNote:  "Starts Gemini CLI interactively: choose \"Login with Google\", finish in the browser, then type /quit. Alternatively export GEMINI_API_KEY.",
			statusFunc: geminiAuth,
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
			LoginNote:     "Starts Antigravity CLI interactively; it signs in via the OS keyring or a Google sign-in (prints a URL over SSH). Type /quit when done. For CI use GEMINI_API_KEY + modelProvider=gemini, or AGY_ADC_AUTH=true.",
			statusFunc:    antigravityAuth,
		}, true
	}
	return Spec{}, false
}

func home() string { h, _ := os.UserHomeDir(); return h }

func fileExists(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }

func geminiAuth(context.Context) (AuthState, string) {
	for _, k := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"} {
		if os.Getenv(k) != "" {
			return AuthOK, k + " is set"
		}
	}
	if os.Getenv("GOOGLE_GENAI_USE_VERTEXAI") == "true" {
		return AuthOK, "Vertex AI mode (GOOGLE_GENAI_USE_VERTEXAI=true)"
	}
	if p := filepath.Join(home(), ".gemini", "oauth_creds.json"); fileExists(p) {
		return AuthOK, "cached Google login at " + p
	}
	return AuthMissing, "no GEMINI_API_KEY and no cached login (~/.gemini/oauth_creds.json); run `council login gemini`"
}

func antigravityAuth(context.Context) (AuthState, string) {
	settings := filepath.Join(home(), ".gemini", "antigravity-cli", "settings.json")
	if b, err := os.ReadFile(settings); err == nil {
		var s struct {
			ModelProvider string `json:"modelProvider"`
		}
		if json.Unmarshal(b, &s) == nil && s.ModelProvider == "gemini" {
			if os.Getenv("GEMINI_API_KEY") != "" {
				return AuthOK, "Gemini API key (modelProvider=gemini, GEMINI_API_KEY set)"
			}
			return AuthMissing, "modelProvider=gemini but GEMINI_API_KEY is not set"
		}
	}
	if os.Getenv("AGY_ADC_AUTH") == "true" {
		return AuthOK, "Application Default Credentials (AGY_ADC_AUTH=true)"
	}
	return AuthUnknown, "agy keeps its session in the OS keyring and has no documented status command; run `council doctor --probe` to verify"
}
