// Package cli implements the council command-line interface.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/Parsa2820/council/internal/chat"
	"github.com/Parsa2820/council/internal/config"
	"github.com/Parsa2820/council/internal/harness"
)

// Version is set at build time with -ldflags "-X github.com/Parsa2820/council/internal/cli.Version=...".
var Version = "dev"

// Env bundles the process streams so the CLI can be tested in-process.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// StdinIsTTY reports whether stdin is an interactive terminal.
	StdinIsTTY bool
}

// errUsage marks errors that should exit with status 2.
type errUsage struct{ msg string }

func (e errUsage) Error() string { return e.msg }

const usage = `council - run several LLM coding CLIs as a group chat and return their consensus

Usage:
  council ask [flags] "prompt"       ask the council (prompt may also come from --file or stdin)
  council init [--force]             write a default config to ~/.config/council/config.yaml
  council doctor [--probe] [agents]  check each harness is installed and signed in (alias: status)
  council login <agent> [-- args]    run the harness CLI's own login flow
  council agents                     list configured agents and how they are invoked
  council version

Run "council <command> -h" for command flags.
`

// Main runs the CLI and returns the process exit code.
func Main(args []string, env Env) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := run(ctx, args, env)
	if err == nil {
		return 0
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	var ec exitCode
	if errors.As(err, &ec) {
		return int(ec)
	}
	fmt.Fprintln(env.Stderr, "council:", err)
	var u errUsage
	if errors.As(err, &u) {
		return 2
	}
	return 1
}

type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func run(ctx context.Context, args []string, env Env) error {
	if len(args) == 0 {
		fmt.Fprint(env.Stderr, usage)
		return errUsage{"no command given"}
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "ask":
		return cmdAsk(ctx, rest, env)
	case "init":
		return cmdInit(rest, env)
	case "doctor", "status":
		return cmdDoctor(ctx, rest, env)
	case "login":
		return cmdLogin(ctx, rest, env)
	case "agents":
		return cmdAgents(rest, env)
	case "version", "--version", "-v":
		fmt.Fprintln(env.Stdout, "council", Version)
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(env.Stdout, usage)
		return nil
	}
	fmt.Fprint(env.Stderr, usage)
	return errUsage{fmt.Sprintf("unknown command %q", cmd)}
}

// parseInterspersed lets flags appear before or after positional arguments.
// Everything after a literal "--" is positional.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		// flag.Parse consumes a "--" terminator; detect it just before rest.
		if i := len(args) - len(rest) - 1; i >= 0 && args[i] == "--" {
			return append(pos, rest...), nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func splitDashDash(args []string) (before, after []string, found bool) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:], true
		}
	}
	return args, nil, false
}

func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		path = config.DefaultPath()
	}
	return config.Load(path)
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---- ask --------------------------------------------------------------------

func cmdAsk(ctx context.Context, args []string, env Env) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	var (
		cfgPath    = fs.String("config", "", "config file (default ~/.config/council/config.yaml)")
		agentsFlag = fs.String("agents", "", "comma-separated agents (default from config, e.g. codex,claude,gemini)")
		rounds     = fs.Int("rounds", 0, "maximum discussion rounds (default from config)")
		moderator  = fs.String("moderator", "", `agent that synthesizes the final answer, or "majority"`)
		file       = fs.String("file", "", "read the prompt (or extra context) from this file")
		asJSON     = fs.Bool("json", false, "print the full result as JSON")
		transcript = fs.String("transcript", "", "write the group chat transcript as Markdown to this file")
		verbose    = fs.Bool("verbose", false, "stream the group chat live to stderr")
		timeout    = fs.Duration("timeout", 0, "per-turn timeout for each agent (default from config)")
		loose      = fs.Bool("loose", false, "accept identical answers as consensus without explicit agree=true")
		maxChars   = fs.Int("max-context", -1, "truncate each earlier message shown to agents to N chars (0 = unlimited)")
	)
	fs.StringVar(file, "f", "", "shorthand for --file")
	fs.BoolVar(verbose, "v", false, "shorthand for --verbose")
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, `Usage: council ask [flags] "prompt"   (or: echo prompt | council ask, council ask -f prompt.md)`)
		fs.PrintDefaults()
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	question, err := readQuestion(pos, *file, env)
	if err != nil {
		return err
	}

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	names := cfg.Defaults.Agents
	if *agentsFlag != "" {
		names = splitList(*agentsFlag)
	}
	var agents []harness.Harness
	for _, n := range names {
		h, err := cfg.Harness(n)
		if err != nil {
			return errUsage{err.Error()}
		}
		agents = append(agents, h)
	}
	opts := chat.Options{
		Rounds:                   cfg.Defaults.Rounds,
		RequireExplicitAgreement: *cfg.Defaults.RequireExplicitAgreement && !*loose,
		MaxTranscriptChars:       cfg.Defaults.MaxTranscriptChars,
	}
	if *rounds > 0 {
		opts.Rounds = *rounds
	}
	if *maxChars >= 0 {
		opts.MaxTranscriptChars = *maxChars
	}
	if *timeout > 0 {
		opts.TurnTimeout = *timeout
	}
	mod := cfg.Defaults.Moderator
	if *moderator != "" {
		mod = *moderator
	}
	if mod != "" && mod != config.ModeratorMajority && mod != "vote" && mod != "none" {
		h, err := cfg.Harness(mod)
		if err != nil {
			return errUsage{"--moderator: " + err.Error()}
		}
		opts.Moderator = h
	}
	if *verbose {
		opts.OnEvent = livePrinter(env.Stderr)
	}

	council, err := chat.New(agents, opts)
	if err != nil {
		return errUsage{err.Error()}
	}
	res, runErr := council.Run(ctx, question)
	if res != nil && *transcript != "" {
		md := chat.NewTranscriptFrom(res).Markdown(resultOrNil(res, runErr))
		if err := os.WriteFile(*transcript, []byte(md), 0o644); err != nil {
			fmt.Fprintln(env.Stderr, "council: writing transcript:", err)
		} else if *verbose {
			fmt.Fprintln(env.Stderr, "transcript written to", *transcript)
		}
	}
	if runErr != nil {
		return runErr
	}
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	printResult(env, res)
	return nil
}

