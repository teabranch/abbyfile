package tools

import (
	"fmt"
	"strings"
)

// CommandPolicy defines execution constraints for custom CLI tools.
// AllowedPrefixes and DeniedSubstrings are plain string checks on the
// argument string — conveniences, not a security boundary. run_command is
// governed by pkg/sandbox instead.
type CommandPolicy struct {
	AllowedPrefixes  []string // if non-empty, command must start with one of these
	DeniedSubstrings []string // command must not contain any of these
	MaxOutputBytes   int64    // cap on captured stdout/stderr each (<=0 = DefaultMaxOutputBytes)
}

// DefaultCommandPolicy returns the default policy: no string checks and a
// DefaultMaxOutputBytes capture cap.
func DefaultCommandPolicy() *CommandPolicy {
	return &CommandPolicy{MaxOutputBytes: DefaultMaxOutputBytes}
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
