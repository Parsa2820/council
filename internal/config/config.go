package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Parsa2820/council/internal/harness"
)

// Duration is a time.Duration that unmarshals from strings like "90s" or "10m".
type Duration time.Duration

// UnmarshalYAML parses a Go duration string; "" and "0" mean zero.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	if s == "" || s == "0" {
		*d = 0
		return nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("line %d: invalid duration %q: %w", n.Line, s, err)
	}
	*d = Duration(v)
	return nil
}

// MarshalYAML renders the duration as a Go duration string such as "10m0s".
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// Config is the root of the config file.
type Config struct {
	Defaults Defaults         `yaml:"defaults"`
	Agents   map[string]Agent `yaml:"agents"`

	path string
}

// Defaults are used when the matching `council ask` flag is not given.
type Defaults struct {
	Agents    []string `yaml:"agents"`
	Rounds    int      `yaml:"rounds"`
	Moderator string   `yaml:"moderator"`
	Timeout   Duration `yaml:"timeout"`
	// RequireExplicitAgreement: after round 1, consensus also needs every agent
	// to set "agree": true (not just name the same answer). Default true.
	RequireExplicitAgreement *bool `yaml:"require_explicit_agreement"`
	// MaxTranscriptChars truncates each earlier message shown to agents (0 = no limit).
	MaxTranscriptChars int `yaml:"max_transcript_chars"`
}

// Agent configures one participant. Harness selects a built-in preset
// (codex, claude, gemini, antigravity/agy) or "generic"; every other field
// overrides the preset.
type Agent struct {
	Harness       string            `yaml:"harness"`
	Command       string            `yaml:"command,omitempty"`
	Args          []string          `yaml:"args,omitempty"`
	ExtraArgs     []string          `yaml:"extra_args,omitempty"`
	Model         string            `yaml:"model,omitempty"`
	ModelFlag     string            `yaml:"model_flag,omitempty"`
	PromptMode    string            `yaml:"prompt_mode,omitempty"`
	StdinTemplate string            `yaml:"stdin_template,omitempty"`
	Output        string            `yaml:"output,omitempty"`
	OutputSource  string            `yaml:"output_source,omitempty"`
	JSONPath      string            `yaml:"json_path,omitempty"`
	ErrorPath     string            `yaml:"error_path,omitempty"`
	Timeout       Duration          `yaml:"timeout,omitempty"`
	Env           map[string]string `yaml:"env,omitempty"`
	Workdir       string            `yaml:"workdir,omitempty"`
	LoginArgs     []string          `yaml:"login_args,omitempty"`
	StatusArgs    []string          `yaml:"status_args,omitempty"`
}

// ModeratorMajority selects a plain majority vote instead of a moderator agent.
const ModeratorMajority = "majority"

// DefaultPath returns the config path: $COUNCIL_CONFIG, else
// $XDG_CONFIG_HOME/council/config.yaml, else ~/.config/council/config.yaml.
// An existing extension-less ~/.config/council/config is also honoured.
func DefaultPath() string {
	if p := os.Getenv("COUNCIL_CONFIG"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		h, _ := os.UserHomeDir()
		base = filepath.Join(h, ".config")
	}
	dir := filepath.Join(base, "council")
	for _, name := range []string{"config.yaml", "config.yml", "config"} {
		if p := filepath.Join(dir, name); fileExists(p) {
			return p
		}
	}
	return filepath.Join(dir, "config.yaml")
}

func fileExists(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }

// Default returns the built-in configuration used when no file exists.
func Default() *Config {
	var c Config
	if err := yaml.Unmarshal([]byte(DefaultYAML), &c); err != nil {
		panic("config: bad DefaultYAML: " + err.Error())
	}
	c.applyDefaults()
	return &c
}

// Load reads path. A missing file yields Default() (with Path() == path).
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c := Default()
		c.path = path
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.path = path
	return c, nil
}

