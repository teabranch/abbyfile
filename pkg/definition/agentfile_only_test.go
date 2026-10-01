package definition

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAbbyfile_BinaryFlag(t *testing.T) {
	p := filepath.Join(t.TempDir(), "Abbyfile")
	content := `version: "1"
agents:
  compiled:
    path: a.md
    version: 1.0.0
  mdonly:
    path: b.md
    version: 1.0.0
    binary: false
`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	af, err := ParseAbbyfile(p)
	if err != nil {
		t.Fatalf("ParseAbbyfile: %v", err)
	}
	if !af.Agents["compiled"].BuildsBinary() {
		t.Error("compiled: BuildsBinary() = false, want true (the default)")
	}
	if af.Agents["mdonly"].BuildsBinary() {
		t.Error("mdonly: BuildsBinary() = true, want false")
	}
}

func TestCheckAgentFileOnly(t *testing.T) {
	ok := &AgentDef{Name: "a", Tools: []string{"Read", "Bash"}}
	if err := CheckAgentFileOnly(ok); err != nil {
		t.Fatalf("native-tools-only agent rejected: %v", err)
	}
	cases := map[string]*AgentDef{
		"custom_tools": {Name: "a", CustomTools: []CustomToolDef{{Name: "lint", Command: "echo"}}},
		"memory":       {Name: "a", Memory: true},
	}
	for want, def := range cases {
		err := CheckAgentFileOnly(def)
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "binary: false") {
			t.Errorf("%s: err = %v, want an error naming %s and binary: false", want, err, want)
		}
	}
}
