package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewDefaultsAndValidation(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		wantErr string
	}{
		{"empty command", Spec{}, "harness command is empty"},
		{"bad prompt mode", Spec{Command: "c", PromptMode: "pigeon"}, `unknown prompt_mode "pigeon"`},
		{"bad output", Spec{Command: "c", Output: "xml"}, `unknown output "xml"`},
		{"bad output source", Spec{Command: "c", OutputSource: "socket"}, `unknown output_source "socket"`},
		{"file source without placeholder", Spec{Command: "c", OutputSource: SourceFile}, "requires {{output_file}}"},
		{"file source with placeholder", Spec{Command: "c", OutputSource: SourceFile, Args: []string{"--out={{output_file}}"}}, ""},
		{"stdin jsonl", Spec{Command: "c", PromptMode: PromptStdin, Output: OutputJSONL}, ""},
		{"file prompt json", Spec{Command: "c", PromptMode: PromptFile, Output: OutputJSON}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, err := New("x", c.spec)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) || !strings.HasPrefix(err.Error(), `agent "x"`) {
					t.Fatalf("got %v, want error containing %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if h.Name() != "x" || h.Kind() != KindGeneric {
				t.Errorf("Name/Kind = %q/%q", h.Name(), h.Kind())
			}
		})
	}
	h, _ := New("bare", Spec{Command: "c"})
	sp := h.Spec()
	if sp.Kind != KindGeneric || sp.PromptMode != PromptArg || sp.Output != OutputText || sp.OutputSource != SourceStdout {
		t.Errorf("defaults not filled in: %+v", sp)
	}
	h, _ = New("k", Spec{Command: "c", Kind: KindClaude})
	if h.Kind() != KindClaude {
		t.Errorf("Kind() = %q", h.Kind())
	}
}

func TestFuncHarness(t *testing.T) {
	f := &Func{AgentName: "n", Fn: func(_ context.Context, p string) (string, error) {
		if p == "fail" {
			return "", errors.New("nope")
		}
		return "echo " + p, nil
	}}
	var _ Harness = f
	if f.Name() != "n" || f.Kind() != "func" {
		t.Fatalf("Name/Kind = %q/%q", f.Name(), f.Kind())
	}
	f.KindName = "mock"
	if f.Kind() != "mock" {
		t.Fatalf("Kind = %q", f.Kind())
	}
	if out, err := f.Run(context.Background(), "hi"); out != "echo hi" || err != nil {
		t.Fatalf("Run = %q %v", out, err)
	}
	if _, err := f.Run(context.Background(), "fail"); err == nil {
		t.Fatal("want error")
	}
}

