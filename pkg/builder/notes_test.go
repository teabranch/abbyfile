package builder

import (
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/definition"
)

// Review Focus #2.
func TestSandboxNotes(t *testing.T) {
	tests := []struct {
		name string
		def  *definition.AgentDef
		want []string // substrings, one per expected note; nil = no notes
	}{
		{"bash no sandbox", &definition.AgentDef{Name: "a", Tools: []string{"Read", "Bash"}}, []string{"run_command will refuse every call"}},
		{"bash empty allowlist", &definition.AgentDef{Name: "a", Tools: []string{"Bash"}, Sandbox: &definition.SandboxDef{}}, []string{"refuse every call"}},
		{"bash allowlisted", &definition.AgentDef{Name: "a", Tools: []string{"Bash"}, Sandbox: &definition.SandboxDef{AllowCommands: []string{"ls"}}}, nil},
		{"unrestricted", &definition.AgentDef{Name: "a", Tools: []string{"Bash"}, Sandbox: &definition.SandboxDef{Bash: "unrestricted"}}, []string{"unrestricted"}},
		{"root dir", &definition.AgentDef{Name: "a", Tools: []string{"Read"}, Sandbox: &definition.SandboxDef{AllowedDirs: []string{"/"}}}, []string{"allowed_dirs includes /"}},
		{"no bash", &definition.AgentDef{Name: "a", Tools: []string{"Read"}}, nil},
	}
	for _, tt := range tests {
		got := SandboxNotes(tt.def)
		if len(got) != len(tt.want) {
			t.Errorf("%s: notes = %q, want %d", tt.name, got, len(tt.want))
			continue
		}
		for i, sub := range tt.want {
			if !strings.Contains(got[i], sub) || !strings.Contains(got[i], `"a"`) {
				t.Errorf("%s: note %q should mention %q and the agent name", tt.name, got[i], sub)
			}
		}
	}
}
