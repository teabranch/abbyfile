package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAgentfile(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  string
	}{
		{
			name:  "only Agentfile",
			files: []string{"Agentfile"},
			want:  "Agentfile",
		},
		{
			name:  "only agentfile.yaml",
			files: []string{"agentfile.yaml"},
			want:  "agentfile.yaml",
		},
		{
			name:  "both present Agentfile wins",
			files: []string{"Agentfile", "agentfile.yaml"},
			want:  "Agentfile",
		},
		{
			name:  "neither present falls back to Agentfile",
			files: nil,
			want:  "Agentfile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			for _, f := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("agents: {}"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			// Run resolveAgentfile from inside the temp directory.
			orig, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chdir(orig) })

			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}

			got := resolveAgentfile()
			if got != tt.want {
				t.Errorf("resolveAgentfile() = %q, want %q", got, tt.want)
			}
		})
	}
}