func resultOrNil(res *chat.Result, err error) *chat.Result {
	if err != nil {
		return nil
	}
	return res
}

func readQuestion(pos []string, file string, env Env) (string, error) {
	var parts []string
	useStdin := false
	for _, p := range pos {
		if p == "-" {
			useStdin = true
			continue
		}
		parts = append(parts, p)
	}
	q := strings.Join(parts, " ")
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		q = joinNonEmpty(q, string(b))
	}
	if useStdin || (q == "" && !env.StdinIsTTY && env.Stdin != nil) {
		b, err := io.ReadAll(env.Stdin)
		if err != nil {
			return "", err
		}
		q = joinNonEmpty(q, string(b))
	}
	if strings.TrimSpace(q) == "" {
		return "", errUsage{`no prompt given: use council ask "prompt", --file, or pipe it on stdin`}
	}
	return q, nil
}

func joinNonEmpty(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "\n\n" + b
}

func printResult(env Env, res *chat.Result) {
	out := env.Stdout
	if res.Details != "" && strings.TrimSpace(res.Details) != strings.TrimSpace(res.Answer) {
		fmt.Fprintln(out, strings.TrimSpace(res.Details))
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Final answer:", res.Answer)
	} else {
		fmt.Fprintln(out, res.Answer)
	}
	status := "no full consensus"
	if res.Converged {
		status = "consensus"
	}
	line := fmt.Sprintf("── %s after %d round(s) · method: %s", status, res.Rounds, res.Method)
	if res.Moderator != "" {
		line += " (" + res.Moderator + ")"
	}
	if len(res.Supporters) > 0 {
		line += " · for: " + strings.Join(res.Supporters, ", ")
	}
	if len(res.Dissenters) > 0 {
		line += " · against: " + strings.Join(res.Dissenters, ", ")
	}
	fmt.Fprintln(env.Stderr, line)
	for _, n := range res.Notes {
		fmt.Fprintln(env.Stderr, "note:", n)
	}
}

func livePrinter(w io.Writer) func(chat.Event) {
	return func(e chat.Event) {
		switch e.Kind {
		case chat.EventRoundStart:
			fmt.Fprintf(w, "\n━━━━━━━━ Round %d ━━━━━━━━\n", e.Round)
		case chat.EventAgentMessage, chat.EventModerator:
			m := e.Message
			label := m.Agent
			if m.Role == chat.RoleModerator {
				label = "moderator " + m.Agent
			}
			fmt.Fprintf(w, "\n▶ %s (%s)\n", label, m.Duration.Round(time.Millisecond))
			if !m.OK() {
				fmt.Fprintf(w, "  ✗ error: %s\n", m.Error)
				return
			}
			fmt.Fprintln(w, indent(strings.TrimSpace(m.Content), "  "))
			if m.Vote == nil {
				fmt.Fprintln(w, "  (no vote block found: counted as abstention)")
			}
		case chat.EventConsensus:
			fmt.Fprintf(w, "\n✔ consensus reached in round %d: %s\n", e.Round, e.Text)
		case chat.EventNoConsensus:
			fmt.Fprintf(w, "\n✖ no consensus after %d round(s); deciding by synthesis/vote\n", e.Round)
		case chat.EventModeratorStart:
			fmt.Fprintf(w, "\n━━━━━━━━ Moderator synthesis ━━━━━━━━\n")
		}
	}
}

func indent(s, p string) string {
	return p + strings.ReplaceAll(s, "\n", "\n"+p)
}

// ---- init -------------------------------------------------------------------

