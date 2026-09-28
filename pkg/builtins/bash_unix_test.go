//go:build unix

package builtins

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

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

// Review Focus #5.
func TestRunCommand_CancelKillsGroup(t *testing.T) {
	root := realTempDir(t)
	pidFile := filepath.Join(root, "pid")
	base := sandboxCtx(t, root, sandbox.Config{Bash: sandbox.BashUnrestricted})
	ctx, cancel := context.WithCancel(base)
	done := make(chan error, 1)
	go func() {
		_, err := handleRunCommand(ctx, map[string]any{"command": "sleep 60 & echo $! > " + pidFile + "; wait"})
		done <- err
	}()
	var pid int
	for i := 0; i < 100 && pid == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
	}
	if pid == 0 {
		t.Fatal("pid never written")
	}
	cancel()
	<-done
	for i := 0; i < 60; i++ {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("grandchild %d survived cancel", pid)
}

// TestRunCommand_ReapsGrandchildOnNormalExit covers the case where the direct
// child (sh) exits normally, without ever hitting the context deadline, while
// a background grandchild ("sleep 60") keeps stdout/stderr open. cmd.Cancel
// never runs on this path, so only an explicit KillProcessGroup call after
// cmd.Run() returns reaps the grandchild. Without it, cmd.Run would block
// until the process group's WaitDelay elapses, return exec.ErrWaitDelay, and
// handleRunCommand would misreport the command as failed even though it
// completed successfully — this is the controller ruling under test.
func TestRunCommand_ReapsGrandchildOnNormalExit(t *testing.T) {
	root := realTempDir(t)
	pidFile := filepath.Join(root, "pid")
	ctx := sandboxCtx(t, root, sandbox.Config{Bash: sandbox.BashUnrestricted})

	start := time.Now()
	out, err := handleRunCommand(ctx, map[string]any{"command": "sleep 60 & echo $! > " + pidFile + "; echo done"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("handleRunCommand err = %v, want nil (child exited successfully)", err)
	}
	if strings.TrimSpace(out) != "done" {
		t.Fatalf("out = %q, want %q", out, "done")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("handleRunCommand took %s, want < 5s", elapsed)
	}

	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("pid file contents %q: %v", b, err)
	}
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })

	for i := 0; i < 60; i++ {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("grandchild %d survived normal exit", pid)
}
