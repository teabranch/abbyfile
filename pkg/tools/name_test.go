package tools

import (
	"strings"
	"testing"
)

func TestValidateToolName(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"read_file", true},
		{"a.b-c_D9", true},
		{strings.Repeat("x", 128), true},
		{"", false},
		{strings.Repeat("x", 129), false},
		{"has space", false},
		{"slash/name", false},
		{"émoji", false},
		{"colon:name", false},
	}
	for _, c := range cases {
		err := ValidateToolName(c.name)
		if (err == nil) != c.ok {
			t.Errorf("ValidateToolName(%q) err=%v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestRegisterRejectsInvalidName(t *testing.T) {
	r := NewRegistry()
	err := r.Register(&Definition{Name: "bad name", Builtin: true})
	if err == nil || !strings.Contains(err.Error(), "A-Za-z0-9_.-") {
		t.Fatalf("Register err = %v, want SEP-986 message", err)
	}
}
