package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeCLIs copies the fake codex, claude, agy and opencode scripts from
// testdata/fakecli into a temp dir, puts it first on PATH and returns the
// paths of files that record the fake's argv and stdin.
func fakeCLIs(t *testing.T) (argsFile, stdinFile string) {
	t.Helper()
	needSh(t)
	bin := t.TempDir()
	for _, name := range []string{"codex", "claude", "agy", "opencode"} {
		b, err := os.ReadFile(filepath.Join("testdata", "fakecli", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, name), b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	rec := t.TempDir()
	argsFile, stdinFile = filepath.Join(rec, "args"), filepath.Join(rec, "stdin")
	t.Setenv("FAKE_ARGS", argsFile)
	t.Setenv("FAKE_STDIN", stdinFile)
	t.Setenv("FAKE_CHILD", filepath.Join(rec, "child.pid"))
	t.Setenv("FAKE_MODE", "ok")
	t.Setenv("FAKE_AUTH", "ok")
	return argsFile, stdinFile
}

func readLines(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func presetHarness(t *testing.T, kind string, mod func(*Spec)) *CommandHarness {
	t.Helper()
	sp, ok := Preset(kind)
	if !ok {
		t.Fatalf("no preset %q", kind)
	}
	if mod != nil {
		mod(&sp)
	}
	h, err := New(kind, sp)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// TestPresetRunContracts drives each built-in preset against a fake of the
// real CLI that checks the documented argument and stdin contract and prints
// the CLI's real output format.
func TestPresetRunContracts(t *testing.T) {
	const prompt = "What is 6*7?\nAnswer with \"quotes\" and a vote."
	n := len(prompt)
	agyLine := `{"event":"user","message":{"content":"What is 6*7?\nAnswer with \"quotes\" and a vote."}}`
	type tc struct {
		kind      string
		model     string
		extra     []string
		wantArgs  []string // "<OUT>" matches the temp output file
		wantStdin string
		want      string
	}
	cases := []tc{
		{KindCodex, "", nil,
			[]string{"exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "--color", "never", "--output-last-message", "<OUT>", "-"},
			prompt, "codex read " + strconv.Itoa(n) + " bytes"},
		{KindCodex, "gpt-5-codex", []string{"-c", "model_reasoning_effort=high"},
			[]string{"exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "--color", "never", "--output-last-message", "<OUT>", "--model", "gpt-5-codex", "-c", "model_reasoning_effort=high", "-"},
			prompt, "codex read " + strconv.Itoa(n) + " bytes"},
		{KindClaude, "", nil,
			[]string{"-p", "--output-format", "json", "--no-session-persistence"},
			prompt, "claude read " + strconv.Itoa(n) + " bytes"},
		{KindClaude, "opus", []string{"--permission-mode", "plan"},
			[]string{"-p", "--output-format", "json", "--no-session-persistence", "--model", "opus", "--permission-mode", "plan"},
			prompt, "claude read " + strconv.Itoa(n) + " bytes"},
		{KindAntigravity, "", nil,
			[]string{"--input-format", "stream-json", "--output-format", "stream-json"},
			agyLine + "\n", "agy read " + strconv.Itoa(len(agyLine)) + " bytes"},
		{"agy", "gemini-3.1-pro-high", nil,
			[]string{"--input-format", "stream-json", "--output-format", "stream-json", "--model", "gemini-3.1-pro-high"},
			agyLine + "\n", "agy read " + strconv.Itoa(len(agyLine)) + " bytes"},
		{KindOpenCode, "", nil,
			[]string{"run", "--format", "json", "--agent", "plan"},
			prompt, "opencode read " + strconv.Itoa(n) + " bytes"},
		{"open-code", "anthropic/claude-sonnet-4-5", []string{"--variant", "high"},
			[]string{"run", "--format", "json", "--agent", "plan", "--model", "anthropic/claude-sonnet-4-5", "--variant", "high"},
			prompt, "opencode read " + strconv.Itoa(n) + " bytes"},
	}
	for _, c := range cases {
		t.Run(c.kind+"/"+c.model, func(t *testing.T) {
			argsFile, stdinFile := fakeCLIs(t)
			h := presetHarness(t, c.kind, func(sp *Spec) { sp.Model, sp.ExtraArgs = c.model, c.extra })
			got, err := h.Run(context.Background(), prompt)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("answer %q, want %q", got, c.want)
			}
			args := readLines(t, argsFile)
			for i, a := range c.wantArgs {
				if a == "<OUT>" && i < len(args) && filepath.Base(args[i]) == "output.txt" {
					args[i] = "<OUT>"
				}
			}
			if !reflect.DeepEqual(args, c.wantArgs) {
				t.Errorf("argv\n got %q\nwant %q", args, c.wantArgs)
			}
			stdin, _ := os.ReadFile(stdinFile)
			if string(stdin) != c.wantStdin {
				t.Errorf("stdin\n got %q\nwant %q", stdin, c.wantStdin)
			}
		})
	}
}

// TestPresetRunFailures covers each preset's failure modes: errors reported
// in the output format, malformed output, missing answers and non-zero exits.
func TestPresetRunFailures(t *testing.T) {
	cases := []struct {
		kind, mode string
		want       []string // substrings of the error
	}{
		{KindCodex, "nofile", []string{"codex: expected output file was not written"}},
		{KindCodex, "empty", []string{"codex: no answer in output"}},
		{KindCodex, "fail", []string{"codex: codex exited: exit status 1", "401 Unauthorized"}},
		{KindClaude, "is_error", []string{"harness reported error: Credit balance is too low"}},
		{KindClaude, "is_error_exit", []string{"exit status 1", "harness reported error: Invalid API key"}},
		{KindClaude, "malformed", []string{"output is not valid JSON", "Claude Code is starting"}},
		{KindClaude, "empty", []string{"no answer in output"}},
		{KindClaude, "fail", []string{"claude exited: exit status 1", "ECONNREFUSED"}},
		{KindAntigravity, "error", []string{"harness reported error: quota exhausted"}},
		{KindAntigravity, "error_exit", []string{"exit status 1", "authentication required"}},
		{KindAntigravity, "malformed", []string{"no answer in output"}},
		{KindAntigravity, "noresult", []string{"no answer in output"}},
		{KindAntigravity, "fail", []string{"exit status 3", "keyring unavailable"}},
		{KindOpenCode, "error", []string{"exit status 1", "harness reported error: invalid x-api-key"}},
		{KindOpenCode, "malformed", []string{"no answer in output"}},
		{KindOpenCode, "notext", []string{"no answer in output"}},
		{KindOpenCode, "fail", []string{"exit status 1", "Model not found"}},
	}
	for _, c := range cases {
		t.Run(c.kind+"/"+c.mode, func(t *testing.T) {
			fakeCLIs(t)
			t.Setenv("FAKE_MODE", c.mode)
			_, err := presetHarness(t, c.kind, nil).Run(context.Background(), "q")
			if err == nil {
				t.Fatal("want error")
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}

// TestPresetTimeoutKillsProcessTree: every preset's CLI starts a child that
// would outlive it; the timeout must kill the whole process group.
func TestPresetTimeoutKillsProcessTree(t *testing.T) {
	for _, kind := range BuiltinKinds() {
		t.Run(kind, func(t *testing.T) {
			fakeCLIs(t)
			t.Setenv("FAKE_MODE", "hang")
			h := presetHarness(t, kind, func(sp *Spec) { sp.Timeout = 500 * time.Millisecond })
			start := time.Now()
			_, err := h.Run(context.Background(), "q")
			if err == nil || !strings.Contains(err.Error(), kind+": timed out") {
				t.Fatalf("want timeout, got %v", err)
			}
			// The grandchild keeps stdout open; had it survived, Run would
			// have waited for WaitDelay (3s) more, i.e. at least 3.5s in total.
			if d := time.Since(start); d > 2500*time.Millisecond {
				t.Errorf("Run returned after %v: child process was not killed", d)
			}
			assertProcessGone(t, readPID(t, os.Getenv("FAKE_CHILD")))
		})
	}
}

func readPID(t *testing.T, p string) int {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("child pid not recorded: %v", err)
	}
	pid := 0
	for _, c := range strings.TrimSpace(string(b)) {
		pid = pid*10 + int(c-'0')
	}
	return pid
}

// TestPresetCheck runs doctor checks against the fakes.
func TestPresetCheck(t *testing.T) {
	cases := []struct {
		kind, auth  string
		env         map[string]string
		wantAuth    AuthState
		wantVersion string
		wantDetail  string
	}{
		{KindCodex, "ok", nil, AuthOK, "codex-cli 0.46.0", "Logged in using ChatGPT"},
		{KindCodex, "missing", nil, AuthMissing, "codex-cli 0.46.0", "Not logged in"},
		{KindClaude, "ok", nil, AuthOK, "2.0.14 (Claude Code)", `{"loggedIn": true, "authMethod": "claude.ai"}`},
		{KindClaude, "missing", nil, AuthMissing, "2.0.14 (Claude Code)", `{"loggedIn": false}`},
		{KindAntigravity, "ok", nil, AuthOK, "1.3.3", "signed in (2 models available)"},
		{KindAntigravity, "missing", nil, AuthMissing, "1.3.3", "Please sign in to view available models"},
		{KindAntigravity, "nokey", nil, AuthMissing, "1.3.3", "GEMINI_API_KEY environment variable is not set"},
		{KindOpenCode, "ok", nil, AuthOK, "1.19.0", "2 stored credential(s), 0 provider environment variable(s)"},
		{KindOpenCode, "env", nil, AuthOK, "1.19.0", "0 stored credential(s), 1 provider environment variable(s)"},
		{KindOpenCode, "none", nil, AuthOK, "1.19.0", "only OpenCode's free models"},
		{KindOpenCode, "broken", nil, AuthMissing, "1.19.0", "failed to read auth.json"},
	}
	for _, c := range cases {
		t.Run(c.kind+"/"+c.auth, func(t *testing.T) {
			fakeCLIs(t)
			t.Setenv("FAKE_AUTH", c.auth)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			st := presetHarness(t, c.kind, nil).Check(context.Background())
			if !st.Installed || st.Agent != c.kind || st.Kind != c.kind || filepath.Base(st.Path) != st.Command {
				t.Errorf("unexpected status %+v", st)
			}
			if st.Auth != c.wantAuth || st.Version != c.wantVersion || !strings.Contains(st.Detail, c.wantDetail) {
				t.Errorf("got auth=%s version=%q detail=%q", st.Auth, st.Version, st.Detail)
			}
		})
	}
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPresetLoginCommands(t *testing.T) {
	cases := []struct {
		kind     string
		wantArgv []string
		wantNote string
	}{
		{KindCodex, []string{"codex", "login"}, "--device-auth"},
		{KindClaude, []string{"claude", "auth", "login"}, "--console"},
		{KindAntigravity, []string{"agy"}, "GEMINI_API_KEY"},
		{"agy", []string{"agy"}, "OS keyring"},
		{KindOpenCode, []string{"opencode", "auth", "login"}, "--provider"},
	}
	for _, c := range cases {
		argv, note := presetHarness(t, c.kind, nil).LoginCommand()
		if !reflect.DeepEqual(argv, c.wantArgv) || !strings.Contains(note, c.wantNote) {
			t.Errorf("%s: got %q %q", c.kind, argv, note)
		}
	}
}

// TestPresetLoginAndStatusAgainstFakes runs the login and status argv the
// presets produce against the fakes, as council login/doctor would.
func TestPresetLoginAndStatusAgainstFakes(t *testing.T) {
	argsFile, _ := fakeCLIs(t)
	for _, c := range []struct {
		kind string
		want []string
	}{
		{KindCodex, []string{"login"}},
		{KindClaude, []string{"auth", "login"}},
		{KindOpenCode, []string{"auth", "login"}},
	} {
		argv, _ := presetHarness(t, c.kind, nil).LoginCommand()
		out, err := runQuietArgv(argv)
		if err != nil || !strings.Contains(out, "fake "+c.kind+" login") {
			t.Errorf("%s login: %q %v", c.kind, out, err)
		}
		if got := readLines(t, argsFile); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s login argv %q, want %q", c.kind, got, c.want)
		}
	}
	for _, kind := range []string{KindCodex, KindClaude} {
		h := presetHarness(t, kind, nil)
		t.Setenv("FAKE_AUTH", "ok")
		if _, err := h.runQuiet(context.Background(), 5*time.Second, h.Spec().StatusArgs...); err != nil {
			t.Errorf("%s status should succeed when signed in: %v", kind, err)
		}
		t.Setenv("FAKE_AUTH", "missing")
		if _, err := h.runQuiet(context.Background(), 5*time.Second, h.Spec().StatusArgs...); err == nil {
			t.Errorf("%s status should fail when signed out", kind)
		}
	}
}

func runQuietArgv(argv []string) (string, error) {
	h := &CommandHarness{name: "x", spec: Spec{Command: argv[0]}}
	return h.runQuiet(context.Background(), 5*time.Second, argv[1:]...)
}

func TestCanonicalKindAndBuiltins(t *testing.T) {
	cases := map[string]string{
		"agy": KindAntigravity, " AGY ": KindAntigravity, "claude-code": KindClaude,
		"open-code": KindOpenCode, "OpenCode": KindOpenCode, "openai": KindCodex, "custom": KindGeneric,
		"gemini": "gemini", "gemini-cli": "gemini-cli",
		"Codex": KindCodex, "ollama": "ollama", "": "",
	}
	for in, want := range cases {
		if got := CanonicalKind(in); got != want {
			t.Errorf("CanonicalKind(%q) = %q, want %q", in, got, want)
		}
	}
	if got, want := BuiltinKinds(), []string{"antigravity", "claude", "codex", "opencode"}; !reflect.DeepEqual(got, want) {
		t.Errorf("BuiltinKinds() = %q", got)
	}
	for _, k := range []string{"generic", "custom", "ollama", "", "gemini", "gemini-cli"} {
		if _, ok := Preset(k); ok {
			t.Errorf("Preset(%q) should not exist", k)
		}
	}
	for _, k := range []string{"codex", "openai", "claude", "claude-code", "antigravity", "agy", "opencode", "open-code"} {
		sp, ok := Preset(k)
		if !ok || sp.Kind != CanonicalKind(k) || sp.Timeout != 10*time.Minute || sp.LoginArgs == nil {
			t.Errorf("Preset(%q) = %+v, %v", k, sp, ok)
		}
		if len(sp.StatusArgs) == 0 {
			t.Errorf("Preset(%q) needs a status command", k)
		}
	}
}

func TestRemoved(t *testing.T) {
	for _, k := range []string{"gemini", "Gemini-CLI ", "gemini-cli"} {
		why, ok := Removed(k)
		if !ok || !strings.Contains(why, "Gemini CLI is deprecated") || !strings.Contains(why, "harness: antigravity") || !strings.Contains(why, "harness: generic") {
			t.Errorf("Removed(%q) = %q, %v", k, why, ok)
		}
	}
	for _, k := range []string{"agy", "codex", "generic", ""} {
		if _, ok := Removed(k); ok {
			t.Errorf("Removed(%q) should be false", k)
		}
	}
}

func TestAntigravityStatus(t *testing.T) {
	errExit := errors.New("exit status 1")
	cases := []struct {
		name, out string
		err       error
		want      AuthState
		detail    string
	}{
		{"signed in", "Fetching available models...\ngemini-3.8-flash-high\tGemini 3.8 Flash (High)\ngemini-3.1-pro\tGemini 3.1 Pro\n", nil, AuthOK, "signed in (2 models available)"},
		{"signed in, no list", "Fetching available models...", nil, AuthOK, "signed in"},
		{"signed out", "Fetching available models...\nError: Please sign in to view available models. Launch the CLI without arguments to sign in.", errExit, AuthMissing, "Please sign in to view available models. Launch the CLI without arguments to sign in."},
		{"no output", "", errExit, AuthMissing, "exit status 1"},
	}
	for _, c := range cases {
		if got, detail := antigravityStatus(c.out, c.err); got != c.want || detail != c.detail {
			t.Errorf("%s: got %s %q", c.name, got, detail)
		}
	}
}

func TestOpenCodeStatus(t *testing.T) {
	errExit := errors.New("exit status 1")
	cases := []struct {
		name, out string
		err       error
		want      AuthState
		detail    string
	}{
		{"stored", "\x1b[0m┌  Credentials \x1b[90m~/.local/share/opencode/auth.json\n│\n●  Anthropic \x1b[90mapi\n│\n└  1 credential\n", nil, AuthOK, "1 stored credential(s), 0 provider environment variable(s)"},
		{"both", "└  2 credentials\n\n┌  Environment\n│\n●  OpenAI OPENAI_API_KEY\n●  Anthropic ANTHROPIC_API_KEY\n└  2 environment variables\n", nil, AuthOK, "2 stored credential(s), 2 provider environment variable(s)"},
		{"none", "┌  Credentials ~/.local/share/opencode/auth.json\n│\n└  0 credentials\n", nil, AuthOK, "no provider configured: only OpenCode's free models (run `council login opencode` to add one)"},
		{"failed", "\x1b[91mError: \x1b[0mfailed to read auth.json", errExit, AuthMissing, "Error: failed to read auth.json"},
		{"failed silently", "", errExit, AuthMissing, "exit status 1"},
	}
	for _, c := range cases {
		if got, detail := openCodeStatus(c.out, c.err); got != c.want || detail != c.detail {
			t.Errorf("%s: got %s %q", c.name, got, detail)
		}
	}
}

func TestOpenCodeMultiStepAnswerIsTheLastText(t *testing.T) {
	fakeCLIs(t)
	t.Setenv("FAKE_MODE", "multi")
	got, err := presetHarness(t, KindOpenCode, nil).Run(context.Background(), "q")
	if err != nil || got != "final answer" {
		t.Fatalf("got %q, %v", got, err)
	}
}
