//go:build windows

package harness

import "testing"

// assertProcessGone is a no-op on Windows, where the shell-script fakes that
// spawn child processes are skipped.
func assertProcessGone(t *testing.T, pid int) { t.Helper() }