func cmdInit(args []string, env Env) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	cfgPath := fs.String("config", "", "where to write the config (default ~/.config/council/config.yaml)")
	force := fs.Bool("force", false, "overwrite an existing config")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	p := *cfgPath
	if p == "" {
		p = config.DefaultPath()
	}
	if err := config.WriteDefault(p, *force); err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, "wrote", p)
	return nil
}

// ---- doctor -----------------------------------------------------------------

func cmdDoctor(ctx context.Context, args []string, env Env) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	cfgPath := fs.String("config", "", "config file")
	probe := fs.Bool("probe", false, "also send a tiny prompt to each installed harness to verify it really works")
	asJSON := fs.Bool("json", false, "print results as JSON")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	names := pos
	if len(names) == 0 {
		names = cfg.AgentNames()
		for _, d := range cfg.Defaults.Agents {
			if _, ok := cfg.Agents[d]; !ok {
				names = append(names, d)
			}
		}
	}
	type row struct {
		harness.Status
		Probe string `json:"probe,omitempty"`
	}
	var rows []row
	bad := false
	for _, n := range names {
		h, err := cfg.Harness(n)
		if err != nil {
			return errUsage{err.Error()}
		}
		r := row{Status: h.Check(ctx)}
		if *probe && r.Installed {
			pctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			out, err := h.Run(pctx, "Reply with exactly the word OK and nothing else.")
			cancel()
			if err != nil {
				r.Probe = "FAIL: " + oneLine(err.Error(), 160)
				r.Auth = harness.AuthMissing
			} else {
				r.Probe = "ok: " + oneLine(out, 40)
				if r.Auth == harness.AuthUnknown {
					r.Auth = harness.AuthOK
				}
			}
		}
		if !r.Installed || r.Auth == harness.AuthMissing {
			bad = true
		}
		rows = append(rows, r)
	}
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(env.Stdout, "config: %s\n\n", cfg.Path())
		tw := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "AGENT\tKIND\tCOMMAND\tINSTALLED\tAUTH\tVERSION\tDETAIL")
		for _, r := range rows {
			inst := "no"
			if r.Installed {
				inst = "yes"
			}
			detail := r.Detail
			if r.Probe != "" {
				detail = "probe " + r.Probe + "; " + detail
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Agent, r.Kind, r.Command, inst, r.Auth, dash(r.Version), oneLine(detail, 120))
		}
		tw.Flush()
		if bad {
			fmt.Fprintln(env.Stdout, "\nSome harnesses are not ready. Install the CLI, then run `council login <agent>`.")
		}
	}
	if bad {
		return exitCode(1)
	}
	return nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}

// ---- login ------------------------------------------------------------------

func cmdLogin(ctx context.Context, args []string, env Env) error {
	before, extra, _ := splitDashDash(args)
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	cfgPath := fs.String("config", "", "config file")
	pos, err := parseInterspersed(fs, before)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errUsage{"usage: council login <agent> [-- extra args for the CLI's login command]"}
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	h, err := cfg.Harness(pos[0])
	if err != nil {
		return errUsage{err.Error()}
	}
	argv, note := h.LoginCommand()
	if argv == nil {
		return fmt.Errorf("agent %q has no login command; set login_args in the config or sign in with the CLI directly", pos[0])
	}
	argv = append(argv, extra...)
	if _, err := exec.LookPath(argv[0]); err != nil {
		return fmt.Errorf("%s is not installed or not on PATH", argv[0])
	}
	if note != "" {
		fmt.Fprintln(env.Stderr, note)
	}
	fmt.Fprintf(env.Stderr, "running: %s\n", strings.Join(argv, " "))
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Stdin, c.Stdout, c.Stderr = env.Stdin, env.Stdout, env.Stderr
	return c.Run()
}

// ---- agents -----------------------------------------------------------------

func cmdAgents(args []string, env Env) error {
	fs := flag.NewFlagSet("agents", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	cfgPath := fs.String("config", "", "config file")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "config: %s\ndefault agents: %s · rounds: %d · moderator: %s\n\n",
		cfg.Path(), strings.Join(cfg.Defaults.Agents, ","), cfg.Defaults.Rounds, cfg.Defaults.Moderator)
	tw := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "AGENT\tKIND\tPROMPT\tOUTPUT\tCOMMAND LINE")
	for _, n := range cfg.AgentNames() {
		h, err := cfg.Harness(n)
		if err != nil {
			return err
		}
		sp := h.Spec()
		line := strings.Join(append([]string{sp.Command}, h.BuildArgs("<prompt>", "<prompt_file>", "<output_file>")...), " ")
		out := string(sp.Output)
		if sp.JSONPath != "" {
			out += ":" + sp.JSONPath
		}
		if sp.OutputSource == harness.SourceFile {
			out += " (file)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", n, sp.Kind, sp.PromptMode, out, line)
	}
	return tw.Flush()
}
