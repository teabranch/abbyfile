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

func TestDefaultCommandPolicy_NoDenylist(t *testing.T) {
	p := DefaultCommandPolicy()
	if len(p.DeniedSubstrings) != 0 {
		t.Errorf("default denylist must be gone (it was never a security boundary), got %q", p.DeniedSubstrings)
	}
	if p.MaxOutputBytes != DefaultMaxOutputBytes {
		t.Errorf("MaxOutputBytes = %d, want %d", p.MaxOutputBytes, DefaultMaxOutputBytes)
	}
}
