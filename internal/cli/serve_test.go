package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a goroutine-safe stderr for a server running in the background.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestServeFlagErrors(t *testing.T) {
	cfg := writeConfig(t, "defaults: {agents: [codex]}\n")
	bad := writeConfig(t, "defaults: [\n")
	cases := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"serve", "--config", cfg, "extra"}, 2, "serve takes no arguments"},
		{[]string{"serve", "--config", cfg, "--max-runs", "0"}, 2, "--max-runs must be at least 1"},
		{[]string{"serve", "--bogus"}, 1, "flag provided but not defined: -bogus"},
		{[]string{"serve", "--addr"}, 1, "flag needs an argument"},
		{[]string{"serve", "--config", bad}, 1, "council:"},
		{[]string{"serve", "--config", cfg, "--addr", "127.0.0.1:not-a-port"}, 1, "council:"},
	}
	for _, c := range cases {
		_, errOut, code := runCLI(t, "", c.args...)
		if code != c.code || !strings.Contains(errOut, c.want) {
			t.Errorf("%v: exit %d, stderr:\n%s", c.args, code, errOut)
		}
	}
	if _, errOut, code := runCLI(t, "", "serve", "-h"); code != 0 || !strings.Contains(errOut, "-addr string") || !strings.Contains(errOut, "-open") {
		t.Errorf("serve -h: %d\n%s", code, errOut)
	}
	if out, _, _ := runCLI(t, "", "help"); !strings.Contains(out, "council serve") {
		t.Error("usage should mention serve")
	}
}

// startServe runs `council serve args...` until the returned stop function is
// called, and waits for the startup line.
func startServe(t *testing.T, args ...string) (url string, stderr *syncBuffer, stop func() error) {
	t.Helper()
	stderr = &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, append([]string{"serve"}, args...), Env{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: stderr})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s := stderr.String()
		if i := strings.Index(s, "council web UI on "); i >= 0 {
			url = strings.Fields(s[i+len("council web UI on "):])[0]
			break
		}
		select {
		case err := <-done:
			cancel()
			t.Fatalf("serve exited early: %v\n%s", err, s)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("serve did not start:\n%s", s)
		}
		time.Sleep(5 * time.Millisecond)
	}
	var once sync.Once
	var err error
	stop = func() error {
		once.Do(func() {
			cancel()
			select {
			case err = <-done:
			case <-time.After(10 * time.Second):
				err = errors.New("serve did not stop")
			}
		})
		return err
	}
	t.Cleanup(func() { _ = stop() })
	return url, stderr, stop
}

func TestServeRunsAndStops(t *testing.T) {
	cfg := writeConfig(t, "defaults: {agents: [codex]}\n")
	var opened []string
	old := openBrowser
	t.Cleanup(func() { openBrowser = old })
	openBrowser = func(u string) error { opened = append(opened, u); return nil }

	url, stderr, stop := startServe(t, "--config", cfg, "--addr", "127.0.0.1:0", "--open", "--max-runs", "3")
	if !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.HasSuffix(url, "/") {
		t.Fatalf("url %q", url)
	}
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(body), "council-token") {
		t.Errorf("GET /: %d", res.StatusCode)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || opened[0] != url {
		t.Errorf("opened %v", opened)
	}
	s := stderr.String()
	if strings.Contains(s, "WARNING") || !strings.Contains(s, "(config "+cfg+")") || !strings.Contains(s, "council web UI stopped") {
		t.Errorf("stderr:\n%s", s)
	}
}

func TestServeLocalhostURLAndOpenFailure(t *testing.T) {
	cfg := writeConfig(t, "defaults: {agents: [codex]}\n")
	old := openBrowser
	t.Cleanup(func() { openBrowser = old })
	openBrowser = func(string) error { return errors.New("no display") }
	url, stderr, stop := startServe(t, "--config", cfg, "--addr", "localhost:0", "--open")
	if !strings.HasPrefix(url, "http://localhost:") || strings.HasSuffix(url, ":/") {
		t.Errorf("url %q", url)
	}
	stop()
	if !strings.Contains(stderr.String(), "could not open a browser: no display") {
		t.Errorf("stderr:\n%s", stderr)
	}
}

func TestServeWarnsWhenNotLoopback(t *testing.T) {
	cfg := writeConfig(t, "defaults: {agents: [codex]}\n")
	old := listen
	t.Cleanup(func() { listen = old })
	var asked string
	// Never actually expose a test server: pretend 0.0.0.0 by listening on loopback.
	listen = func(network, addr string) (net.Listener, error) {
		asked = addr
		return old(network, "127.0.0.1:0")
	}
	url, stderr, stop := startServe(t, "--config", cfg, "--addr", "0.0.0.0:0")
	if asked != "0.0.0.0:0" {
		t.Errorf("listen got %q", asked)
	}
	// AllowAnyHost is on, so a non-localhost Host header is accepted.
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Host = "council.lan:8787"
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("status %d", res.StatusCode)
	}
	stop()
	if !strings.Contains(stderr.String(), "WARNING: listening on") {
		t.Errorf("stderr:\n%s", stderr)
	}
}

func TestServeListenerFailure(t *testing.T) {
	cfg := writeConfig(t, "defaults: {agents: [codex]}\n")
	old := listen
	t.Cleanup(func() { listen = old })
	listen = func(network, addr string) (net.Listener, error) {
		ln, err := old(network, "127.0.0.1:0")
		if err == nil {
			ln.Close() // Serve fails at once
		}
		return ln, err
	}
	err := run(context.Background(), []string{"serve", "--config", cfg}, Env{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: &syncBuffer{}})
	if err == nil {
		t.Fatal("serving on a closed listener should fail")
	}
}
