//go:build !unix

package tools

import (
	"os/exec"
	"time"
)

// ConfigureProcessGroup on non-unix platforms only bounds Wait; process
// groups are not portable there. Agents are built for darwin and linux.
func ConfigureProcessGroup(cmd *exec.Cmd) {
	cmd.WaitDelay = 2 * time.Second
}

// KillProcessGroup reaps any processes the tool left behind in its group;
// call after Wait returns. It is a no-op on non-unix platforms; process
// groups are not portable there.
func KillProcessGroup(cmd *exec.Cmd) {}
