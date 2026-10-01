package runtimecfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Env values are often secrets: every entry diff shows them as ***, while
// the written entry carries the real values and a change to a value alone
// is still a change (marked, not shown).
func TestFilePreviewRedactsEnv(t *testing.T) {
	for _, r := range []Runtime{ClaudeCode, Codex, Gemini} {
		t.Run(string(r), func(t *testing.T) {
			chdirTemp(t)
			w := For(r, fileOpts)
			e := entry()
			e.Env = map[string]string{"TOKEN": "oldsecret", "KEEP": "samesecret"}
			c, err := w.PlanAdd(ScopeProject, "agent", e)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(c.Preview, "oldsecret") || strings.Contains(c.Preview, "samesecret") || !strings.Contains(c.Preview, "***") {
				t.Errorf("add preview must redact env values:\n%s", c.Preview)
			}
			if _, err := c.Apply(); err != nil {
				t.Fatal(err)
			}
			path, _ := w.ConfigPath(ScopeProject)
			if b, _ := os.ReadFile(path); !strings.Contains(string(b), "oldsecret") {
				t.Fatalf("written entry must carry the real value:\n%s", b)
			}

			e.Env = map[string]string{"TOKEN": "newsecret", "KEEP": "samesecret"}
			c, err = w.PlanAdd(ScopeProject, "agent", e)
			if err != nil {
				t.Fatal(err)
			}
			if c.Noop {
				t.Fatal("changing only an env value must not be a no-op")
			}
			for _, s := range []string{"oldsecret", "newsecret", "samesecret"} {
				if strings.Contains(c.Preview, s) {
					t.Errorf("update preview leaks %q:\n%s", s, c.Preview)
				}
			}
			if !strings.Contains(c.Preview, redactedChanged) {
				t.Errorf("update preview must mark the changed value:\n%s", c.Preview)
			}
			if _, err := c.Apply(); err != nil {
				t.Fatal(err)
			}
			if b, _ := os.ReadFile(path); !strings.Contains(string(b), "newsecret") {
				t.Fatalf("written entry must carry the new value:\n%s", b)
			}

			c, err = w.PlanRemove(ScopeProject, "agent")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(c.Preview, "newsecret") || !strings.Contains(c.Preview, "***") {
				t.Errorf("remove preview must redact env values:\n%s", c.Preview)
			}
		})
	}
}

func TestCLIPreviewDiffRedactsEnv(t *testing.T) {
	d := chdirTemp(t)
	os.WriteFile(filepath.Join(d, ".mcp.json"), []byte(`{"mcpServers":{"agent":{"type":"stdio","command":"/old","args":[],"env":{"K":"oldsecret"}}}}`), 0o600)
	var calls []call
	opts := cliOpts(&calls, nil, "claude")
	e := entry()
	e.Env = map[string]string{"K": "newsecret"}
	c, err := For(ClaudeCode, opts).PlanAdd(ScopeProject, "agent", e)
	if err != nil || c.Method != MethodCLI {
		t.Fatalf("change = %+v, %v", c, err)
	}
	if strings.Contains(c.Preview, "oldsecret") || strings.Contains(c.Preview, "newsecret") {
		t.Errorf("CLI add preview leaks an env value:\n%s", c.Preview)
	}
	c, err = For(ClaudeCode, opts).PlanRemove(ScopeProject, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c.Preview, "oldsecret") {
		t.Errorf("CLI remove preview leaks an env value:\n%s", c.Preview)
	}
}
