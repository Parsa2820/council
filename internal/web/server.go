package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Parsa2820/council/internal/config"
	"github.com/Parsa2820/council/internal/harness"
)

//go:embed static
var staticFS embed.FS

// TokenHeader carries the per-process CSRF token on state-changing requests.
const TokenHeader = "X-Council-Token"

// Options configure a Server.
type Options struct {
	// LoadConfig returns the current config; it is called for every request,
	// so edits to the config file apply without a restart.
	LoadConfig func() (*config.Config, error)
	// MaxRuns is the number of runs that may be active at once (default 2).
	MaxRuns int
	// AllowAnyHost disables the loopback Host check. Set it only when the
	// server is deliberately bound to a non-loopback address.
	AllowAnyHost bool
	// Version is shown in the page footer.
	Version string
	// StatusTimeout bounds the per-agent install/sign-in check (default 20s).
	StatusTimeout time.Duration
	// Heartbeat is the interval of SSE keep-alive comments (default 15s).
	Heartbeat time.Duration
	// StatusTTL is how long an agent's install/sign-in status is cached
	// (default 3m), so reloading the page does not rerun every check.
	// GET /api/agents?refresh=1 checks again.
	StatusTTL time.Duration
}

// Server is the web UI's http.Handler.
type Server struct {
	opts  Options
	token string
	page  *template.Template
	mux   *http.ServeMux
	slots chan struct{}

	mu   sync.Mutex
	runs map[string]context.CancelFunc

	statusMu sync.Mutex
	statuses map[string]cachedStatus
}

type cachedStatus struct {
	st harness.Status
	at time.Time
}

var now = time.Now

// check returns h's status, from the cache when it is fresh. The key covers
// everything the check depends on, so editing the config invalidates it.
func (s *Server) check(ctx context.Context, h *harness.CommandHarness, refresh bool) harness.Status {
	sp := h.Spec()
	key := strings.Join(append([]string{h.Name(), sp.Kind, sp.Command}, sp.StatusArgs...), "\x00")
	s.statusMu.Lock()
	c, ok := s.statuses[key]
	s.statusMu.Unlock()
	if ok && !refresh && now().Sub(c.at) < s.opts.StatusTTL {
		return c.st
	}
	st := h.Check(ctx)
	if ctx.Err() == nil { // do not cache a check cut short by the timeout
		s.statusMu.Lock()
		s.statuses[key] = cachedStatus{st, now()}
		s.statusMu.Unlock()
	}
	return st
}

// New returns a Server. It fails only if the embedded assets are broken or
// the system has no randomness for the CSRF token.
func New(opts Options) (*Server, error) {
	if opts.LoadConfig == nil {
		return nil, errors.New("web: LoadConfig is required")
	}
	if opts.MaxRuns <= 0 {
		opts.MaxRuns = 2
	}
	if opts.StatusTimeout <= 0 {
		opts.StatusTimeout = 20 * time.Second
	}
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = 15 * time.Second
	}
	if opts.StatusTTL <= 0 {
		opts.StatusTTL = 3 * time.Minute
	}
	tok, err := newToken()
	if err != nil {
		return nil, err
	}
	page, err := template.ParseFS(staticFS, "static/index.html")
	if err != nil {
		return nil, err
	}
	s := &Server{opts: opts, token: tok, page: page, slots: make(chan struct{}, opts.MaxRuns), runs: map[string]context.CancelFunc{}, statuses: map[string]cachedStatus{}}
	sub, _ := fs.Sub(staticFS, "static")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(sub)))
	mux.HandleFunc("GET /api/agents", s.handleAgents)
	mux.HandleFunc("POST /api/run", s.handleRun)
	mux.HandleFunc("POST /api/runs/{id}/cancel", s.handleCancel)
	s.mux = mux
	return s, nil
}

