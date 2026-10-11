package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Parsa2820/council/internal/harness"
)

func TestDurationYAML(t *testing.T) {
	cases := []struct {
		in      string
		want    Duration
		wantErr string
	}{
		{`t: 90s`, Duration(90 * time.Second), ""},
		{`t: 10m`, Duration(10 * time.Minute), ""},
		{`t: "1h30m"`, Duration(90 * time.Minute), ""},
		{`t: ""`, 0, ""},
		{`t: 0`, 0, ""},
		{`t: 30`, 0, `line 1: invalid duration "30"`},
		{`t: soon`, 0, `invalid duration "soon"`},
		{`t: [1, 2]`, 0, "cannot unmarshal"},
	}
	for _, c := range cases {
		var v struct {
			T Duration `yaml:"t"`
		}
		err := yaml.Unmarshal([]byte(c.in), &v)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: got %v, want error %q", c.in, err, c.wantErr)
			}
			continue
		}
		if err != nil || v.T != c.want {
			t.Errorf("%s: got %v %v, want %v", c.in, time.Duration(v.T), err, time.Duration(c.want))
		}
	}
	out, err := yaml.Marshal(struct {
		T Duration `yaml:"t"`
	}{Duration(90 * time.Second)})
	if err != nil || string(out) != "t: 1m30s\n" {
		t.Errorf("MarshalYAML: %q %v", out, err)
	}
	if v, err := Duration(0).MarshalYAML(); v != "0s" || err != nil {
		t.Errorf("MarshalYAML(0) = %v %v", v, err)
	}
}

func TestDefaultPathResolution(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("COUNCIL_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := filepath.Join(home, ".config", "council")

	if got := DefaultPath(); got != filepath.Join(dir, "config.yaml") {
		t.Errorf("no file: %s", got)
	}
	writeFile(t, filepath.Join(dir, "config"), "")
	if got := DefaultPath(); got != filepath.Join(dir, "config") {
		t.Errorf("extension-less config: %s", got)
	}
	writeFile(t, filepath.Join(dir, "config.yml"), "")
	if got := DefaultPath(); got != filepath.Join(dir, "config.yml") {
		t.Errorf("config.yml preferred over config: %s", got)
	}
	writeFile(t, filepath.Join(dir, "config.yaml"), "")
	if got := DefaultPath(); got != filepath.Join(dir, "config.yaml") {
		t.Errorf("config.yaml preferred: %s", got)
	}
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if got := DefaultPath(); got != filepath.Join(xdg, "council", "config.yaml") {
		t.Errorf("XDG: %s", got)
	}
	t.Setenv("COUNCIL_CONFIG", "/explicit.yaml")
	if got := DefaultPath(); got != "/explicit.yaml" {
		t.Errorf("COUNCIL_CONFIG: %s", got)
	}
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadErrors(t *testing.T) {
	d := t.TempDir()
	bad := filepath.Join(d, "bad.yaml")
	writeFile(t, bad, "defaults: {rounds: zero}\n")
	if _, err := Load(bad); err == nil || !strings.HasPrefix(err.Error(), bad+": ") {
		t.Errorf("parse error should be prefixed with the path: %v", err)
	}
	if _, err := Load(d); err == nil {
		t.Error("loading a directory should fail")
	}
	good := filepath.Join(d, "good.yaml")
	writeFile(t, good, "defaults: {rounds: 5}\n")
	c, err := Load(good)
	if err != nil || c.Path() != good || c.Defaults.Rounds != 5 {
		t.Fatalf("Load: %+v %v", c, err)
	}
	missing := filepath.Join(d, "missing.yaml")
	c, err = Load(missing)
	if err != nil || c.Path() != missing || !reflect.DeepEqual(c.Defaults.Agents, []string{"codex", "claude", "agy"}) {
		t.Fatalf("missing file should load defaults: %+v %v", c, err)
	}
}

func TestParseTable(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{"empty", "", ""},
		{"comments only", "# nothing\n", ""},
		{"unknown top-level field", "default: {}\n", "field default not found"},
		{"unknown agent field", "agents: {a: {harness: generic, command: x, colour: red}}\n", "field colour not found"},
		{"negative rounds", "defaults: {rounds: -1}\n", "defaults.rounds must be >= 1"},
		{"unknown moderator", "defaults: {moderator: judge}\n", `defaults.moderator: unknown agent "judge"`},
		{"builtin moderator", "defaults: {moderator: claude}\n", ""},
		{"defined moderator", "defaults: {moderator: j}\nagents: {j: {command: judge}}\n", ""},
		{"unknown harness", "agents: {a: {harness: chatgpt}}\n", `agent "a": unknown harness "chatgpt" (want one of antigravity, claude, codex, opencode or generic)`},
		{"removed gemini harness", "agents: {g: {harness: gemini}}\n", `agent "g": the gemini harness was removed in council v0.2.1 because Gemini CLI is deprecated; use ` + "`harness: antigravity`" + ` (Google's Antigravity CLI, agy) or ` + "`harness: generic`" + ` instead`},
		{"removed gemini-cli alias", "agents: {g: {harness: Gemini-CLI, command: gemini}}\n", "Gemini CLI is deprecated"},
		{"gemini by name in defaults", "defaults: {agents: [codex, gemini]}\nagents: {codex: {}}\n", ""},
		{"gemini by name as moderator", "defaults: {moderator: gemini}\n", `defaults.moderator: agent "gemini" is not defined, and the gemini harness was removed`},
		{"gemini name with another harness", "agents: {gemini: {harness: antigravity}}\n", ""},
		{"generic needs command", "agents: {a: {harness: generic}}\n", `agent "a": generic harness needs `},
		{"implicit generic needs command", "agents: {a: {model: m}}\n", "generic harness needs"},
		{"bad prompt mode", "agents: {a: {command: x, prompt_mode: carrier-pigeon}}\n", `unknown prompt_mode "carrier-pigeon"`},
		{"bad output", "agents: {codex: {output: xml}}\n", `unknown output "xml"`},
		{"bad output source", "agents: {a: {command: x, output_source: file}}\n", "requires {{output_file}}"},
		{"bad timeout", "agents: {a: {command: x, timeout: 5}}\n", `invalid duration "5"`},
		{"not a mapping", "- a\n- b\n", "cannot unmarshal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := Parse([]byte(c.yaml))
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("got %v, want error %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Agents == nil || cfg.Defaults.Rounds < 1 || cfg.Defaults.RequireExplicitAgreement == nil {
				t.Fatalf("defaults not applied: %+v", cfg.Defaults)
			}
		})
	}
}