func TestBuildArgsTable(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		want []string
	}{
		{"arg mode appends prompt", Spec{Args: []string{"run"}}, []string{"run", "PROMPT"}},
		{"arg mode placeholder", Spec{Args: []string{"--q={{prompt}}", "x"}}, []string{"--q=PROMPT", "x"}},
		{"stdin mode adds nothing", Spec{Args: []string{"run"}, PromptMode: PromptStdin}, []string{"run"}},
		{"file mode appends file", Spec{Args: []string{"run"}, PromptMode: PromptFile}, []string{"run", "PF"}},
		{"file mode placeholder", Spec{Args: []string{"-f", "{{prompt_file}}"}, PromptMode: PromptFile}, []string{"-f", "PF"}},
		{"extra args appended when no placeholder", Spec{Args: []string{"run"}, ExtraArgs: []string{"-a", "-b"}, PromptMode: PromptStdin}, []string{"run", "-a", "-b"}},
		{"extra args placeholder", Spec{Args: []string{"{{extra_args}}", "run"}, ExtraArgs: []string{"-a"}, PromptMode: PromptStdin}, []string{"-a", "run"}},
		{"model, workdir, output substitution", Spec{Args: []string{"--m={{model}}", "--cd", "{{workdir}}", "-o", "{{output_file}}"}, Model: "M", Dir: "/w", PromptMode: PromptStdin}, []string{"--m=M", "--cd", "/w", "-o", "OF"}},
		{"model args need a flag", Spec{Args: []string{" {{model_args}} "}, Model: "M", PromptMode: PromptStdin}, nil},
		{"model args need a model", Spec{Args: []string{"{{model_args}}"}, ModelFlag: "-m", PromptMode: PromptStdin}, nil},
		{"model args", Spec{Args: []string{"{{model_args}}"}, Model: "M", ModelFlag: "-m", PromptMode: PromptStdin}, []string{"-m", "M"}},
		{"prompt text is not re-expanded", Spec{Args: []string{"{{prompt}}"}}, []string{"PROMPT"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.spec.Command = "tool"
			h, err := New("x", c.spec)
			if err != nil {
				t.Fatal(err)
			}
			if got := h.BuildArgs("PROMPT", "PF", "OF"); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
	h, _ := New("x", Spec{Command: "t", Args: []string{"{{prompt}}"}})
	if got := h.BuildArgs("say {{output_file}} {{model}}", "PF", "OF"); !reflect.DeepEqual(got, []string{"say {{output_file}} {{model}}"}) {
		t.Errorf("placeholders inside the prompt must be left alone: %q", got)
	}
}

func TestStdinPayload(t *testing.T) {
	cases := []struct{ tmpl, prompt, want string }{
		{"", "raw\nprompt", "raw\nprompt"},
		{"<{{prompt}}>", "p", "<p>"},
		{`{"c":{{prompt_json}}}`, "a \"q\"\n", `{"c":"a \"q\"\n"}`},
		{"{{prompt}}|{{prompt_json}}", "x", `x|"x"`},
	}
	for _, c := range cases {
		h := &CommandHarness{spec: Spec{StdinTemplate: c.tmpl}}
		if got := h.stdinPayload(c.prompt); got != c.want {
			t.Errorf("stdinPayload(%q, %q) = %q, want %q", c.tmpl, c.prompt, got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		raw     string
		want    string
		wantErr string
	}{
		{"text", Spec{Output: OutputText}, "  hi \n", "hi", ""},
		{"text empty", Spec{Output: OutputText}, " \n ", "", "no answer in output"},
		{"json whole doc", Spec{Output: OutputJSON}, `{"a":1}`, `{"a":1}`, ""},
		{"json string doc", Spec{Output: OutputJSON}, `"just text"`, "just text", ""},
		{"json number answer", Spec{Output: OutputJSON, JSONPath: "n"}, `{"n":42}`, "42", ""},
		{"json nested path", Spec{Output: OutputJSON, JSONPath: "choices.0.text"}, `{"choices":[{"text":" a "}]}`, "a", ""},
		{"json invalid", Spec{Output: OutputJSON, JSONPath: "r"}, `{"r":`, "", "output is not valid JSON"},
		{"json missing path", Spec{Output: OutputJSON, JSONPath: "r"}, `{"x":1}`, "", `JSON path "r" not found`},
		{"json null answer", Spec{Output: OutputJSON, JSONPath: "r"}, `{"r":null}`, "", `JSON path "r" is null`},
		{"json empty answer", Spec{Output: OutputJSON, JSONPath: "r"}, `{"r":"  "}`, "", "no answer in output"},
		{"json error string", Spec{Output: OutputJSON, JSONPath: "r", ErrorPath: "error"}, `{"r":"x","error":"boom"}`, "", "harness reported error: boom"},
		{"json error bool uses answer", Spec{Output: OutputJSON, JSONPath: "r", ErrorPath: "is_error"}, `{"r":"why","is_error":true}`, "", "harness reported error: why"},
		{"json error bool without answer", Spec{Output: OutputJSON, JSONPath: "r", ErrorPath: "is_error"}, `{"is_error":true}`, "", "harness reported error: true"},
		{"json error object", Spec{Output: OutputJSON, JSONPath: "r", ErrorPath: "error"}, `{"error":{"code":1}}`, "", `harness reported error: {"code":1}`},
		{"json falsy error", Spec{Output: OutputJSON, JSONPath: "r", ErrorPath: "error"}, `{"r":"ok","error":null}`, "ok", ""},
		{"json false error", Spec{Output: OutputJSON, JSONPath: "r", ErrorPath: "is_error"}, `{"r":"ok","is_error":false}`, "ok", ""},
		{"jsonl last match wins", Spec{Output: OutputJSONL, JSONPath: "t"}, "{\"t\":\"a\"}\n{\"t\":\"b\"}\n{\"x\":1}\n", "b", ""},
		{"jsonl skips noise", Spec{Output: OutputJSONL, JSONPath: "t"}, "{\"t\":\"a\"}\nlog line\n{broken\n\n", "a", ""},
		{"jsonl none", Spec{Output: OutputJSONL, JSONPath: "t"}, "log\n{\"x\":1}\n", "", "no answer in output"},
		{"jsonl empty", Spec{Output: OutputJSONL, JSONPath: "t"}, "", "", "no answer in output"},
		{"jsonl error line", Spec{Output: OutputJSONL, JSONPath: "t", ErrorPath: "err"}, "{\"t\":\"a\"}\n{\"err\":\"late failure\"}\n", "", "harness reported error: late failure"},
		{"jsonl crlf", Spec{Output: OutputJSONL, JSONPath: "t"}, "{\"t\":\"a\"}\r\n", "a", ""},
		{"unsupported", Spec{Output: "yaml"}, "x", "", `unsupported output format "yaml"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := &CommandHarness{name: "x", spec: c.spec}
			got, err := h.parse([]byte(c.raw))
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("got %q %v, want error %q", got, err, c.wantErr)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got %q %v, want %q", got, err, c.want)
			}
		})
	}
}

func TestTruthyAndStringify(t *testing.T) {
	cases := []struct {
		v    any
		want bool
		str  string
	}{
		{nil, false, "null"},
		{false, false, "false"},
		{true, true, "true"},
		{"", false, ""},
		{"x", true, "x"},
		{float64(0), false, "0"},
		{1.5, true, "1.5"},
		{map[string]any{}, false, "{}"},
		{map[string]any{"a": 1}, true, `{"a":1}`},
		{[]any{}, false, "[]"},
		{[]any{1}, true, "[1]"},
		{42, true, "42"}, // not produced by encoding/json, but handled
	}
	for _, c := range cases {
		if got := truthy(c.v); got != c.want {
			t.Errorf("truthy(%#v) = %v", c.v, got)
		}
		if got := stringify(c.v); got != c.str {
			t.Errorf("stringify(%#v) = %q, want %q", c.v, got, c.str)
		}
	}
}

func TestSanitizeAndArgsContain(t *testing.T) {
	cases := map[string]string{"claude-opus_4": "claude-opus_4", "a b/c:d": "a_b_c_d", "ü": "_", "": ""}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
	if !argsContain([]string{"a", "--o={{output_file}}"}, "{{output_file}}") || argsContain(nil, "x") || argsContain([]string{"a"}, "b") {
		t.Error("argsContain")
	}
}

func TestLookupTable(t *testing.T) {
	doc := map[string]any{"a": map[string]any{"b": []any{"x", map[string]any{"c": "y"}}, "s": "str"}}
	cases := []struct {
		path string
		want any
		ok   bool
	}{
		{"", doc, true},
		{" . ", doc, true},
		{".a.s", "str", true},
		{"a.b.0", "x", true},
		{"a.b.-1.c", "y", true},
		{"a.b.-2", "x", true},
		{"a.b.-3", nil, false},
		{"a.b.2", nil, false},
		{"a.b.x", nil, false},
		{"a.s.0", nil, false}, // cannot index into a string
		{"a.z", nil, false},
	}
	for _, c := range cases {
		got, ok := Lookup(doc, c.path)
		if ok != c.ok || (ok && !reflect.DeepEqual(got, c.want)) {
			t.Errorf("Lookup(%q) = %v, %v", c.path, got, ok)
		}
	}
}

func TestRunErrors(t *testing.T) {
	needSh(t)
	t.Run("command not found", func(t *testing.T) {
		h, _ := New("ghost", Spec{Command: filepath.Join(t.TempDir(), "missing")})
		_, err := h.Run(context.Background(), "x")
		if err == nil || !strings.HasPrefix(err.Error(), "ghost: ") || !strings.Contains(err.Error(), "exited") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("non-zero exit falls back to stdout", func(t *testing.T) {
		h, _ := New("a", Spec{Command: script(t, `echo "only on stdout"; exit 4`)})
		_, err := h.Run(context.Background(), "x")
		if err == nil || !strings.Contains(err.Error(), "exit status 4: only on stdout") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("non-zero exit with parse error", func(t *testing.T) {
		h, _ := New("a", Spec{Command: script(t, `echo "garbage"; echo "auth failed" >&2; exit 1`), Output: OutputJSON})
		_, err := h.Run(context.Background(), "x")
		if err == nil || !strings.Contains(err.Error(), "output is not valid JSON") || !strings.Contains(err.Error(), "auth failed") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("long stderr is tailed", func(t *testing.T) {
		h, _ := New("a", Spec{Command: script(t, `i=0; while [ $i -lt 300 ]; do printf 'line%03d ' $i >&2; i=$((i+1)); done; exit 1`)})
		_, err := h.Run(context.Background(), "x")
		if err == nil || strings.Contains(err.Error(), "line000") || !strings.Contains(err.Error(), "…") || !strings.Contains(err.Error(), "line299") {
			t.Fatalf("got %.200v", err)
		}
	})
	t.Run("output file missing but non-zero exit", func(t *testing.T) {
		h, _ := New("a", Spec{Command: script(t, `echo "stdout answer"; echo "it broke" >&2; exit 2`), Args: []string{"{{output_file}}"}, OutputSource: SourceFile, PromptMode: PromptStdin})
		_, err := h.Run(context.Background(), "x")
		if err == nil || !strings.Contains(err.Error(), "exit status 2: it broke") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("parent context cancelled", func(t *testing.T) {
		h, _ := New("a", Spec{Command: script(t, `sleep 5`)})
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		_, err := h.Run(ctx, "x")
		if err == nil || err.Error() != "a: context canceled" || !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("parent deadline", func(t *testing.T) {
		h, _ := New("a", Spec{Command: script(t, `sleep 5`)})
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := h.Run(ctx, "x")
		if err == nil || !strings.Contains(err.Error(), "a: timed out") || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("temp dir unavailable", func(t *testing.T) {
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))
		h, _ := New("a", Spec{Command: "true"})
		if _, err := h.Run(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "does-not-exist") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestRunWorkdirAndPromptFile(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	// The prompt file is removed after the run, together with its temp dir.
	h, err := New("a b", Spec{Command: script(t, `pwd; cat "$1"; echo "$1" > "$PWD/pf"`), PromptMode: PromptFile, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.Run(context.Background(), "from file")
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(dir)
	if lines := strings.Split(got, "\n"); len(lines) != 2 || (lines[0] != dir && lines[0] != real) || lines[1] != "from file" {
		t.Fatalf("got %q", got)
	}
	pf, _ := os.ReadFile(filepath.Join(dir, "pf"))
	p := strings.TrimSpace(string(pf))
	if !strings.Contains(p, "council-a_b-") {
		t.Errorf("temp dir should include the sanitized agent name: %s", p)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("prompt file %s should be cleaned up: %v", p, err)
	}
}

func TestCheckBranches(t *testing.T) {
	needSh(t)
	t.Run("status fails silently", func(t *testing.T) {
		h, _ := New("a", Spec{Command: script(t, `case "$1" in --version) exit 1;; *) exit 7;; esac`), StatusArgs: []string{"whoami"}})
		st := h.Check(context.Background())
		if !st.Installed || st.Version != "" || st.Auth != AuthMissing || st.Detail != "exit status 7" {
			t.Fatalf("got %+v", st)
		}
	})
	t.Run("no status configured", func(t *testing.T) {
		h, _ := New("a", Spec{Command: script(t, `echo "v1
extra line"`)})
		st := h.Check(context.Background())
		if st.Auth != AuthUnknown || st.Detail != "no status command configured" || st.Version != "v1" {
			t.Fatalf("got %+v", st)
		}
	})
	t.Run("status func", func(t *testing.T) {
		h, _ := New("a", Spec{Command: script(t, `exit 0`), statusFunc: func(context.Context) (AuthState, string) { return AuthOK, "custom" }})
		if st := h.Check(context.Background()); st.Auth != AuthOK || st.Detail != "custom" {
			t.Fatalf("got %+v", st)
		}
	})
	t.Run("not installed", func(t *testing.T) {
		h, _ := New("a", Spec{Command: "definitely-not-a-real-binary-xyz", Kind: KindCodex})
		st := h.Check(context.Background())
		if st.Installed || st.Detail != "not found on PATH" || st.Kind != KindCodex || st.Auth != AuthUnknown {
			t.Fatalf("got %+v", st)
		}
	})
	t.Run("cancelled context", func(t *testing.T) {
		h, _ := New("a", Spec{Command: script(t, `sleep 5`), StatusArgs: []string{"status"}})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		start := time.Now()
		if st := h.Check(ctx); st.Auth != AuthMissing || st.Version != "" {
			t.Fatalf("got %+v", st)
		}
		if time.Since(start) > 2*time.Second {
			t.Fatal("Check should not wait on a cancelled context")
		}
	})
}

func TestHomeAndFileExists(t *testing.T) {
	d := t.TempDir()
	t.Setenv("HOME", d)
	t.Setenv("USERPROFILE", d)
	if home() != d {
		t.Errorf("home() = %q, want %q", home(), d)
	}
	f := filepath.Join(d, "f")
	writeFile(t, f, "x")
	if !fileExists(f) || fileExists(d) || fileExists(filepath.Join(d, "nope")) {
		t.Error("fileExists")
	}
}
