//go:build !windows

package harness

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// assertProcessGone waits up to two seconds for pid to exit. A zombie
// (killed, not yet reaped by init) counts as gone.
func assertProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		if st := strings.TrimSpace(string(out)); st == "" || strings.HasPrefix(st, "Z") {
			return
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("process %d (a grandchild of the harness) survived the timeout", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestConfigureProcess(t *testing.T) {
	cmd := exec.Command("true")
	configureProcess(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("child must get its own process group")
	}
	// Cancel before Start has nothing to kill.
	if err := cmd.Cancel(); err != nil {
		t.Fatalf("Cancel before start: %v", err)
	}
}
