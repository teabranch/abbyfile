//go:build unix

package tools

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// KillProcessGroup reaps any processes the tool left behind in its group;
// call after Wait returns. It sends SIGKILL to the group and ignores ESRCH
// (the group is already gone) — there is nothing else to report or do.
func KillProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// waitDelay bounds how long Wait blocks on inherited pipes after a kill.
const waitDelay = 2 * time.Second

// ConfigureProcessGroup starts cmd in its own process group and makes
// context cancellation kill the whole group; exec.CommandContext alone
// kills only the direct child and orphans grandchildren.
func ConfigureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone // exited at the deadline; not a failure
			}
			return err
		}
		return nil
	}
	cmd.WaitDelay = waitDelay
}
