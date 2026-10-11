package web

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Parsa2820/council/internal/chat"
	"github.com/Parsa2820/council/internal/config"
	"github.com/Parsa2820/council/internal/consensus"
	"github.com/Parsa2820/council/internal/harness"
)

// fakeAgentConfig is a config whose agents run examples/fake-agent.sh.
const fakeAgentConfig = `
defaults:
  agents: [alpha, beta, gamma]
  rounds: 3
  timeout: 20s
agents:
  alpha: {command: SCRIPT, args: [alpha, "42", follow], prompt_mode: stdin, status_args: ["--status-probe"]}
  beta:  {command: SCRIPT, args: [beta, "42", stubborn], prompt_mode: stdin, status_args: ["--status-probe"]}
  gamma: {command: SCRIPT, args: [gamma, "41", follow], prompt_mode: stdin}
  slow:  {command: SCRIPT, args: [slow, "1", follow, "30"], prompt_mode: stdin}
  judge: {command: SCRIPT, args: [judge, "n/a"], prompt_mode: stdin}
  broken: {command: /definitely/not/here, prompt_mode: stdin}
`

func needSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a POSIX shell script")
	}
}

func loader(t *testing.T, yaml string) func() (*config.Config, error) {
	t.Helper()
	script, err := filepath.Abs("../../examples/fake-agent.sh")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(strings.ReplaceAll(yaml, "SCRIPT", script)), 0o644); err != nil {
		t.Fatal(err)
	}
	return func() (*config.Config, error) { return config.Load(p) }
}

func newServer(t *testing.T, opts Options) (*Server, *httptest.Server) {
	t.Helper()
	if opts.LoadConfig == nil {
		opts.LoadConfig = loader(t, fakeAgentConfig)
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return s, ts
}

// post sends a same-origin JSON request with the page token.
func post(t *testing.T, s *Server, ts *httptest.Server, path, body string, mod func(*http.Request)) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", ts.URL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set(TokenHeader, s.Token())
	if mod != nil {
		mod(req)
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func errorOf(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	var body struct{ Error string }
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("status %d, body is not a JSON error: %v", res.StatusCode, err)
	}
	return body.Error
}

type event struct {
	Name string
	Data map[string]any
}

// readEvents parses an SSE stream until it ends, calling each(e) per event;
// each may return false to stop reading.
func readEvents(t *testing.T, r io.Reader, each func(event) bool) []event {
	t.Helper()
	var out []event
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var cur event
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.Name = line[7:]
		case strings.HasPrefix(line, "data: "):
			if err := json.Unmarshal([]byte(line[6:]), &cur.Data); err != nil {
				t.Fatalf("bad data %q: %v", line, err)
			}
		case line == "" && cur.Name != "":
			out = append(out, cur)
			if each != nil && !each(cur) {
				return out
			}
			cur = event{}
		}
	}
	return out
}

func names(evs []event) string {
	var n []string
	for _, e := range evs {
		n = append(n, e.Name)
	}
	return strings.Join(n, " ")
}

func TestNew(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Error("LoadConfig is required")
	}
	s, err := New(Options{LoadConfig: func() (*config.Config, error) { return config.Default(), nil }})
	if err != nil {
		t.Fatal(err)
	}
	if s.opts.MaxRuns != 2 || s.opts.StatusTimeout != 20*time.Second || s.opts.Heartbeat != 15*time.Second || cap(s.slots) != 2 {
		t.Errorf("defaults not applied: %+v", s.opts)
	}
	if len(s.Token()) != 32 {
		t.Errorf("token %q", s.Token())
	}
	if s2, _ := New(Options{LoadConfig: func() (*config.Config, error) { return config.Default(), nil }}); s2.Token() == s.Token() {
		t.Error("each server needs its own token")
	}
	old := randRead
	t.Cleanup(func() { randRead = old })
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	if _, err := New(Options{LoadConfig: func() (*config.Config, error) { return config.Default(), nil }}); err == nil || !strings.Contains(err.Error(), "no entropy") {
		t.Errorf("got %v", err)
	}
}

