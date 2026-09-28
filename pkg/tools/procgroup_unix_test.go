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
//
// The elapsed-time assertion is load-bearing: without process-group kill,
// cmd.Wait blocks on the stdout/stderr pipes the orphaned "sleep 60"
// grandchild still holds open until it exits on its own (~60s), then
// waitGone would trivially pass because the grandchild is finally gone. The
// wall-clock check catches that: a correct process-group kill returns in
// ~300ms (the executor timeout), well under the 1.5s ceiling.
func TestExecutorCLIKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	def := &Definition{
		Name: "spawner", Command: "sh",
		Args: []string{"-c", "sleep 60 & echo $! > " + pidFile + "; wait"},
	}
	start := time.Now()
	_, err := NewExecutor(300*time.Millisecond, nil).RunRaw(context.Background(), def, nil)
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want timeout", err)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("RunRaw took %s; process-group kill must return promptly (~300ms), not wait for the orphaned grandchild to exit on its own", elapsed)
	}
	waitGone(t, readPid(t, pidFile))
}

// TestExecutorCLIReapsGrandchildOnNormalExit covers the case where the
// direct child exits normally (not via context cancellation) while a
// background grandchild keeps stdout/stderr open. cmd.Cancel never runs, so
// only an explicit KillProcessGroup call after Wait returns reaps the
// grandchild; without it, cmd.Wait would return exec.ErrWaitDelay after the
// process group's WaitDelay elapses and the command would be misreported as
// failed even though it succeeded.
func TestExecutorCLIReapsGrandchildOnNormalExit(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	def := &Definition{
		Name: "spawner", Command: "sh",
		Args: []string{"-c", "sleep 60 & echo $! > " + pidFile + "; echo done"},
	}
	start := time.Now()
	got, err := NewExecutor(10*time.Second, nil).RunRaw(context.Background(), def, nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunRaw err = %v, want nil (child exited successfully)", err)
	}
	if got != "done" {
		t.Fatalf("RunRaw = %q, want %q", got, "done")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("RunRaw took %s, want < 5s", elapsed)
	}
	waitGone(t, readPid(t, pidFile))
}
