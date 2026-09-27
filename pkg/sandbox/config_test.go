package sandbox

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultAndNormalize(t *testing.T) {
	d := Default()
	if len(d.AllowedDirs) != 1 || d.AllowedDirs[0] != "." {
		t.Errorf("Default().AllowedDirs = %q, want [.]", d.AllowedDirs)
	}
	if d.Bash != BashRestricted || d.MaxCommandTimeout != 120*time.Second || len(d.AllowCommands) != 0 {
		t.Errorf("Default() = %+v", d)
	}
	n := Config{}.Normalize()
	if len(n.AllowedDirs) != 1 || n.Bash != BashRestricted || n.MaxCommandTimeout != DefaultMaxCommandTimeout {
		t.Errorf("zero Config normalized = %+v", n)
	}
	kept := Config{AllowedDirs: []string{}}.Normalize()
	if kept.AllowedDirs == nil || len(kept.AllowedDirs) != 0 {
		t.Errorf("explicit empty AllowedDirs must survive Normalize, got %#v", kept.AllowedDirs)
	}
}

func TestValidate(t *testing.T) {
	bad := map[string]Config{
		"empty allowed_dirs": {AllowedDirs: []string{}},
		"blank dir":          {AllowedDirs: []string{"  "}},
		"bad bash":           {Bash: "yolo"},
		"negative timeout":   {MaxCommandTimeout: -time.Second},
		"bad entry":          {AllowCommands: []string{"go test; rm -rf /"}},
	}
	for name, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: Validate() = nil, want error", name)
		} else if !strings.Contains(err.Error(), "sandbox") {
			t.Errorf("%s: error %q should name the sandbox key", name, err)
		}
	}
	if err := Default().Validate(); err != nil {
		t.Errorf("Default().Validate() = %v", err)
	}
}