func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"localhost": true, "LOCALHOST:8787": true, "127.0.0.1": true, "127.0.0.1:8787": true, "127.3.4.5:1": true,
		"[::1]:8787": true, "::1": true, "[::1]": true,
		"": false, ":8787": false, "0.0.0.0:8787": false, "[::]:8787": false, "192.168.1.5:8787": false,
		"evil.example:8787": false, "localhost.evil.example": false, "10.0.0.1": false,
	}
	for in, want := range cases {
		if got := IsLoopbackHost(in); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIndexAndStaticAssets(t *testing.T) {
	s, ts := newServer(t, Options{Version: "v9.9.9"})
	cases := []struct {
		path, ctype string
		want        []string
	}{
		{"/", "text/html", []string{`<meta name="council-token" content="` + s.Token() + `">`, "council v9.9.9", `src="/static/app.js"`, `id="prompt"`, `id="agent-list"`, `id="moderator"`, `id="cancel"`, `id="download"`}},
		{"/static/app.js", "javascript", []string{`fetch("/api/run"`, "X-Council-Token", "transcript", "renderAgents", "refresh=1", "checking…", `$("moderator").addEventListener("change"`}},
		{"/static/style.css", "text/css", []string{"prefers-color-scheme: dark", "@media (max-width"}},
	}
	for _, c := range cases {
		res, err := ts.Client().Get(ts.URL + c.path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Type"), c.ctype) {
			t.Errorf("%s: %d %s", c.path, res.StatusCode, res.Header.Get("Content-Type"))
		}
		for _, w := range c.want {
			if !strings.Contains(string(b), w) {
				t.Errorf("%s does not contain %q", c.path, w)
			}
		}
		for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
			if res.Header.Get(h) == "" {
				t.Errorf("%s: missing %s", c.path, h)
			}
		}
	}
	for _, p := range []string{"/static/nope.js", "/nope", "/api/nope"} {
		res, _ := ts.Client().Get(ts.URL + p)
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d", p, res.StatusCode)
		}
	}
	res, _ := ts.Client().Post(ts.URL+"/api/agents", "application/json", nil)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden && res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/agents: %d", res.StatusCode)
	}
}

func TestHostCheck(t *testing.T) {
	for _, allow := range []bool{false, true} {
		_, ts := newServer(t, Options{AllowAnyHost: allow})
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/agents?status=0", nil)
		req.Host = "evil.example:8787" // DNS rebinding: the name resolves to 127.0.0.1
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if want := map[bool]int{false: 403, true: 200}[allow]; res.StatusCode != want {
			t.Errorf("AllowAnyHost=%v: status %d, want %d", allow, res.StatusCode, want)
		}
		if !allow && !strings.Contains(errorOf(t, res), `host "evil.example:8787" is not allowed`) {
			t.Error("error message")
		}
		res.Body.Close()
	}
}

func TestWriteChecks(t *testing.T) {
	s, ts := newServer(t, Options{})
	cases := []struct {
		name string
		mod  func(*http.Request)
		want string
	}{
		{"cross-site fetch", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, "cross-site request refused"},
		{"same-site fetch", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }, "cross-site request refused"},
		{"foreign origin", func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }, "cross-origin request refused"},
		{"other port", func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:1") }, "cross-origin request refused"},
		{"null origin", func(r *http.Request) { r.Header.Set("Origin", "null") }, "cross-origin request refused"},
		{"bad origin", func(r *http.Request) { r.Header.Set("Origin", "http://[::1") }, "cross-origin request refused"},
		{"form post", func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }, "content type must be application/json"},
		{"text/plain", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, "content type must be application/json"},
		{"no token", func(r *http.Request) { r.Header.Del(TokenHeader) }, "missing or wrong X-Council-Token"},
		{"wrong token", func(r *http.Request) { r.Header.Set(TokenHeader, strings.Repeat("0", 32)) }, "missing or wrong X-Council-Token"},
	}
	for _, c := range cases {
		res := post(t, s, ts, "/api/run", `{"prompt":"q"}`, c.mod)
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d", c.name, res.StatusCode)
		}
		if msg := errorOf(t, res); !strings.Contains(msg, c.want) {
			t.Errorf("%s: %q", c.name, msg)
		}
	}
	// Non-browser clients send no Origin or Sec-Fetch-Site; the token and the
	// JSON content type are still required, and enough.
	res := post(t, s, ts, "/api/runs/none/cancel", `{}`, func(r *http.Request) {
		r.Header.Del("Origin")
		r.Header.Set("Sec-Fetch-Site", "none")
	})
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("valid request should reach the handler: %d", res.StatusCode)
	}
	res.Body.Close()
}

