package tools

import (
	"fmt"
	"strings"
)

// CommandPolicy defines execution constraints for CLI tools.
type CommandPolicy struct {
	AllowedPrefixes  []string // if non-empty, command must start with one of these
	DeniedSubstrings []string // command must not contain any of these
	MaxOutputBytes   int64    // max stdout+stderr size (0 = unlimited)
}

// DefaultCommandPolicy returns a policy with sensible defaults for run_command.
func DefaultCommandPolicy() *CommandPolicy {
	return &CommandPolicy{
		DeniedSubstrings: []string{
			"rm -rf /",
			"rm -rf ~",
			"mkfs.",
			"dd if=",
			":(){", // fork bomb
			"chmod -R 777",
		},
		MaxOutputBytes: 10 << 20, // 10 MB
	}
}

// Check validates a command string against the policy. Returns an error if denied.
func (p *CommandPolicy) Check(command string) error {
	if p == nil {
		return nil
	}

	// Check deny list first.
	for _, denied := range p.DeniedSubstrings {
		if strings.Contains(command, denied) {
			return fmt.Errorf("command denied: contains %q", denied)
		}
	}

	// Check allow list (if configured).
	if len(p.AllowedPrefixes) > 0 {
		allowed := false
		for _, prefix := range p.AllowedPrefixes {
			if strings.HasPrefix(command, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("command denied: does not match any allowed prefix")
		}
	}

	return nil
}