// Parse decodes and validates YAML config bytes.
func Parse(b []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Path is where the config was loaded from (may not exist).
func (c *Config) Path() string { return c.path }

func (c *Config) applyDefaults() {
	if c.Agents == nil {
		c.Agents = map[string]Agent{}
	}
	if c.Defaults.Rounds == 0 {
		c.Defaults.Rounds = 3
	}
	if c.Defaults.Moderator == "" {
		c.Defaults.Moderator = ModeratorMajority
	}
	if c.Defaults.Timeout == 0 {
		c.Defaults.Timeout = Duration(10 * time.Minute)
	}
	if c.Defaults.RequireExplicitAgreement == nil {
		t := true
		c.Defaults.RequireExplicitAgreement = &t
	}
	if len(c.Defaults.Agents) == 0 {
		c.Defaults.Agents = []string{"codex", "claude", "gemini"}
	}
}

// Validate checks agent definitions and defaults.
func (c *Config) Validate() error {
	if c.Defaults.Rounds < 1 {
		return fmt.Errorf("defaults.rounds must be >= 1")
	}
	for _, name := range c.AgentNames() {
		if _, err := c.Spec(name); err != nil {
			return err
		}
	}
	if m := c.Defaults.Moderator; m != ModeratorMajority {
		if _, err := c.Spec(m); err != nil {
			return fmt.Errorf("defaults.moderator: %w", err)
		}
	}
	return nil
}

// AgentNames returns configured agent names, sorted.
func (c *Config) AgentNames() []string {
	names := make([]string, 0, len(c.Agents))
	for n := range c.Agents {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Spec resolves an agent name to a harness Spec. Names that are not defined
// in the config but match a built-in harness kind (codex, claude, gemini,
// antigravity, agy) resolve to that preset.
func (c *Config) Spec(name string) (harness.Spec, error) {
	a, ok := c.Agents[name]
	if !ok {
		if _, isBuiltin := harness.Preset(name); !isBuiltin {
			return harness.Spec{}, fmt.Errorf("unknown agent %q (define it under `agents:` in %s)", name, c.displayPath())
		}
		a = Agent{Harness: name}
	}
	kind := harness.CanonicalKind(a.Harness)
	if kind == "" {
		if _, isBuiltin := harness.Preset(name); isBuiltin {
			kind = harness.CanonicalKind(name)
		} else {
			kind = harness.KindGeneric
		}
	}
	sp, isBuiltin := harness.Preset(kind)
	if !isBuiltin {
		if kind != harness.KindGeneric {
			return harness.Spec{}, fmt.Errorf("agent %q: unknown harness %q (want one of %s or generic)", name, a.Harness, strings.Join(harness.BuiltinKinds(), ", "))
		}
		if a.Command == "" {
			return harness.Spec{}, fmt.Errorf("agent %q: generic harness needs `command`", name)
		}
		sp = harness.Spec{Kind: harness.KindGeneric}
	}
	if a.Command != "" {
		sp.Command = os.ExpandEnv(expandHome(a.Command))
	}
	if a.Args != nil {
		sp.Args = a.Args
	}
	if a.ExtraArgs != nil {
		sp.ExtraArgs = a.ExtraArgs
	}
	setStr(&sp.Model, a.Model)
	setStr(&sp.ModelFlag, a.ModelFlag)
	setStr(&sp.StdinTemplate, a.StdinTemplate)
	setStr(&sp.JSONPath, a.JSONPath)
	setStr(&sp.ErrorPath, a.ErrorPath)
	setStr(&sp.Dir, expandHome(a.Workdir))
	if a.PromptMode != "" {
		sp.PromptMode = harness.PromptMode(a.PromptMode)
	}
	if a.Output != "" {
		sp.Output = harness.OutputFormat(a.Output)
	}
	if a.OutputSource != "" {
		sp.OutputSource = harness.OutputSource(a.OutputSource)
	}
	switch {
	case a.Timeout > 0:
		sp.Timeout = time.Duration(a.Timeout)
	case c.Defaults.Timeout > 0:
		sp.Timeout = time.Duration(c.Defaults.Timeout)
	}
	if len(a.Env) > 0 {
		sp.Env = a.Env
	}
	if a.LoginArgs != nil {
		sp.LoginArgs = a.LoginArgs
	}
	if a.StatusArgs != nil {
		sp.StatusArgs = a.StatusArgs
	}
	if _, err := harness.New(name, sp); err != nil {
		return harness.Spec{}, err
	}
	return sp, nil
}

// Harness builds the runnable harness for an agent name.
func (c *Config) Harness(name string) (*harness.CommandHarness, error) {
	sp, err := c.Spec(name)
	if err != nil {
		return nil, err
	}
	return harness.New(name, sp)
}

func (c *Config) displayPath() string {
	if c.path == "" {
		return "the config file"
	}
	return c.path
}

func setStr(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, strings.TrimPrefix(p, "~"))
	}
	return p
}

// WriteDefault writes DefaultYAML to path, refusing to overwrite unless force.
func WriteDefault(path string, force bool) error {
	if fileExists(path) && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(DefaultYAML), 0o644)
}