func TestAgentsEndpoint(t *testing.T) {
	needSh(t)
	_, ts := newServer(t, Options{LoadConfig: loader(t, fakeAgentConfig+"\n# defaults include an undefined, non-built-in name\n")})
	res, err := ts.Client().Get(ts.URL + "/api/agents")
	if err != nil {
		t.Fatal(err)
	}
	var body AgentsResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.Header.Get("Cache-Control") != "no-store" || !strings.HasSuffix(body.Config, "config.yaml") {
		t.Errorf("headers/config: %v %q", res.Header, body.Config)
	}
	if body.Defaults.Rounds != 3 || body.Defaults.Timeout != "20s" || body.Defaults.Moderator != "majority" || strings.Join(body.Defaults.Agents, ",") != "alpha,beta,gamma" {
		t.Errorf("defaults %+v", body.Defaults)
	}
	if body.Limits.MaxRounds != 10 || body.Limits.MaxAgents != 12 {
		t.Errorf("limits %+v", body.Limits)
	}
	got := map[string]AgentInfo{}
	var order []string
	for _, a := range body.Agents {
		got[a.Agent] = a
		order = append(order, a.Agent)
	}
	if strings.Join(order, ",") != "alpha,beta,gamma,broken,judge,slow" {
		t.Errorf("default agents first, then the rest sorted: %v", order)
	}
	if a := got["alpha"]; !a.Default || !a.Installed || a.Auth != "ok" || a.Version != "fake-agent 1.0" || a.Detail != "Logged in (fake)" || a.Kind != "generic" {
		t.Errorf("alpha %+v", a)
	}
	if a := got["gamma"]; a.Auth != "unknown" || a.Detail != "no status command configured" {
		t.Errorf("gamma %+v", a)
	}
	if a := got["broken"]; a.Installed || a.Default || a.Detail != "not found on PATH" {
		t.Errorf("broken %+v", a)
	}

	// ?status=0 skips the (slow) checks.
	res, _ = ts.Client().Get(ts.URL + "/api/agents?status=0")
	body = AgentsResponse{}
	json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	if a := body.Agents[0]; a.Agent != "alpha" || a.Installed || a.Auth != "unknown" || a.Command == "" {
		t.Errorf("status=0: %+v", a)
	}
}

func TestAgentsUnknownDefaultAndConfigError(t *testing.T) {
	_, ts := newServer(t, Options{LoadConfig: func() (*config.Config, error) {
		c, err := config.Parse([]byte("defaults: {agents: [ghost]}\n"))
		return c, err
	}})
	res, _ := ts.Client().Get(ts.URL + "/api/agents?status=0")
	var body AgentsResponse
	json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	if len(body.Agents) != 1 || body.Agents[0].Agent != "ghost" || !strings.Contains(body.Agents[0].Detail, `unknown agent "ghost"`) {
		t.Errorf("got %+v", body.Agents)
	}

	s, ts := newServer(t, Options{LoadConfig: func() (*config.Config, error) { return nil, errors.New("bad yaml") }})
	res, _ = ts.Client().Get(ts.URL + "/api/agents")
	if res.StatusCode != 500 || errorOf(t, res) != "loading config: bad yaml" {
		t.Errorf("agents: %d", res.StatusCode)
	}
	res = post(t, s, ts, "/api/run", `{"prompt":"q"}`, nil)
	if res.StatusCode != 500 || errorOf(t, res) != "loading config: bad yaml" {
		t.Errorf("run: %d", res.StatusCode)
	}
}

