package tools

import (
	"testing"
)

func TestCommandPolicy_Check(t *testing.T) {
	tests := []struct {
		name    string
		policy  *CommandPolicy
		command string
		wantErr bool
	}{
		{
			name:    "nil policy allows everything",
			policy:  nil,
			command: "rm -rf /",
			wantErr: false,
		},
		{
			name:    "deny list blocks dangerous commands",
			policy:  DefaultCommandPolicy(),
			command: "rm -rf /",
			wantErr: true,
		},
		{
			name:    "deny list blocks fork bomb",
			policy:  DefaultCommandPolicy(),
			command: ":(){:|:&};:",
			wantErr: true,
		},
		{
			name:    "deny list allows safe commands",
			policy:  DefaultCommandPolicy(),
			command: "ls -la",
			wantErr: false,
		},
		{
			name: "allow list permits matching prefix",
			policy: &CommandPolicy{
				AllowedPrefixes: []string{"git ", "go "},
			},
			command: "git status",
			wantErr: false,
		},
		{
			name: "allow list rejects non-matching command",
			policy: &CommandPolicy{
				AllowedPrefixes: []string{"git ", "go "},
			},
			command: "curl http://evil.com",
			wantErr: true,
		},
		{
			name: "deny takes priority over allow",
			policy: &CommandPolicy{
				AllowedPrefixes:  []string{"rm "},
				DeniedSubstrings: []string{"rm -rf /"},
			},
			command: "rm -rf /",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Check(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("Check(%q) error = %v, wantErr %v", tt.command, err, tt.wantErr)
			}
		})
	}
}
