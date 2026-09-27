//go:build unix

package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// waitGone polls until pid no longer exists (ESRCH) or the deadline passes.
func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("grandchild %d survived cancellation", pid)
}

func readPid(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("pid file never written")
	return 0
}

// Review Focus #5.
func TestExecutorCLIKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	def := &Definition{
		Name: "spawner", Command: "sh",
		Args: []string{"-c", "sleep 60 & echo $! > " + pidFile + "; wait"},
	}
	_, err := NewExecutor(300*time.Millisecond, nil).RunRaw(context.Background(), def, nil)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want timeout", err)
	}
	waitGone(t, readPid(t, pidFile))
}