func TestRunValidation(t *testing.T) {
	s, ts := newServer(t, Options{})
	long := strings.Repeat("x", limits.MaxPromptBytes+1)
	many := `"alpha"` + strings.Repeat(`,"alpha"`, limits.MaxAgents)
	cases := []struct{ body, want string }{
		{`not json`, "invalid request body"},
		{`{"prompt":"q","colour":"red"}`, `unknown field "colour"`},
		{`{"prompt":"   "}`, "prompt is empty"},
		{`{"prompt":"` + long + `"}`, "the limit is 200000"},
		{`{"prompt":"q","rounds":-1}`, "rounds must be between 1 and 10"},
		{`{"prompt":"q","rounds":11}`, "rounds must be between 1 and 10"},
		{`{"prompt":"q","agents":[` + many + `]}`, "at most 12 agents"},
		{`{"prompt":"q","agents":["ghost"]}`, `unknown agent "ghost"`},
		{`{"prompt":"q","agents":["alpha","alpha"]}`, `duplicate agent "alpha"`},
		{`{"prompt":"q","timeout":"soon"}`, `timeout "soon" must be a duration`},
		{`{"prompt":"q","timeout":"-5s"}`, "must be a duration"},
		{`{"prompt":"q","timeout":"2h"}`, "at most 1h0m0s"},
		{`{"prompt":"q","moderator":"nobody"}`, `moderator: unknown agent "nobody"`},
	}
	for _, c := range cases {
		res := post(t, s, ts, "/api/run", c.body, nil)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%.60s: status %d", c.body, res.StatusCode)
		}
		if msg := errorOf(t, res); !strings.Contains(msg, c.want) {
			t.Errorf("%.60s: %q does not contain %q", c.body, msg, c.want)
		}
	}
}

func TestRunStreamsToConsensus(t *testing.T) {
	needSh(t)
	s, ts := newServer(t, Options{})
	res := post(t, s, ts, "/api/run", `{"prompt":"What is 6*7?"}`, nil)
	defer res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	evs := readEvents(t, res.Body, nil)
	want := "start round_start message message message round_start message message message consensus result transcript"
	if got := names(evs); got != want {
		t.Fatalf("events:\n got %s\nwant %s", got, want)
	}
	start := evs[0].Data
	if len(start["id"].(string)) != 12 || start["rounds"].(float64) != 3 || start["moderator"] != "" {
		t.Errorf("start %v", start)
	}
	for _, e := range evs {
		if e.Name != "message" {
			continue
		}
		v, _ := e.Data["vote"].(map[string]any)
		if e.Data["role"] != "agent" || v == nil || v["answer"] == "" || strings.Contains(e.Data["text"].(string), "```") {
			t.Errorf("message %v", e.Data)
		}
	}
	if c := evs[9].Data; c["answer"] != "42" || c["round"].(float64) != 2 {
		t.Errorf("consensus %v", c)
	}
	r := evs[10].Data
	if r["answer"] != "42" || r["method"] != "unanimous" || r["converged"] != true || r["rounds"].(float64) != 2 ||
		len(r["supporters"].([]any)) != 3 || len(r["dissenters"].([]any)) != 0 {
		t.Errorf("result %v", r)
	}
	md := evs[11].Data["markdown"].(string)
	if !strings.Contains(md, "# Council transcript") || !strings.Contains(md, "- **Answer:** 42") {
		t.Errorf("transcript:\n%s", md)
	}
	if len(s.runs) != 0 || len(s.slots) != 0 {
		t.Errorf("run not cleaned up: %d runs, %d slots", len(s.runs), len(s.slots))
	}
}

