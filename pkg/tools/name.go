package tools

import (
	"fmt"
	"regexp"
)

// toolNamePattern is the MCP tool-name rule (SEP-986, spec 2025-11-25+).
var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// ValidateToolName reports whether name is a legal MCP tool name.
func ValidateToolName(name string) error {
	if !toolNamePattern.MatchString(name) {
		return fmt.Errorf("tool name %q is invalid: must be 1-128 characters from [A-Za-z0-9_.-]", name)
	}
	return nil
}
