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
