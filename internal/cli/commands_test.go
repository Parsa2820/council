package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Parsa2820/council/internal/chat"
	"github.com/Parsa2820/council/internal/consensus"
)

// fakePresetCLIs puts the harness package's fake codex, claude, agy and
// opencode scripts first on PATH and isolates every credential source.
func fakePresetCLIs(t *testing.T) (argsFile string) {
	t.Helper()
	needSh(t)
	bin := t.TempDir()
	for _, name := range []string{"codex", "claude", "agy", "opencode"} {
		b, err := os.ReadFile(filepath.Join("..", "harness", "testdata", "fakecli", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, name), b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"GEMINI_API_KEY", "FAKE_STDIN", "FAKE_CHILD"} {
		t.Setenv(k, "")
	}
	argsFile = filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_ARGS", argsFile)
	t.Setenv("FAKE_MODE", "ok")
	t.Setenv("FAKE_AUTH", "ok")
	return argsFile
}

func needSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLIs are POSIX shell scripts")
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunDispatch(t *testing.T) {
	cases := []struct {
		args               []string
		code               int
		wantOut, wantErr   string
		noStdout, noStderr bool
	}{
		{[]string{"help"}, 0, "Usage:\n  council ask", "", false, true},
		{[]string{"-h"}, 0, "council init [--force]", "", false, true},
		{[]string{"--help"}, 0, "council login <agent>", "", false, true},
		{[]string{"version"}, 0, "council dev\n", "", false, true},
		{[]string{"--version"}, 0, "council dev\n", "", false, true},
		{[]string{"-v"}, 0, "council dev\n", "", false, true},
		{nil, 2, "", "Usage:", true, false},
		{[]string{"launch"}, 2, "", `council: unknown command "launch"`, true, false},
		{[]string{"ask", "-h"}, 0, "", "Usage: council ask [flags]", true, false},
		{[]string{"init", "-h"}, 0, "", "-force", true, false},
		{[]string{"doctor", "-h"}, 0, "", "-probe", true, false},
		{[]string{"login", "-h"}, 0, "", "-config", true, false},
		{[]string{"agents", "-h"}, 0, "", "-config", true, false},
		{[]string{"ask", "--rounds", "many", "q"}, 1, "", `invalid value "many" for flag -rounds`, true, false},
		{[]string{"init", "--bogus"}, 1, "", "flag provided but not defined: -bogus", true, false},
		{[]string{"doctor", "--bogus"}, 1, "", "flag provided but not defined", true, false},
		{[]string{"login", "--bogus", "codex"}, 1, "", "flag provided but not defined", true, false},
		{[]string{"agents", "--bogus"}, 1, "", "flag provided but not defined", true, false},
	}
	for _, c := range cases {
		out, errOut, code := runCLI(t, "", c.args...)
		if code != c.code || !strings.Contains(out, c.wantOut) || !strings.Contains(errOut, c.wantErr) {
			t.Errorf("%q: exit %d (want %d)\nstdout %q\nstderr %q", c.args, code, c.code, out, errOut)
		}
		if (c.noStdout && out != "") || (c.noStderr && errOut != "") {
			t.Errorf("%q: unexpected output: stdout %q stderr %q", c.args, out, errOut)
		}
	}
}

func TestMainExitCodes(t *testing.T) {
	var o, e bytes.Buffer
	env := Env{Stdout: &o, Stderr: &e, StdinIsTTY: true}
	cases := []struct {
		err     error
		code    int
		printed bool
	}{
		{exitCode(3), 3, false},
		{errUsage{"bad"}, 2, true},
		{errors.New("boom"), 1, true},
	}
	for _, c := range cases {
		e.Reset()
		if got := exitFor(c.err, env); got != c.code || (e.Len() > 0) != c.printed {
			t.Errorf("%v: code %d stderr %q", c.err, got, e.String())
		}
	}
	if exitCode(4).Error() != "exit status 4" || (errUsage{"m"}).Error() != "m" {
		t.Error("error strings")
	}
}

func TestLoadConfigDefaultPath(t *testing.T) {
	p := writeConfig(t, "defaults: {rounds: 9}\n")
	t.Setenv("COUNCIL_CONFIG", p)
	c, err := loadConfig("")
	if err != nil || c.Path() != p || c.Defaults.Rounds != 9 {
		t.Fatalf("got %+v %v", c, err)
	}
	if _, err := loadConfig(writeConfig(t, "defaults: [\n")); err == nil {
		t.Fatal("invalid YAML should fail")
	}
}

func TestSplitHelpers(t *testing.T) {
	for in, want := range map[string][]string{
		"":                 nil,
		"a":                {"a"},
		"a,b c":            {"a", "b", "c"},
		" ,a,, b ,":        {"a", "b"},
		"codex, claude,gm": {"codex", "claude", "gm"},
	} {
		if got := splitList(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitList(%q) = %q", in, got)
		}
	}
	cases := []struct {
		in            []string
		before, after []string
		found         bool
	}{
		{nil, nil, nil, false},
		{[]string{"a", "b"}, []string{"a", "b"}, nil, false},
		{[]string{"a", "--", "b", "--", "c"}, []string{"a"}, []string{"b", "--", "c"}, true},
		{[]string{"--"}, []string{}, []string{}, true},
	}
	for _, c := range cases {
		b, a, f := splitDashDash(c.in)
		if f != c.found || len(b) != len(c.before) || len(a) != len(c.after) || (len(b) > 0 && !reflect.DeepEqual(b, c.before)) || (len(a) > 0 && !reflect.DeepEqual(a, c.after)) {
			t.Errorf("splitDashDash(%q) = %q %q %v", c.in, b, a, f)
		}
	}
	for _, c := range []struct{ a, b, want string }{
		{"", "", ""}, {" a ", "", "a"}, {"", " b ", "b"}, {"a", "b", "a\n\nb"},
	} {
		if got := joinNonEmpty(c.a, c.b); got != c.want {
			t.Errorf("joinNonEmpty(%q, %q) = %q", c.a, c.b, got)
		}
	}
	if dash("") != "-" || dash("x") != "x" {
		t.Error("dash")
	}
	if got := indent("a\nb", "  "); got != "  a\n  b" {
		t.Errorf("indent = %q", got)
	}
}

func TestParseInterspersedTable(t *testing.T) {
	cases := []struct {
		args    []string
		pos     []string
		n       int
		wantErr bool
	}{
		{nil, nil, 0, false},
		{[]string{"a", "b"}, []string{"a", "b"}, 0, false},
		{[]string{"-n", "2", "a"}, []string{"a"}, 2, false},
		{[]string{"a", "-n=3"}, []string{"a"}, 3, false},
		{[]string{"--", "-n", "4"}, []string{"-n", "4"}, 0, false},
		{[]string{"a", "--", "-n"}, []string{"a", "-n"}, 0, false},
		{[]string{"-", "x"}, []string{"-", "x"}, 0, false},
		{[]string{"-n"}, nil, 0, true},
		{[]string{"a", "-x"}, nil, 0, true},
	}
	for _, c := range cases {
		fs := newTestFlagSet()
		n := fs.Int("n", 0, "")
		pos, err := parseInterspersed(fs, c.args)
		if (err != nil) != c.wantErr || (!c.wantErr && (!reflect.DeepEqual(pos, c.pos) || *n != c.n)) {
			t.Errorf("parseInterspersed(%q) = %q n=%d err=%v", c.args, pos, *n, err)
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("stdin broke") }

func TestReadQuestion(t *testing.T) {
	file := filepath.Join(t.TempDir(), "q.md")
	if err := os.WriteFile(file, []byte("  from file \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		pos     []string
		file    string
		stdin   string
		tty     bool
		want    string
		wantErr string
	}{
		{"args joined", []string{"what", "is", "this?"}, "", "ignored", false, "what is this?", ""},
		{"file only", nil, file, "", true, "from file", ""},
		{"args then file", []string{"Review:"}, file, "", true, "Review:\n\nfrom file", ""},
		{"piped stdin", nil, "", "piped\n", false, "piped", ""},
		{"tty stdin is not read", nil, "", "typed", true, "", "no prompt given"},
		{"dash reads stdin even on a tty", []string{"Explain", "-"}, "", "code\n", true, "Explain\n\ncode", ""},
		{"file plus dash", []string{"-"}, file, "more", true, "from file\n\nmore", ""},
		{"blank everything", []string{" "}, "", "  \n", false, "", "no prompt given"},
		{"missing file", nil, filepath.Join(t.TempDir(), "nope.md"), "", true, "", "nope.md"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := readQuestion(c.pos, c.file, Env{Stdin: strings.NewReader(c.stdin), StdinIsTTY: c.tty})
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
	if _, err := readQuestion([]string{"-"}, "", Env{Stdin: failingReader{}}); err == nil || err.Error() != "stdin broke" {
		t.Errorf("stdin read error: %v", err)
	}
	if _, err := readQuestion(nil, "", Env{Stdin: nil}); err == nil {
		t.Error("nil stdin and no prompt should be a usage error")
	}
	var u errUsage
	if _, err := readQuestion(nil, "", Env{StdinIsTTY: true}); !errors.As(err, &u) {
		t.Errorf("no prompt should be errUsage, got %T", err)
	}
}

func TestPrintResult(t *testing.T) {
	cases := []struct {
		name            string
		res             chat.Result
		wantOut, wantSt string
		extra           []string
	}{
		{"answer only", chat.Result{Answer: "42", Method: consensus.MethodUnanimous, Converged: true, Rounds: 1, Supporters: []string{"a", "b"}},
			"42\n", "── consensus after 1 round(s) · method: unanimous · for: a, b\n", nil},
		{"details equal to answer", chat.Result{Answer: "42", Details: " 42 ", Method: consensus.MethodMajority, Rounds: 3},
			"42\n", "── no full consensus after 3 round(s) · method: majority\n", nil},
		{"details and moderator", chat.Result{Answer: "tabs", Details: "Long synthesis.\n", Method: consensus.MethodModerator, Rounds: 2, Moderator: "judge", Supporters: []string{"a"}, Dissenters: []string{"b"}, Notes: []string{"n1", "n2"}},
			"Long synthesis.\n\nFinal answer: tabs\n", "── no full consensus after 2 round(s) · method: moderator (judge) · for: a · against: b\nnote: n1\nnote: n2\n", nil},
	}
	for _, c := range cases {
		var o, e bytes.Buffer
		printResult(Env{Stdout: &o, Stderr: &e}, &c.res)
		if o.String() != c.wantOut || e.String() != c.wantSt {
			t.Errorf("%s:\nstdout %q\nstderr %q", c.name, o.String(), e.String())
		}
	}
}

func TestLivePrinter(t *testing.T) {
	var buf bytes.Buffer
	p := livePrinter(&buf)
	vote := &consensus.Vote{FinalAnswer: "x"}
	events := []chat.Event{
		{Kind: chat.EventRoundStart, Round: 2},
		{Kind: chat.EventAgentMessage, Round: 2, Message: &chat.Message{Agent: "a", Role: chat.RoleAgent, Content: "line1\nline2", Vote: vote, Duration: 1234 * time.Millisecond}},
		{Kind: chat.EventAgentMessage, Round: 2, Message: &chat.Message{Agent: "b", Role: chat.RoleAgent, Content: "musing"}},
		{Kind: chat.EventAgentMessage, Round: 2, Message: &chat.Message{Agent: "c", Role: chat.RoleAgent, Error: "c: timed out"}},
		{Kind: chat.EventConsensus, Round: 2, Text: "x"},
		{Kind: chat.EventNoConsensus, Round: 3},
		{Kind: chat.EventModeratorStart, Round: 3},
		{Kind: chat.EventModerator, Round: 4, Message: &chat.Message{Agent: "j", Role: chat.RoleModerator, Content: "final", Vote: vote}},
		{Kind: "unknown"},
	}
	for _, e := range events {
		p(e)
	}
	want := "\n━━━━━━━━ Round 2 ━━━━━━━━\n" +
		"\n▶ a (1.234s)\n  line1\n  line2\n" +
		"\n▶ b (0s)\n  musing\n  (no vote block found: counted as abstention)\n" +
		"\n▶ c (0s)\n  ✗ error: c: timed out\n" +
		"\n✔ consensus reached in round 2: x\n" +
		"\n✖ no consensus after 3 round(s); deciding by synthesis/vote\n" +
		"\n━━━━━━━━ Moderator synthesis ━━━━━━━━\n" +
		"\n▶ moderator j (0s)\n  final\n"
	if buf.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestAskOptionsAndErrors(t *testing.T) {
	cfg := fakeConfig(t, "")
	dir := t.TempDir()
	cases := []struct {
		name   string
		stdin  string
		args   []string
		code   int
		outHas string
		errHas []string
	}{
		{"majority moderator flag", "", []string{"--agents", "alpha,delta", "--rounds", "1", "--moderator", "majority", "q"}, 0, "42", []string{"method: majority", "for: alpha · against: delta"}},
		{"vote and none mean no moderator", "", []string{"--agents", "alpha,gamma", "--rounds", "1", "--moderator", "none", "q"}, 0, "Final answer: 42", []string{"method: majority"}},
		{"vote", "", []string{"--agents", "alpha,gamma", "--rounds", "1", "--moderator", "vote", "q"}, 0, "", []string{"method: majority"}},
		{"unknown moderator", "", []string{"--moderator", "nobody", "q"}, 2, "", []string{`--moderator: unknown agent "nobody"`}},
		{"duplicate agents", "", []string{"--agents", "alpha,alpha", "q"}, 2, "", []string{`duplicate agent "alpha"`}},
		{"every agent broken", "", []string{"--agents", "broken", "q"}, 1, "", []string{"round 1: every agent failed"}},
		{"loose", "", []string{"--agents", "alpha,beta", "--loose", "q"}, 0, "42", []string{"consensus after 1 round(s)"}},
		{"max-context and timeout", "", []string{"--agents", "alpha,beta", "--max-context", "10", "--timeout", "30s", "q"}, 0, "42", nil},
		{"file flag missing file", "", []string{"--file", filepath.Join(dir, "missing.md")}, 1, "", []string{"missing.md"}},
		{"transcript into missing dir", "", []string{"--agents", "alpha,beta", "--transcript", filepath.Join(dir, "no", "such", "t.md"), "q"}, 0, "42", []string{"council: writing transcript:"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, errOut, code := runCLI(t, c.stdin, append([]string{"ask", "--config", cfg}, c.args...)...)
			if code != c.code || !strings.Contains(out, c.outHas) {
				t.Fatalf("exit %d (want %d)\nstdout %q\nstderr %q", code, c.code, out, errOut)
			}
			for _, w := range c.errHas {
				if !strings.Contains(errOut, w) {
					t.Errorf("stderr missing %q:\n%s", w, errOut)
				}
			}
		})
	}
	if _, e, code := runCLI(t, "", "ask", "--config", writeConfig(t, "agents: {x: {harness: nope}}\n"), "q"); code != 1 || !strings.Contains(e, `unknown harness "nope"`) {
		t.Errorf("bad config: %d %s", code, e)
	}
}

func TestAskTranscriptOnFailure(t *testing.T) {
	cfg := fakeConfig(t, "")
	tr := filepath.Join(t.TempDir(), "t.md")
	_, errOut, code := runCLI(t, "", "ask", "--config", cfg, "--agents", "broken", "--transcript", tr, "-v", "q")
	if code != 1 || !strings.Contains(errOut, "transcript written to "+tr) {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	md, err := os.ReadFile(tr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "> **error:**") || strings.Contains(string(md), "## Result") {
		t.Fatalf("partial transcript should have the error and no result section:\n%s", md)
	}
	if resultOrNil(&chat.Result{}, errors.New("x")) != nil || resultOrNil(&chat.Result{}, nil) == nil {
		t.Error("resultOrNil")
	}
}

func TestInitDefaultPathAndForce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.yaml")
	t.Setenv("COUNCIL_CONFIG", p)
	if out, e, code := runCLI(t, "", "init"); code != 0 || out != "wrote "+p+"\n" {
		t.Fatalf("init: %d %q %q", code, out, e)
	}
	if _, e, code := runCLI(t, "", "init"); code != 1 || !strings.Contains(e, "already exists (use --force to overwrite)") {
		t.Fatalf("second init: %d %q", code, e)
	}
	if err := os.WriteFile(p, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, code := runCLI(t, "", "init", "--force"); code != 0 {
		t.Fatalf("init --force: %d", code)
	}
	if b, _ := os.ReadFile(p); !strings.HasPrefix(string(b), "# council configuration") {
		t.Fatalf("not overwritten: %q", b)
	}
}

func TestDoctorPresets(t *testing.T) {
	fakePresetCLIs(t)
	cfg := writeConfig(t, `defaults:
  agents: [codex, claude, opencode]
agents:
  agy: {harness: antigravity}
  codex: {harness: codex}
`)
	t.Run("default agent list, table", func(t *testing.T) {
		out, errOut, code := runCLI(t, "", "doctor", "--config", cfg)
		if code != 0 || errOut != "" {
			t.Fatalf("exit %d: %s\n%s", code, out, errOut)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if lines[0] != "config: "+cfg || !strings.HasPrefix(lines[2], "AGENT") || len(lines) != 7 {
			t.Fatalf("output:\n%s", out)
		}
		for i, prefix := range []string{"agy", "codex", "claude", "opencode"} { // config agents sorted, then defaults
			f := strings.Fields(lines[3+i])
			if f[0] != prefix || f[3] != "yes" || f[4] != "ok" {
				t.Errorf("row %d: %q", i, lines[3+i])
			}
		}
		for _, w := range []string{"codex-cli 0.46.0", "2.0.14 (Claude Code)", "Logged in using ChatGPT", "signed in (2 models available)", "2 stored credential(s)", "1.3.3", "1.19.0"} {
			if !strings.Contains(out, w) {
				t.Errorf("missing %q:\n%s", w, out)
			}
		}
	})
	t.Run("signed out", func(t *testing.T) {
		t.Setenv("FAKE_AUTH", "missing")
		out, _, code := runCLI(t, "", "status", "--config", cfg, "codex", "claude", "agy")
		if code != 1 || strings.Count(out, "missing") != 3 || !strings.Contains(out, "Please sign in to view available models") || !strings.Contains(out, "Some harnesses are not ready") {
			t.Fatalf("exit %d:\n%s", code, out)
		}
	})
	t.Run("json with probe", func(t *testing.T) {
		out, _, code := runCLI(t, "", "doctor", "--config", cfg, "--json", "--probe", "agy", "claude", "codex", "opencode")
		if code != 0 {
			t.Fatalf("exit %d: %s", code, out)
		}
		var rows []map[string]any
		if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 4 {
			t.Fatalf("%v: %s", err, out)
		}
		for _, r := range rows {
			if r["auth"] != "ok" || !strings.HasPrefix(r["probe"].(string), "ok: ") || r["installed"] != true {
				t.Errorf("row %v", r)
			}
		}
		if !strings.Contains(rows[0]["probe"].(string), "agy read") || !strings.Contains(rows[3]["probe"].(string), "opencode read") {
			t.Errorf("probe output: %v", rows[0]["probe"])
		}
	})
	t.Run("probe failure marks auth missing", func(t *testing.T) {
		t.Setenv("FAKE_MODE", "fail")
		out, _, code := runCLI(t, "", "doctor", "--config", cfg, "--probe", "claude")
		if code != 1 || !strings.Contains(out, "probe FAIL: claude: claude exited") || !strings.Contains(out, "missing") {
			t.Fatalf("exit %d:\n%s", code, out)
		}
	})
	t.Run("not installed is not probed", func(t *testing.T) {
		out, _, code := runCLI(t, "", "doctor", "--config", fakeConfig(t, ""), "--probe", "--json", "broken")
		if code != 1 || strings.Contains(out, `"probe"`) || !strings.Contains(out, "not found on PATH") {
			t.Fatalf("exit %d:\n%s", code, out)
		}
	})
	t.Run("unknown agent", func(t *testing.T) {
		if _, e, code := runCLI(t, "", "doctor", "--config", cfg, "ghost"); code != 2 || !strings.Contains(e, `unknown agent "ghost"`) {
			t.Fatalf("exit %d %s", code, e)
		}
	})
	t.Run("bad config", func(t *testing.T) {
		if _, _, code := runCLI(t, "", "doctor", "--config", writeConfig(t, "defaults: {rounds: -2}\n")); code != 1 {
			t.Fatalf("exit %d", code)
		}
	})
}

func TestLoginPresets(t *testing.T) {
	argsFile := fakePresetCLIs(t)
	cfg := writeConfig(t, "agents: {nolog: {command: x}}\n")
	cases := []struct {
		name     string
		args     []string
		exit     string
		code     int
		wantOut  string
		wantErr  []string
		wantArgv []string
	}{
		{"codex with extra args", []string{"codex", "--", "--device-auth"}, "0", 0, "fake codex login: login --device-auth\n",
			[]string{"--device-auth` on a headless machine", "running: codex login --device-auth"}, []string{"login", "--device-auth"}},
		{"config flag after agent", []string{"claude", "--config", cfg}, "0", 0, "fake claude login: auth login\n",
			[]string{"running: claude auth login"}, []string{"auth", "login"}},
		{"login command fails", []string{"codex"}, "3", 1, "fake codex login: login\n", []string{"council: exit status 3"}, []string{"login"}},
		{"opencode with provider", []string{"opencode", "--", "--provider", "anthropic"}, "0", 0, "fake opencode login: auth login --provider anthropic\n",
			[]string{"running: opencode auth login --provider anthropic"}, []string{"auth", "login", "--provider", "anthropic"}},
		{"unknown agent", []string{"ghost"}, "0", 2, "", []string{`unknown agent "ghost"`}, nil},
		{"removed gemini", []string{"gemini"}, "0", 2, "", []string{"Gemini CLI is deprecated", "harness: antigravity"}, nil},
		{"too many agents", []string{"codex", "claude"}, "0", 2, "", []string{"usage: council login <agent>"}, nil},
		{"no login command", []string{"nolog"}, "0", 1, "", []string{`agent "nolog" has no login command`}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			os.Remove(argsFile)
			t.Setenv("FAKE_LOGIN_EXIT", c.exit)
			args := append([]string{"login"}, c.args...)
			if c.name != "config flag after agent" {
				args = append([]string{"login", "--config", cfg}, c.args...)
			}
			out, errOut, code := runCLI(t, "", args...)
			if code != c.code || out != c.wantOut {
				t.Fatalf("exit %d (want %d)\nstdout %q\nstderr %q", code, c.code, out, errOut)
			}
			for _, w := range c.wantErr {
				if !strings.Contains(errOut, w) {
					t.Errorf("stderr missing %q: %q", w, errOut)
				}
			}
			if c.wantArgv != nil {
				b, _ := os.ReadFile(argsFile)
				if got := strings.Fields(string(b)); !reflect.DeepEqual(got, c.wantArgv) {
					t.Errorf("CLI got argv %q, want %q", got, c.wantArgv)
				}
			}
		})
	}
	t.Run("not installed", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, e, code := runCLI(t, "", "login", "--config", cfg, "opencode"); code != 1 || !strings.Contains(e, "opencode is not installed or not on PATH") {
			t.Fatalf("exit %d %q", code, e)
		}
	})
	t.Run("bad config", func(t *testing.T) {
		if _, _, code := runCLI(t, "", "login", "--config", writeConfig(t, "nope: 1\n"), "codex"); code != 1 {
			t.Fatalf("exit %d", code)
		}
	})
}

func TestAgentsListing(t *testing.T) {
	cfg := writeConfig(t, `defaults: {agents: [mine], rounds: 4, moderator: mine}
agents:
  mine:
    command: tool
    args: ["-o", "{{output_file}}", "{{prompt}}"]
    output: json
    json_path: a.b
    output_source: file
`)
	out, _, code := runCLI(t, "", "agents", "--config", cfg)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, w := range []string{"config: " + cfg, "default agents: mine · rounds: 4 · moderator: mine", "AGENT", "mine   generic  arg     json:a.b (file)  tool -o <output_file> <prompt>"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q:\n%s", w, out)
		}
	}
	if _, _, code := runCLI(t, "", "agents", "--config", writeConfig(t, "defaults: {rounds: -1}\n")); code != 1 {
		t.Errorf("bad config: exit %d", code)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }

func TestDoctorJSONWriteError(t *testing.T) {
	cfg := writeConfig(t, "agents: {x: {command: definitely-not-installed-xyz}}\n")
	var e bytes.Buffer
	code := Main([]string{"doctor", "--config", cfg, "--json", "x"}, Env{Stdout: failingWriter{}, Stderr: &e, StdinIsTTY: true})
	if code != 1 || !strings.Contains(e.String(), "stdout closed") {
		t.Fatalf("exit %d %q", code, e.String())
	}
}
