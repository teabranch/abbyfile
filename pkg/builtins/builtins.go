// Package builtins provides shared tool implementations that generated
// agent binaries can reference by Claude Code tool name.
package builtins

import (
	"fmt"

	"github.com/teabranch/agentfile/pkg/tools"
)

// Claude Code tool names that agents can declare in their .md frontmatter.
const (
	NameRead  = "Read"
	NameWrite = "Write"
	NameEdit  = "Edit"
	NameBash  = "Bash"
	NameGlob  = "Glob"
	NameGrep  = "Grep"
)

var registry = map[string]func() *tools.Definition{
	NameRead:  ReadFileTool,
	NameWrite: WriteFileTool,
	NameEdit:  EditFileTool,
	NameBash:  RunCommandTool,
	NameGlob:  GlobFilesTool,
	NameGrep:  GrepSearchTool,
}

// All returns definitions for every built-in tool.
func All() []*tools.Definition {
	defs := make([]*tools.Definition, 0, len(registry))
	for _, fn := range registry {
		defs = append(defs, fn())
	}
	return defs
}

// ForNames returns definitions for the given Claude Code tool names.
// Returns an error if any name is unknown.
func ForNames(names []string) ([]*tools.Definition, error) {
	defs := make([]*tools.Definition, 0, len(names))
	for _, name := range names {
		fn, ok := registry[name]
		if !ok {
			return nil, fmt.Errorf("unknown builtin tool: %q (valid: Read, Write, Edit, Bash, Glob, Grep)", name)
		}
		defs = append(defs, fn())
	}
	return defs, nil
}
