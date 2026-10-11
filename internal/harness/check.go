package harness

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// Check reports whether the harness CLI is installed and signed in. It never
// reads credentials itself; it only asks the CLI (or checks for the presence
// of its documented credential sources).
func (h *CommandHarness) Check(ctx context.Context) Status {
	st := Status{Agent: h.name, Kind: h.spec.Kind, Command: h.spec.Command, Auth: AuthUnknown}
	path, err := exec.LookPath(h.spec.Command)
	if err != nil {
		st.Detail = "not found on PATH"
		return st
	}
	st.Installed, st.Path = true, path

	if out, err := h.runQuiet(ctx, 15*time.Second, "--version"); err == nil {
		st.Version = firstLine(out)
	}

	switch {
	case len(h.spec.StatusArgs) > 0:
		out, err := h.runQuiet(ctx, 30*time.Second, h.spec.StatusArgs...)
		if err == nil {
			st.Auth = AuthOK
		} else {
			st.Auth = AuthMissing
		}
		st.Detail = firstLine(out)
		if st.Detail == "" && err != nil {
			st.Detail = err.Error()
		}
	case h.spec.statusFunc != nil:
		st.Auth, st.Detail = h.spec.statusFunc(ctx)
	default:
		st.Detail = "no status command configured"
	}
	return st
}

func (h *CommandHarness) runQuiet(ctx context.Context, d time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.spec.Command, args...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	configureProcess(cmd)
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	return strings.TrimSpace(buf.String()), err
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if n := 200; len(s) > n {
		for n > 0 && !utf8.RuneStart(s[n]) {
			n-- // do not split a multi-byte character
		}
		s = s[:n] + "…"
	}
	return s
}
