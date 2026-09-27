package sandbox

import (
	"strings"
	"testing"
)

func TestAllowRuleMatches(t *testing.T) {
	tests := []struct {
		entry string
		argv  []string
		want  bool
	}{
		{"go test", []string{"go", "test"}, true},
		{"go test", []string{"go", "test", "./..."}, false},
		{"go test", []string{"go"}, false},
		{"go test *", []string{"go", "test", "./...", "-run", "X"}, true},
		{"go test *", []string{"go", "test"}, true},
		{"go test *", []string{"go", "vet", "./..."}, false},
		{"make *", []string{"make"}, true},
		{"make *", []string{"makes"}, false},
		{"git status", []string{"/usr/bin/git", "status"}, false},
		{"'my tool' run", []string{"my tool", "run"}, true},
	}
	for _, tt := range tests {
		r, err := ParseAllowEntry(tt.entry)
		if err != nil {
			t.Fatalf("ParseAllowEntry(%q): %v", tt.entry, err)
		}
		if got := r.Matches(tt.argv); got != tt.want {
			t.Errorf("%q.Matches(%q) = %v, want %v", tt.entry, tt.argv, got, tt.want)
		}
	}
}

func TestParseAllowEntry_Rejects(t *testing.T) {
	tests := map[string]string{
		"":            "empty",
		"*":           "bare *",
		"go * test":   "last word",
		"go test; rm": "operator",
		"go test | x": "operator",
	}
	for entry, wantSub := range tests {
		_, err := ParseAllowEntry(entry)
		if err == nil || !strings.Contains(err.Error(), wantSub) {
			t.Errorf("ParseAllowEntry(%q) error = %v, want it to contain %q", entry, err, wantSub)
		}
	}
}
