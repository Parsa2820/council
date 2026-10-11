package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Parsa2820/council/internal/chat"
)

// fakeConfig writes a config whose agents are examples/fake-agent.sh run
// through the generic harness: an end-to-end test without any real LLM CLI.
func fakeConfig(t *testing.T, extra string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake agent is a POSIX shell script")
	}
	script, err := filepath.Abs("../../examples/fake-agent.sh")
	if err != nil {
		t.Fatal(err)
	}
	cfg := `
defaults:
  agents: [alpha, beta, gamma]
  rounds: 3
  timeout: 20s
agents:
  alpha: {harness: generic, command: SCRIPT, args: [alpha, "42", follow], prompt_mode: stdin}
  beta:  {harness: generic, command: SCRIPT, args: [beta, "42", stubborn], prompt_mode: stdin}
  gamma: {harness: generic, command: SCRIPT, args: [gamma, "41", follow], prompt_mode: stdin}
  delta: {harness: generic, command: SCRIPT, args: [delta, "7", stubborn], prompt_mode: stdin}
  judge: {harness: generic, command: SCRIPT, args: [judge, "n/a"], prompt_mode: stdin}
  broken: {harness: generic, command: /definitely/not/here, prompt_mode: stdin}
  fakeclaude:
    harness: generic
    command: SCRIPT
    args: [fc, "1"]
    status_args: ["--status-probe"]
    login_args: ["login"]
` + extra
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(strings.ReplaceAll(cfg, "SCRIPT", script)), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runCLI(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var o, e bytes.Buffer
	env := Env{Stdin: strings.NewReader(stdin), Stdout: &o, Stderr: &e, StdinIsTTY: stdin == ""}
	code = Main(args, env)
	return o.String(), e.String(), code
}

func TestAskEndToEndConsensus(t *testing.T) {
	cfg := fakeConfig(t, "")
	tr := filepath.Join(t.TempDir(), "chat.md")
	out, errOut, code := runCLI(t, "", "ask", "--config", cfg, "What is 6*7?", "--transcript", tr, "--verbose")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if !strings.Contains(out, "Final answer: 42") {
		t.Fatalf("stdout:\n%s", out)
	}
	for _, want := range []string{"Round 1", "Round 2", "consensus reached in round 2: 42", "method: unanimous"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("verbose output missing %q:\n%s", want, errOut)
		}
	}
	md, err := os.ReadFile(tr)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Council transcript", "## Round 2", "### gamma", "- **Answer:** 42", "- **Converged:** true after 2 round(s)"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("transcript missing %q", want)
		}
	}
}

func TestAskJSONStdinAndModerator(t *testing.T) {
	cfg := fakeConfig(t, "")
	out, errOut, code := runCLI(t, "What is 6*7?\n", "ask", "--config", cfg, "--json", "--agents", "alpha,delta", "--rounds", "2", "--moderator", "judge")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	var res chat.Result
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	// alpha follows (alphabetical tie-break picks "42"), delta insists on 7:
	// no consensus, so the judge moderates.
	if res.Converged || res.Rounds != 2 || res.Method != "moderator" || res.Moderator != "judge" {
		t.Fatalf("unexpected: %+v", res)
	}
	if res.Question != "What is 6*7?" || res.Answer == "" || len(res.Votes) != 2 {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestAskFileFlagAndBrokenAgent(t *testing.T) {
	cfg := fakeConfig(t, "")
	q := filepath.Join(t.TempDir(), "q.md")
	os.WriteFile(q, []byte("What is 6*7?"), 0o644)
	out, errOut, code := runCLI(t, "", "ask", "--config", cfg, "-f", q, "--agents", "alpha,beta,broken")
	if code != 0 || !strings.Contains(out, "42") {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, out, errOut)
	}
}

func TestAskUsageErrors(t *testing.T) {
	cfg := fakeConfig(t, "")
	if _, _, code := runCLI(t, "", "ask", "--config", cfg); code != 2 {
		t.Fatalf("missing prompt: exit %d", code)
	}
	if _, e, code := runCLI(t, "", "ask", "--config", cfg, "--agents", "nobody", "q"); code != 2 || !strings.Contains(e, "unknown agent") {
		t.Fatalf("unknown agent: exit %d %s", code, e)
	}
	if _, _, code := runCLI(t, "", "frobnicate"); code != 2 {
		t.Fatalf("unknown command: exit %d", code)
	}
}

func TestDoctor(t *testing.T) {
	cfg := fakeConfig(t, "")
	out, _, code := runCLI(t, "", "doctor", "--config", cfg, "alpha", "broken")
	if code != 1 {
		t.Fatalf("broken agent should make doctor exit 1, got %d", code)
	}
	if !strings.Contains(out, "broken") || !strings.Contains(out, "not found on PATH") {
		t.Fatalf("doctor output:\n%s", out)
	}
	out, _, code = runCLI(t, "", "status", "--config", cfg, "--json", "--probe", "alpha")
	if code != 0 {
		t.Fatalf("status exit %d: %s", code, out)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 || rows[0]["auth"] != "ok" {
		t.Fatalf("probe should mark auth ok: %v %s", err, out)
	}
}

func TestInitAndAgents(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c", "config.yaml")
	if out, e, code := runCLI(t, "", "init", "--config", p); code != 0 || !strings.Contains(out, p) {
		t.Fatalf("init: %d %s %s", code, out, e)
	}
	if _, _, code := runCLI(t, "", "init", "--config", p); code == 0 {
		t.Fatal("second init without --force should fail")
	}
	out, _, code := runCLI(t, "", "agents", "--config", p)
	if code != 0 || !strings.Contains(out, "codex exec --skip-git-repo-check") || !strings.Contains(out, "agy --input-format stream-json") {
		t.Fatalf("agents output:\n%s", out)
	}
}

func TestLoginErrors(t *testing.T) {
	cfg := fakeConfig(t, "")
	if _, e, code := runCLI(t, "", "login", "--config", cfg, "alpha"); code != 1 || !strings.Contains(e, "no login command") {
		t.Fatalf("login without login_args: %d %s", code, e)
	}
	if _, _, code := runCLI(t, "", "login", "--config", cfg); code != 2 {
		t.Fatalf("login without agent should be usage error, got %d", code)
	}
}

func TestParseInterspersed(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	n := fs.Int("n", 0, "")
	v := fs.Bool("v", false, "")
	pos, err := parseInterspersed(fs, []string{"hello", "-n", "3", "world", "-v", "--", "-x", "y"})
	if err != nil {
		t.Fatal(err)
	}
	if *n != 3 || !*v || !reflect.DeepEqual(pos, []string{"hello", "world", "-x", "y"}) {
		t.Fatalf("n=%d v=%v pos=%q", *n, *v, pos)
	}
}

func TestVersion(t *testing.T) {
	out, _, code := runCLI(t, "", "version")
	if code != 0 || !strings.HasPrefix(out, "council ") {
		t.Fatalf("%d %q", code, out)
	}
}
