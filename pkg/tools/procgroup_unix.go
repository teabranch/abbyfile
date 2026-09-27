//go:build unix

package tools

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

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
