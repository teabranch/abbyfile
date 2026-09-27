package builder

import (
	"fmt"
	"slices"

	"github.com/teabranch/abbyfile/pkg/builtins"
	"github.com/teabranch/abbyfile/pkg/definition"
)

// SandboxNotes returns warnings and notes about def's sandbox for
// `abby build` to print on stderr. Empty when nothing is noteworthy.
func SandboxNotes(def *definition.AgentDef) []string {
	var notes []string
	sb := def.Sandbox
	hasBash := slices.Contains(def.Tools, builtins.NameBash)
	switch {
	case sb != nil && sb.Bash == "unrestricted":
		notes = append(notes, fmt.Sprintf("warning: agent %q sets sandbox.bash: unrestricted; run_command runs any shell command with the user's permissions", def.Name))
	case hasBash && (sb == nil || len(sb.AllowCommands) == 0):
		notes = append(notes, fmt.Sprintf("note: agent %q declares Bash but sandbox.allow_commands is empty; run_command will refuse every call until commands are allowed (see docs/guides/tools.md#sandbox)", def.Name))
	}
	if sb != nil && slices.Contains(sb.AllowedDirs, "/") {
		notes = append(notes, fmt.Sprintf("warning: agent %q sandbox.allowed_dirs includes /; file tools can reach the whole filesystem", def.Name))
	}
	return notes
}
