package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAbbyfile(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  string
	}{
		{
			name:  "only Abbyfile",
			files: []string{"Abbyfile"},
			want:  "Abbyfile",
		},
		{
			name:  "only abbyfile.yaml",
			files: []string{"abbyfile.yaml"},
			want:  "abbyfile.yaml",
		},
		{
			name:  "both present Abbyfile wins",
			files: []string{"Abbyfile", "abbyfile.yaml"},
			want:  "Abbyfile",
		},
		{
			name:  "neither present falls back to Abbyfile",
			files: nil,
			want:  "Abbyfile",
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

			// Run resolveAbbyfile from inside the temp directory.
			orig, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chdir(orig) })

			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}

			got := resolveAbbyfile()
			if got != tt.want {
				t.Errorf("resolveAbbyfile() = %q, want %q", got, tt.want)
			}
		})
	}
}