var randRead = rand.Read

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := randRead(b); err != nil {
		return "", fmt.Errorf("web: generating token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Token returns the CSRF token that the page sends in TokenHeader.
func (s *Server) Token() string { return s.token }

// ServeHTTP applies the security checks and headers, then routes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	if !s.opts.AllowAnyHost && !IsLoopbackHost(r.Host) {
		// Blocks DNS rebinding: a page on evil.example that resolves to
		// 127.0.0.1 still sends Host: evil.example.
		httpError(w, http.StatusForbidden, "host %q is not allowed; open the UI via localhost or 127.0.0.1", r.Host)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if err := s.checkWrite(r); err != nil {
			httpError(w, http.StatusForbidden, "%v", err)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

// checkWrite is the CSRF defence for state-changing requests: same-origin,
// a JSON body (which a plain HTML form cannot send) and the page's token.
func (s *Server) checkWrite(r *http.Request) error {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return fmt.Errorf("cross-site request refused (Sec-Fetch-Site: %s)", site)
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || u.Host != r.Host {
			return fmt.Errorf("cross-origin request refused (Origin %q, Host %q)", o, r.Host)
		}
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		return fmt.Errorf("content type must be application/json")
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(TokenHeader)), []byte(s.token)) != 1 {
		return fmt.Errorf("missing or wrong %s header; reload the page", TokenHeader)
	}
	return nil
}

// IsLoopbackHost reports whether a Host header (or listen address) names this
// machine only: localhost, 127.0.0.0/8 or ::1, with or without a port.
func IsLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func httpError(w http.ResponseWriter, code int, format string, args ...any) {
	writeJSON(w, code, map[string]string{"error": fmt.Sprintf(format, args...)})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = s.page.Execute(w, map[string]string{"Token": s.token, "Version": s.opts.Version})
}

// AgentInfo is one row of GET /api/agents.
type AgentInfo struct {
	harness.Status
	Default bool `json:"default"`
}

// AgentsResponse is the body of GET /api/agents.
type AgentsResponse struct {
	Config   string      `json:"config"`
	Agents   []AgentInfo `json:"agents"`
	Defaults struct {
		Agents    []string `json:"agents"`
		Rounds    int      `json:"rounds"`
		Moderator string   `json:"moderator"`
		Timeout   string   `json:"timeout"`
	} `json:"defaults"`
	Limits Limits `json:"limits"`
}

// agentNames lists configured agents (sorted) followed by default agents that
// are built-in presets without a definition, as `council doctor` does.
func agentNames(cfg *config.Config) []string {
	names := cfg.AgentNames()
	for _, d := range cfg.Defaults.Agents {
		if _, ok := cfg.Agents[d]; !ok {
			names = append(names, d)
		}
	}
	return names
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.opts.LoadConfig()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "loading config: %v", err)
		return
	}
	var resp AgentsResponse
	resp.Config = cfg.Path()
	resp.Defaults.Agents = cfg.Defaults.Agents
	resp.Defaults.Rounds = cfg.Defaults.Rounds
	resp.Defaults.Moderator = cfg.Defaults.Moderator
	resp.Defaults.Timeout = time.Duration(cfg.Defaults.Timeout).String()
	resp.Limits = limits
	isDefault := map[string]bool{}
	for _, d := range cfg.Defaults.Agents {
		isDefault[d] = true
	}
	names := agentNames(cfg)
	resp.Agents = make([]AgentInfo, len(names))
	check := r.URL.Query().Get("status") != "0"
	refresh := r.URL.Query().Get("refresh") == "1"
	ctx, cancel := context.WithTimeout(r.Context(), s.opts.StatusTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for i, n := range names {
		info := AgentInfo{Status: harness.Status{Agent: n, Auth: harness.AuthUnknown}, Default: isDefault[n]}
		h, err := cfg.Harness(n)
		if err != nil {
			info.Detail = err.Error()
			resp.Agents[i] = info
			continue
		}
		info.Kind, info.Command = h.Kind(), h.Spec().Command
		resp.Agents[i] = info
		if !check {
			continue
		}
		wg.Add(1)
		go func(i int, h *harness.CommandHarness) {
			defer wg.Done()
			resp.Agents[i].Status = s.check(ctx, h, refresh)
		}(i, h)
	}
	wg.Wait()
	sort.SliceStable(resp.Agents, func(i, j int) bool { return resp.Agents[i].Default && !resp.Agents[j].Default })
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cancel, ok := s.runs[r.PathValue("id")]
	s.mu.Unlock()
	if !ok {
		httpError(w, http.StatusNotFound, "no active run %q", r.PathValue("id"))
		return
	}
	cancel()
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}