func TestRunModeratorTimeoutAndLoose(t *testing.T) {
	needSh(t)
	s, ts := newServer(t, Options{})
	res := post(t, s, ts, "/api/run", `{"prompt":"q","agents":["alpha","gamma"],"rounds":1,"moderator":"judge","timeout":"30s","loose":true}`, nil)
	defer res.Body.Close()
	evs := readEvents(t, res.Body, nil)
	if got := names(evs); got != "start round_start message message no_consensus moderator_start message result transcript" {
		t.Fatalf("events: %s", got)
	}
	if evs[0].Data["moderator"] != "judge" || evs[6].Data["role"] != "moderator" || evs[6].Data["agent"] != "judge" {
		t.Errorf("moderator events: %v %v", evs[0].Data, evs[6].Data)
	}
	if r := evs[7].Data; r["method"] != "moderator" || r["moderator"] != "judge" || r["converged"] != false {
		t.Errorf("result %v", r)
	}
	// "majority", "vote" and "none" mean no moderator agent.
	for _, m := range []string{"majority", "vote", "none"} {
		res := post(t, s, ts, "/api/run", `{"prompt":"q","agents":["alpha"],"rounds":1,"moderator":"`+m+`","timeout":"0"}`, nil)
		evs := readEvents(t, res.Body, nil)
		res.Body.Close()
		if evs[0].Data["moderator"] != "" || evs[len(evs)-2].Name != "result" {
			t.Errorf("%s: %s", m, names(evs))
		}
	}
}

func TestRunError(t *testing.T) {
	needSh(t)
	s, ts := newServer(t, Options{})
	res := post(t, s, ts, "/api/run", `{"prompt":"q","agents":["broken"]}`, nil)
	defer res.Body.Close()
	evs := readEvents(t, res.Body, nil)
	if got := names(evs); got != "start round_start message error transcript" {
		t.Fatalf("events: %s", got)
	}
	if msg := evs[2].Data["error"].(string); !strings.Contains(msg, "broken") {
		t.Errorf("message error %q", msg)
	}
	if !strings.Contains(evs[3].Data["error"].(string), "every agent failed") {
		t.Errorf("error %v", evs[3].Data)
	}
	if md := evs[4].Data["markdown"].(string); !strings.Contains(md, "**error:**") || strings.Contains(md, "## Result") {
		t.Errorf("partial transcript:\n%s", md)
	}
}

// startSlow starts a run whose agent sleeps 30s and returns the response
// after its start event.
func startSlow(t *testing.T, s *Server, ts *httptest.Server) (*http.Response, string, *bufio.Reader) {
	t.Helper()
	res := post(t, s, ts, "/api/run", `{"prompt":"q","agents":["slow"]}`, nil)
	if res.StatusCode != 200 {
		t.Fatalf("status %d: %s", res.StatusCode, errorOf(t, res))
	}
	br := bufio.NewReader(res.Body)
	var id string
	readEvents(t, br, func(e event) bool { id, _ = e.Data["id"].(string); return false })
	if id == "" {
		t.Fatal("no start event")
	}
	return res, id, br
}

func waitIdle(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		n := len(s.runs)
		s.mu.Unlock()
		if n == 0 && len(s.slots) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run still active: %d runs, %d slots", n, len(s.slots))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCancelEndpoint(t *testing.T) {
	needSh(t)
	s, ts := newServer(t, Options{})
	res, id, br := startSlow(t, s, ts)
	defer res.Body.Close()
	start := time.Now()
	c := post(t, s, ts, "/api/runs/"+id+"/cancel", `{}`, nil)
	if c.StatusCode != 200 {
		t.Fatalf("cancel: %d", c.StatusCode)
	}
	c.Body.Close()
	evs := readEvents(t, br, nil)
	if got := names(evs); !strings.HasSuffix(got, "cancelled transcript") {
		t.Fatalf("events after cancel: %s", got)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("cancel took %v", time.Since(start))
	}
	waitIdle(t, s)
	c = post(t, s, ts, "/api/runs/"+id+"/cancel", `{}`, nil)
	if c.StatusCode != http.StatusNotFound || !strings.Contains(errorOf(t, c), "no active run") {
		t.Errorf("cancelling a finished run: %d", c.StatusCode)
	}
}

