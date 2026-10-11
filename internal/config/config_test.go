package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"council/internal/harness"
)

func TestDefaultConfig(t *testing.T) {
	c := Default()
	if c.Defaults.Rounds != 3 || c.Defaults.Moderator != "majority" {
		t.Fatalf("bad defaults: %+v", c.Defaults)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"codex", "claude", "gemini", "agy"} {
		sp, err := c.Spec(n)
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		if sp.Timeout != 10*time.Minute {
			t.Errorf("%s timeout %v", n, sp.Timeout)
		}
	}
	if sp, _ := c.Spec("agy"); sp.Kind != harness.KindAntigravity || sp.Command != "agy" {
		t.Fatalf("agy spec: %+v", sp)
	}
}

func TestBuiltinNameWithoutDefinition(t *testing.T) {
	c, err := Parse([]byte("agents: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if sp, err := c.Spec("antigravity"); err != nil || sp.Command != "agy" {
		t.Fatalf("got %+v %v", sp, err)
	}
	if _, err := c.Spec("nope"); err == nil {
		t.Fatal("unknown agent should error")
	}
}

func TestOverridesAndGeneric(t *testing.T) {
	y := `
defaults:
  agents: [fast-claude, echo]
  rounds: 2
  moderator: fast-claude
  timeout: 90s
agents:
  fast-claude:
    harness: claude
    model: haiku
    extra_args: ["--max-turns", "2"]
    timeout: 30s
  echo:
    harness: generic
    command: cat
    prompt_mode: stdin
    output: text
    env: {A: b}
`
	c, err := Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	sp, _ := c.Spec("fast-claude")
	if sp.Model != "haiku" || sp.Timeout != 30*time.Second || sp.JSONPath != "result" || len(sp.ExtraArgs) != 2 {
		t.Fatalf("override failed: %+v", sp)
	}
	sp, _ = c.Spec("echo")
	if sp.Kind != harness.KindGeneric || sp.Command != "cat" || sp.Timeout != 90*time.Second || sp.Env["A"] != "b" {
		t.Fatalf("generic failed: %+v", sp)
	}
}

func TestValidationErrors(t *testing.T) {
	bad := map[string]string{
		"generic without command": "agents:\n  x:\n    harness: generic\n",
		"unknown harness":         "agents:\n  x:\n    harness: pigeon\n",
		"bad prompt mode":         "agents:\n  x:\n    harness: generic\n    command: c\n    prompt_mode: telepathy\n",
		"bad duration":            "defaults:\n  timeout: soon\n",
		"unknown field":           "agents:\n  x:\n    harness: claude\n    modle: typo\n",
		"bad moderator":           "defaults:\n  moderator: ghost\n",
	}
	for name, y := range bad {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestLoadMissingAndInit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "council", "config.yaml")
	c, err := Load(p)
	if err != nil || c.Path() != p || len(c.Agents) == 0 {
		t.Fatalf("missing file should load defaults: %v", err)
	}
	if err := WriteDefault(p, false); err != nil {
		t.Fatal(err)
	}
	if err := WriteDefault(p, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("second init should refuse: %v", err)
	}
	if err := WriteDefault(p, true); err != nil {
		t.Fatal(err)
	}
	c, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.AgentNames(); strings.Join(got, ",") != "agy,claude,codex,gemini" {
		t.Fatalf("agents: %v", got)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("COUNCIL_CONFIG", "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if got := DefaultPath(); got != filepath.Join(dir, "council", "config.yaml") {
		t.Fatalf("got %s", got)
	}
	os.MkdirAll(filepath.Join(dir, "council"), 0o755)
	os.WriteFile(filepath.Join(dir, "council", "config"), []byte("{}"), 0o644)
	if got := DefaultPath(); got != filepath.Join(dir, "council", "config") {
		t.Fatalf("extension-less config not honoured: %s", got)
	}
	t.Setenv("COUNCIL_CONFIG", "/x/y.yaml")
	if DefaultPath() != "/x/y.yaml" {
		t.Fatal("COUNCIL_CONFIG not honoured")
	}
}

func TestEmptyFile(t *testing.T) {
	c, err := Parse(nil)
	if err != nil || c.Defaults.Rounds != 3 {
		t.Fatalf("empty config should parse to defaults: %v", err)
	}
}