func TestApplyDefaultsKeepsExplicitValues(t *testing.T) {
	c, err := Parse([]byte(`defaults:
  agents: [a]
  rounds: 7
  moderator: a
  timeout: 1m
  require_explicit_agreement: false
  max_transcript_chars: 100
agents:
  a: {command: x}
`))
	if err != nil {
		t.Fatal(err)
	}
	d := c.Defaults
	if !reflect.DeepEqual(d.Agents, []string{"a"}) || d.Rounds != 7 || d.Moderator != "a" ||
		d.Timeout != Duration(time.Minute) || *d.RequireExplicitAgreement || d.MaxTranscriptChars != 100 {
		t.Fatalf("got %+v", d)
	}
}

func TestSpecResolution(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("COUNCIL_TEST_BIN", "/opt/bin")
	c, err := Parse([]byte(`defaults:
  timeout: 2m
agents:
  codex: {}
  fast-claude:
    harness: claude-code
    model: haiku
    extra_args: ["--permission-mode", "plan"]
    timeout: 30s
    env: {ANTHROPIC_LOG: debug}
    workdir: ~/proj
  mycode:
    harness: open-code
    command: $COUNCIL_TEST_BIN/opencode
    args: ["--output-format", "json", "-p", "{{prompt}}"]
    prompt_mode: arg
    model_flag: -m
    json_path: response.text
    error_path: err
    login_args: []
    status_args: [whoami]
  tool:
    harness: custom
    command: ~/bin/tool
    args: ["-o", "{{output_file}}"]
    prompt_mode: stdin
    stdin_template: "Q: {{prompt}}"
    output: jsonl
    output_source: file
  agy: {harness: ""}
`))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		check func(harness.Spec) bool
	}{
		{"codex", func(s harness.Spec) bool {
			return s.Kind == harness.KindCodex && s.Command == "codex" && s.Timeout == 2*time.Minute && s.OutputSource == harness.SourceFile
		}},
		{"fast-claude", func(s harness.Spec) bool {
			return s.Kind == harness.KindClaude && s.Model == "haiku" && s.Timeout == 30*time.Second &&
				reflect.DeepEqual(s.ExtraArgs, []string{"--permission-mode", "plan"}) && s.Env["ANTHROPIC_LOG"] == "debug" &&
				s.Dir == filepath.Join(home, "proj") && s.JSONPath == "result"
		}},
		{"mycode", func(s harness.Spec) bool {
			return s.Kind == harness.KindOpenCode && s.Command == "/opt/bin/opencode" && s.PromptMode == harness.PromptArg &&
				s.ModelFlag == "-m" && s.JSONPath == "response.text" && s.ErrorPath == "err" &&
				s.LoginArgs != nil && len(s.LoginArgs) == 0 && reflect.DeepEqual(s.StatusArgs, []string{"whoami"})
		}},
		{"tool", func(s harness.Spec) bool {
			return s.Kind == harness.KindGeneric && s.Command == filepath.Join(home, "bin", "tool") && s.StdinTemplate == "Q: {{prompt}}" &&
				s.Output == harness.OutputJSONL && s.OutputSource == harness.SourceFile && s.Timeout == 2*time.Minute
		}},
		{"agy", func(s harness.Spec) bool { return s.Kind == harness.KindAntigravity && s.Command == "agy" }},
		{"opencode", func(s harness.Spec) bool { return s.Kind == harness.KindOpenCode && s.Command == "opencode" }},
		{"claude-code", func(s harness.Spec) bool { return s.Kind == harness.KindClaude }},
	}
	for _, tc := range cases {
		sp, err := c.Spec(tc.name)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !tc.check(sp) {
			t.Errorf("%s: unexpected spec %+v", tc.name, sp)
		}
		h, err := c.Harness(tc.name)
		if err != nil || h.Name() != tc.name || h.Kind() != sp.Kind {
			t.Errorf("Harness(%s) = %v, %v", tc.name, h, err)
		}
	}
	if _, err := c.Harness("nobody"); err == nil || !strings.Contains(err.Error(), "the config file") {
		t.Errorf("unknown agent without a path should mention the config file: %v", err)
	}
	c.path = "/etc/council.yaml"
	if _, err := c.Spec("nobody"); err == nil || !strings.Contains(err.Error(), "/etc/council.yaml") {
		t.Errorf("unknown agent should mention the config path: %v", err)
	}
	if got := c.AgentNames(); !reflect.DeepEqual(got, []string{"agy", "codex", "fast-claude", "mycode", "tool"}) {
		t.Errorf("AgentNames = %q", got)
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cases := map[string]string{
		"~":        home,
		"~/a/b":    filepath.Join(home, "a", "b"),
		"~user/x":  "~user/x",
		"/abs":     "/abs",
		"rel/~/x":  "rel/~/x",
		"":         "",
		"~nothing": "~nothing",
	}
	for in, want := range cases {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteDefaultErrors(t *testing.T) {
	d := t.TempDir()
	blocker := filepath.Join(d, "file")
	writeFile(t, blocker, "x")
	if err := WriteDefault(filepath.Join(blocker, "sub", "config.yaml"), false); err == nil {
		t.Error("creating a directory below a file should fail")
	}
	p := filepath.Join(d, "c", "config.yaml")
	if err := WriteDefault(p, false); err != nil {
		t.Fatal(err)
	}
	if err := WriteDefault(p, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("second write without force: %v", err)
	}
	writeFile(t, p, "edited")
	if err := WriteDefault(p, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != DefaultYAML {
		t.Error("force should overwrite")
	}
	if runtime.GOOS != "windows" && os.Getuid() != 0 {
		ro := filepath.Join(d, "ro")
		if err := os.MkdirAll(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		if err := WriteDefault(filepath.Join(ro, "config.yaml"), false); err == nil {
			t.Error("writing into a read-only directory should fail")
		}
	}
}

func TestDefaultYAMLRoundTrip(t *testing.T) {
	c, err := Parse([]byte(DefaultYAML))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.AgentNames(), []string{"agy", "claude", "codex", "opencode"}) {
		t.Errorf("default agents: %q", c.AgentNames())
	}
	if d := Default(); !reflect.DeepEqual(d.Defaults, c.Defaults) || d.Path() != "" {
		t.Errorf("Default() differs from parsing DefaultYAML")
	}
}
