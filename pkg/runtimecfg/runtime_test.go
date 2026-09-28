package runtimecfg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAndParseMethod(t *testing.T) {
	if r, err := Parse("codex"); err != nil || r != Codex {
		t.Errorf("Parse(codex) = %v %v", r, err)
	}
	if _, err := Parse("nope"); err == nil {
		t.Error("Parse(nope) must fail")
	}
	for _, m := range []string{"auto", "cli", "file"} {
		if _, err := ParseMethod(m); err != nil {
			t.Errorf("ParseMethod(%s): %v", m, err)
		}
	}
	if _, err := ParseMethod("magic"); err == nil {
		t.Error("ParseMethod(magic) must fail")
	}
}

func TestResolve(t *testing.T) {
	if ws, _ := Resolve("all", fileOpts); len(ws) != 3 {
		t.Errorf("all = %d", len(ws))
	}
	if ws, _ := Resolve("gemini", fileOpts); len(ws) != 1 || ws[0].Runtime() != Gemini {
		t.Error("gemini")
	}
	if _, err := Resolve("nope", fileOpts); err == nil {
		t.Error("nope must fail")
	}
}

// C3: detection is by CLI on PATH or runtime config dir, never by $HOME existing.
func TestDetect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	none := Options{Method: MethodFile, LookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	if ws := Detect(none); len(ws) != 1 || ws[0].Runtime() != ClaudeCode {
		t.Errorf("fallback = %v, want [claude-code]", ws)
	}
	os.MkdirAll(filepath.Join(home, ".gemini"), 0o755)
	if ws := Detect(none); len(ws) != 1 || ws[0].Runtime() != Gemini {
		t.Errorf("gemini dir → %v", ws)
	}
	onPath := Options{Method: MethodFile, LookPath: func(n string) (string, error) {
		if n == "codex" {
			return "/fake/codex", nil
		}
		return "", os.ErrNotExist
	}}
	got := map[Runtime]bool{}
	for _, w := range Detect(onPath) {
		got[w.Runtime()] = true
	}
	if !got[Codex] || !got[Gemini] || got[ClaudeCode] {
		t.Errorf("detect = %v, want codex (PATH) + gemini (dir) only", got)
	}
}

func TestLegacyClaudePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if p, _ := LegacyClaudePath(); p != filepath.Join(home, ".claude", "mcp.json") {
		t.Errorf("LegacyClaudePath = %q", p)
	}
}
