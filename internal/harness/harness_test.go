package harness

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func needSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fakes require a POSIX shell")
	}
}

// script writes an executable shell script and returns its path.
func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLookup(t *testing.T) {
	doc := map[string]any{"a": map[string]any{"b": []any{"x", map[string]any{"c": "y"}}}}
	cases := map[string]any{"a.b.0": "x", "a.b.1.c": "y", "a.b.-1.c": "y", "": doc}
	for p, want := range cases {
		got, ok := Lookup(doc, p)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("Lookup(%q)=%v,%v", p, got, ok)
		}
	}
	for _, p := range []string{"a.z", "a.b.9", "a.b.x"} {
		if _, ok := Lookup(doc, p); ok {
			t.Errorf("Lookup(%q) should fail", p)
		}
	}
}

func TestBuildArgsPlaceholders(t *testing.T) {
	h, err := New("x", Spec{
		Command: "tool", Args: []string{"run", "{{model_args}}", "--in={{prompt_file}}", "{{extra_args}}"},
		Model: "m1", ModelFlag: "-m", ExtraArgs: []string{"--fast"}, PromptMode: PromptFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := h.BuildArgs("hi", "/tmp/p", "/tmp/o")
	want := []string{"run", "-m", "m1", "--in=/tmp/p", "--fast"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	// Arg mode without placeholder appends the prompt; no model -> no model args.
	h, _ = New("x", Spec{Command: "tool", Args: []string{"{{model_args}}", "q"}, ModelFlag: "-m"})
	if got := h.BuildArgs("hello", "", ""); !reflect.DeepEqual(got, []string{"q", "hello"}) {
		t.Fatalf("got %q", got)
	}
}

func TestPresetsBuildDocumentedFlags(t *testing.T) {
	for _, k := range BuiltinKinds() {
		sp, ok := Preset(k)
		if !ok {
			t.Fatalf("missing preset %s", k)
		}
		sp.Model = "M"
		h, err := New(k, sp)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		args := strings.Join(h.BuildArgs("PROMPT", "PF", "OF"), " ")
		want := map[string]string{
			KindCodex:       "exec --skip-git-repo-check --ephemeral --sandbox read-only --color never --output-last-message OF --model M -",
			KindClaude:      "-p --output-format json --no-session-persistence --model M",
			KindGemini:      "--output-format json --model M -p Respond to the request above (provided on standard input).",
			KindAntigravity: "--input-format stream-json --output-format stream-json --model M",
		}[k]
		if args != want {
			t.Errorf("%s args:\n got %s\nwant %s", k, args, want)
		}
	}
	if _, ok := Preset("agy"); !ok {
		t.Fatal("agy alias should resolve")
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New("x", Spec{}); err == nil {
		t.Fatal("empty command should fail")
	}
	if _, err := New("x", Spec{Command: "c", PromptMode: "pigeon"}); err == nil {
		t.Fatal("bad prompt mode should fail")
	}
	if _, err := New("x", Spec{Command: "c", OutputSource: SourceFile}); err == nil {
		t.Fatal("file output without {{output_file}} should fail")
	}
}

func TestRunStdinText(t *testing.T) {
	needSh(t)
	h, _ := New("a", Spec{Command: script(t, `tr a-z A-Z`), PromptMode: PromptStdin})
	got, err := h.Run(context.Background(), "hello")
	if err != nil || got != "HELLO" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestRunArgJSONPath(t *testing.T) {
	needSh(t)
	h, _ := New("a", Spec{
		Command: script(t, `printf '{"result":"you said: %s","is_error":false}' "$1"`),
		Output:  OutputJSON, JSONPath: "result", ErrorPath: "is_error",
	})
	got, err := h.Run(context.Background(), "ping")
	if err != nil || got != "you said: ping" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestRunJSONErrorPath(t *testing.T) {
	needSh(t)
	h, _ := New("a", Spec{
		Command: script(t, `echo '{"result":"Credit balance too low","is_error":true}'`),
		Output:  OutputJSON, JSONPath: "result", ErrorPath: "is_error",
	})
	_, err := h.Run(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "Credit balance too low") {
		t.Fatalf("want harness error, got %v", err)
	}
}

func TestRunFilePromptAndOutputFile(t *testing.T) {
	needSh(t)
	// Mimics codex: prompt via file, final message written to -o FILE.
	h, err := New("a", Spec{
		Command:    script(t, `cat "$2" | rev > "$4"; echo "noise on stdout"`),
		Args:       []string{"--prompt", "{{prompt_file}}", "-o", "{{output_file}}"},
		PromptMode: PromptFile, OutputSource: SourceFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.Run(context.Background(), "abc")
	if err != nil || got != "cba" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestRunStdinTemplateJSONL(t *testing.T) {
	needSh(t)
	// Mimics agy stream-json: echo the content back inside a result event.
	body := `read -r line
echo '{"event":"init"}'
echo '{"event":"step_update","step_update":{"text_delta":"partial"}}'
printf '{"event":"result","result":{"status":"SUCCESS","response":%s}}\n' "$(printf '%s' "$line" | sed 's/.*"content":\(.*\)}}$/\1/')"`
	sp, _ := Preset(KindAntigravity)
	sp.Command = script(t, body)
	h, _ := New("agy", sp)
	got, err := h.Run(context.Background(), `say "hi"`)
	if err != nil || got != `say "hi"` {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestRunJSONLError(t *testing.T) {
	needSh(t)
	sp, _ := Preset(KindAntigravity)
	sp.Command = script(t, `cat >/dev/null; echo '{"event":"result","result":{"status":"ERROR","response":"","error":"authentication required"}}'; exit 1`)
	h, _ := New("agy", sp)
	_, err := h.Run(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "authentication required") {
		t.Fatalf("got %v", err)
	}
}

func TestRunNonZeroExitIncludesStderr(t *testing.T) {
	needSh(t)
	h, _ := New("a", Spec{Command: script(t, `echo "not logged in" >&2; exit 3`)})
	_, err := h.Run(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("got %v", err)
	}
}

func TestRunTimeoutKillsProcess(t *testing.T) {
	needSh(t)
	h, _ := New("slow", Spec{Command: script(t, `sleep 30`), Timeout: 200 * time.Millisecond})
	start := time.Now()
	_, err := h.Run(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout took too long: %v", time.Since(start))
	}
}

func TestRunEnv(t *testing.T) {
	needSh(t)
	t.Setenv("COUNCIL_TEST_BASE", "base")
	h, _ := New("a", Spec{Command: script(t, `echo "$FOO"`), Env: map[string]string{"FOO": "${COUNCIL_TEST_BASE}-x"}})
	got, err := h.Run(context.Background(), "")
	if err != nil || got != "base-x" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestCheck(t *testing.T) {
	needSh(t)
	ok := script(t, `case "$1" in --version) echo "fake 1.2.3";; status) echo "Logged in"; exit 0;; esac`)
	h, _ := New("a", Spec{Command: ok, StatusArgs: []string{"status"}})
	st := h.Check(context.Background())
	if !st.Installed || st.Auth != AuthOK || st.Version != "fake 1.2.3" || st.Detail != "Logged in" {
		t.Fatalf("got %+v", st)
	}
	bad := script(t, `case "$1" in status) echo "Not logged in"; exit 1;; esac`)
	h, _ = New("a", Spec{Command: bad, StatusArgs: []string{"status"}})
	if st := h.Check(context.Background()); st.Auth != AuthMissing {
		t.Fatalf("got %+v", st)
	}
	h, _ = New("a", Spec{Command: "definitely-not-a-real-binary-xyz"})
	if st := h.Check(context.Background()); st.Installed {
		t.Fatalf("got %+v", st)
	}
}

func TestLoginCommand(t *testing.T) {
	sp, _ := Preset("codex")
	h, _ := New("codex", sp)
	argv, _ := h.LoginCommand()
	if !reflect.DeepEqual(argv, []string{"codex", "login"}) {
		t.Fatalf("got %q", argv)
	}
	sp, _ = Preset("claude")
	h, _ = New("claude", sp)
	if argv, _ := h.LoginCommand(); !reflect.DeepEqual(argv, []string{"claude", "auth", "login"}) {
		t.Fatalf("got %q", argv)
	}
	h, _ = New("g", Spec{Command: "x"})
	if argv, _ := h.LoginCommand(); argv != nil {
		t.Fatalf("generic without login should be nil, got %q", argv)
	}
}