func TestClientDisconnectCancelsRun(t *testing.T) {
	needSh(t)
	s, ts := newServer(t, Options{})
	res, _, _ := startSlow(t, s, ts)
	res.Body.Close() // the browser tab goes away
	waitIdle(t, s)
}

func TestConcurrentRunLimit(t *testing.T) {
	needSh(t)
	s, ts := newServer(t, Options{MaxRuns: 1})
	res, _, _ := startSlow(t, s, ts)
	second := post(t, s, ts, "/api/run", `{"prompt":"q","agents":["alpha"]}`, nil)
	if second.StatusCode != http.StatusTooManyRequests || !strings.Contains(errorOf(t, second), "1 runs are already in progress") {
		t.Errorf("second run: %d", second.StatusCode)
	}
	res.Body.Close()
	waitIdle(t, s)
	third := post(t, s, ts, "/api/run", `{"prompt":"q","agents":["alpha"],"rounds":1}`, nil)
	defer third.Body.Close()
	if third.StatusCode != 200 {
		t.Errorf("after the first run ended a new one should start: %d", third.StatusCode)
	}
}

func TestHeartbeat(t *testing.T) {
	needSh(t)
	s, ts := newServer(t, Options{Heartbeat: 20 * time.Millisecond})
	res, _, br := startSlow(t, s, ts)
	defer res.Body.Close()
	// The round_start and message events may come first; a keep-alive
	// comment must follow while the slow agent works.
	for i := 0; i < 20; i++ {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == ": keep-alive\n" {
			return
		}
	}
	t.Fatal("no keep-alive comment")
}

// noFlush is a ResponseWriter that cannot stream.
type noFlush struct {
	h    http.Header
	code int
}

func (w *noFlush) Header() http.Header         { return w.h }
func (w *noFlush) Write(b []byte) (int, error) { return len(b), nil }
func (w *noFlush) WriteHeader(c int)           { w.code = c }

func TestRunNeedsFlusher(t *testing.T) {
	s, err := New(Options{LoadConfig: loader(t, fakeAgentConfig)})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/run", strings.NewReader(`{"prompt":"q","agents":["alpha"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(TokenHeader, s.Token())
	w := &noFlush{h: http.Header{}}
	s.ServeHTTP(w, req)
	if w.code != http.StatusInternalServerError {
		t.Fatalf("status %d", w.code)
	}
}

func TestEventJSON(t *testing.T) {
	m := &chat.Message{Round: 2, Agent: "a", Role: chat.RoleAgent, Error: "a: timed out", Duration: 1500 * time.Millisecond}
	cases := []struct {
		e    chat.Event
		name string
		want string
	}{
		{chat.Event{Kind: chat.EventRoundStart, Round: 1}, "round_start", `{"round":1}`},
		{chat.Event{Kind: chat.EventNoConsensus, Round: 3}, "no_consensus", `{"round":3}`},
		{chat.Event{Kind: chat.EventModeratorStart, Round: 3}, "moderator_start", `{"round":3}`},
		{chat.Event{Kind: chat.EventConsensus, Round: 2, Text: "x"}, "consensus", `{"answer":"x","round":2}`},
		{chat.Event{Kind: chat.EventAgentMessage, Message: m}, "message", `{"round":2,"agent":"a","role":"agent","error":"a: timed out","duration_ms":1500}`},
		{chat.Event{Kind: chat.EventModerator, Message: &chat.Message{Agent: "j", Role: chat.RoleModerator, Content: "Prose.\n```json\n{\"final_answer\":\"x\",\"agree\":true,\"confidence\":0.5}\n```"}}, "message",
			`{"round":0,"agent":"j","role":"moderator","text":"Prose.","vote":{"answer":"x","agree":true,"confidence":0.5},"duration_ms":0}`},
	}
	for _, c := range cases {
		if c.e.Message != nil && c.e.Message.Content != "" {
			v, _ := consensus.ParseVote(c.e.Message.Content)
			c.e.Message.Vote = &v
		}
		got := eventJSON(c.e)
		b, _ := json.Marshal(got.data)
		if got.name != c.name || string(b) != c.want {
			t.Errorf("%s: got %s %s", c.e.Kind, got.name, b)
		}
	}
	r := resultJSON(&chat.Result{Answer: "a", Method: "majority"})
	if r.Supporters == nil || r.Dissenters == nil {
		t.Error("supporters/dissenters must encode as [] not null")
	}
}

