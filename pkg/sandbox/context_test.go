package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestContextRoundTrip(t *testing.T) {
	s := mustNew(t, Config{AllowCommands: []string{"ls"}}, realTemp(t))
	if got := FromContext(NewContext(context.Background(), s)); got != s {
		t.Fatal("FromContext did not return the stored sandbox")
	}
}

func TestFromContextFallbackIsSecureDefault(t *testing.T) {
	s := FromContext(context.Background())
	if s == nil {
		t.Fatal("fallback must not be nil")
	}
	c := s.Config()
	if c.Bash != BashRestricted || len(c.AllowCommands) != 0 {
		t.Errorf("fallback config = %+v, want restricted with empty allowlist", c)
	}
	wd, _ := os.Getwd()
	wd, _ = filepath.EvalSymlinks(wd)
	if dirs := s.AllowedDirs(); len(dirs) != 1 || dirs[0] != wd {
		t.Errorf("fallback AllowedDirs = %q, want [%s]", dirs, wd)
	}
}
