package sandbox

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitArgv(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{`go test ./...`, []string{"go", "test", "./..."}},
		{`  go   test  `, []string{"go", "test"}},
		{`echo 'a b' "c d"`, []string{"echo", "a b", "c d"}},
		{`echo "a\"b"`, []string{"echo", `a"b`}},
		{`echo "a\qb"`, []string{"echo", `a\qb`}},
		{`echo a\ b`, []string{"echo", "a b"}},
		{`echo ''`, []string{"echo", ""}},
		{`echo 'x;y|z'`, []string{"echo", "x;y|z"}},
		{`echo "$(id)"`, []string{"echo", "$(id)"}},
		{`echo $HOME`, []string{"echo", "$HOME"}},
		{`echo a"b"'c'`, []string{"echo", "abc"}},
		{"echo\ta", []string{"echo", "a"}},
	}
	for _, tt := range tests {
		got, err := SplitArgv(tt.in)
		if err != nil {
			t.Errorf("SplitArgv(%q) error: %v", tt.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitArgv(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitArgv_Rejects(t *testing.T) {
	tests := map[string]string{
		`a; b`:       "operator",
		`a && b`:     "operator",
		`a & b`:      "operator",
		`a | b`:      "operator",
		"a `id`":     "operator",
		`a $(id)`:    "substitution",
		`a > f`:      "operator",
		`a < f`:      "operator",
		"a\nb":       "newline",
		"a\\\nb":     "newline",
		`echo 'open`: "unterminated",
		`echo "open`: "unterminated",
		`echo \`:     "backslash",
		``:           "empty",
		`   `:        "empty",
	}
	for in, wantSub := range tests {
		_, err := SplitArgv(in)
		if err == nil {
			t.Errorf("SplitArgv(%q): expected error containing %q", in, wantSub)
			continue
		}
		if !strings.Contains(err.Error(), wantSub) {
			t.Errorf("SplitArgv(%q) error = %q, want it to contain %q", in, err, wantSub)
		}
	}
}