func TestOpenCommand(t *testing.T) {
	cases := map[string]string{
		"darwin":  "open http://x/",
		"windows": "rundll32 url.dll,FileProtocolHandler http://x/",
		"linux":   "xdg-open http://x/",
		"freebsd": "xdg-open http://x/",
	}
	for goos, want := range cases {
		if got := strings.Join(OpenCommand(goos, "http://x/"), " "); got != want {
			t.Errorf("%s: %s", goos, got)
		}
	}
}

func TestOpenBrowser(t *testing.T) {
	needSh(t)
	bin := t.TempDir()
	log := filepath.Join(bin, "opened")
	name := OpenCommand(runtime.GOOS, "")[0]
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho \"$@\" > "+log+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if err := OpenBrowser("http://127.0.0.1:8787/"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(log)
		if strings.TrimSpace(string(b)) == "http://127.0.0.1:8787/" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("browser opener got %q", b)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Setenv("PATH", t.TempDir())
	if err := OpenBrowser("http://x/"); err == nil {
		t.Error("missing opener should fail")
	}
}

// TestModeratorChoiceIsUsed is the regression test for "the moderator cannot
// be selected": whatever the request names wins over the config default,
// the moderator may also be a participant, and the result says so.
func TestModeratorChoiceIsUsed(t *testing.T) {
	needSh(t)
	cfg := strings.Replace(fakeAgentConfig, "  timeout: 20s\n", "  timeout: 20s\n  moderator: judge\n", 1)
	s, ts := newServer(t, Options{LoadConfig: loader(t, cfg)})
	cases := []struct {
		name, body, wantMod, wantMethod string
	}{
		{"participant as moderator", `{"prompt":"q","agents":["alpha","beta"],"rounds":1,"moderator":"alpha"}`, "alpha", "moderator"},
		{"non-participant", `{"prompt":"q","agents":["alpha","beta"],"rounds":1,"moderator":"gamma"}`, "gamma", "moderator"},
		{"config default", `{"prompt":"q","agents":["alpha","beta"],"rounds":1}`, "judge", "moderator"},
		{"majority overrides the default", `{"prompt":"q","agents":["alpha","beta"],"rounds":1,"moderator":"majority"}`, "", "unanimous"},
	}
	for _, c := range cases {
		res := post(t, s, ts, "/api/run", c.body, nil)
		evs := readEvents(t, res.Body, nil)
		res.Body.Close()
		var mod, result map[string]any
		for _, e := range evs {
			if e.Name == "message" && e.Data["role"] == "moderator" {
				mod = e.Data
			}
			if e.Name == "result" {
				result = e.Data
			}
		}
		if evs[0].Data["moderator"] != c.wantMod || result == nil || result["method"] != c.wantMethod {
			t.Errorf("%s: start %v result %v", c.name, evs[0].Data, result)
			continue
		}
		if c.wantMod == "" {
			if mod != nil || result["moderator"] != nil {
				t.Errorf("%s: no moderator expected, got %v", c.name, mod)
			}
			continue
		}
		if mod == nil || mod["agent"] != c.wantMod || result["moderator"] != c.wantMod || !strings.Contains(mod["text"].(string), "As moderator ("+c.wantMod+")") {
			t.Errorf("%s: moderator message %v, result %v", c.name, mod, result)
		}
	}
}

func TestModeratorFailureFallsBack(t *testing.T) {
	needSh(t)
	s, ts := newServer(t, Options{})
	res := post(t, s, ts, "/api/run", `{"prompt":"q","agents":["alpha","beta"],"rounds":1,"moderator":"broken"}`, nil)
	evs := readEvents(t, res.Body, nil)
	res.Body.Close()
	r := evs[len(evs)-2].Data
	notes, _ := r["notes"].([]any)
	if evs[len(evs)-2].Name != "result" || r["method"] != "unanimous" || r["moderator"] != "broken" || len(notes) != 1 || !strings.HasPrefix(notes[0].(string), "moderator broken failed") {
		t.Fatalf("result %v", r)
	}
}

// statusCounter is a generic agent whose status command counts its runs.
func statusCounter(t *testing.T) (yaml, counter string) {
	t.Helper()
	dir := t.TempDir()
	counter = filepath.Join(dir, "count")
	script := filepath.Join(dir, "agent")
	body := "#!/bin/sh\ncase \"$1\" in\n--version) echo v1 ;;\nwhoami) echo x >> " + counter + "; [ \"${SLOW_STATUS:-}\" ] && sleep 2; echo signed in ;;\nesac\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return "defaults: {agents: [c]}\nagents:\n  c: {command: " + script + ", status_args: [whoami]}\n", counter
}

func TestStatusCache(t *testing.T) {
	needSh(t)
	yaml, counter := statusCounter(t)
	_, ts := newServer(t, Options{LoadConfig: loader(t, yaml), StatusTTL: time.Minute})
	clock := time.Now()
	old := now
	t.Cleanup(func() { now = old })
	now = func() time.Time { return clock }
	get := func(q string) AgentInfo {
		t.Helper()
		res, err := ts.Client().Get(ts.URL + "/api/agents" + q)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body AgentsResponse
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil || len(body.Agents) != 1 {
			t.Fatalf("%v %+v", err, body)
		}
		return body.Agents[0]
	}
	runs := func() int { b, _ := os.ReadFile(counter); return strings.Count(string(b), "x") }
	for i, step := range []struct {
		q       string
		advance time.Duration
		want    int
	}{
		{"", 0, 1},          // first check
		{"", 0, 1},          // cached
		{"?status=0", 0, 1}, // no check at all
		{"?refresh=1", 0, 2},
		{"", 59 * time.Second, 2}, // still fresh
		{"", 2 * time.Second, 3},  // expired
	} {
		clock = clock.Add(step.advance)
		a := get(step.q)
		if runs() != step.want {
			t.Errorf("step %d (%s): %d status runs, want %d", i, step.q, runs(), step.want)
		}
		if step.q != "?status=0" && (a.Auth != harness.AuthOK || a.Detail != "signed in") {
			t.Errorf("step %d: %+v", i, a)
		}
	}
}

func TestStatusTimeoutIsNotCached(t *testing.T) {
	needSh(t)
	yaml, counter := statusCounter(t)
	t.Setenv("SLOW_STATUS", "1")
	s, ts := newServer(t, Options{LoadConfig: loader(t, yaml), StatusTimeout: 300 * time.Millisecond})
	res, _ := ts.Client().Get(ts.URL + "/api/agents")
	res.Body.Close()
	s.statusMu.Lock()
	n := len(s.statuses)
	s.statusMu.Unlock()
	if b, _ := os.ReadFile(counter); n != 0 || len(b) == 0 {
		t.Errorf("a status check cut short by the timeout was cached (%d entries, ran %q)", n, b)
	}
}
