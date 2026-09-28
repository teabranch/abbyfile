package definition

import (
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

const dualWithSandbox = `---
name: sb
---

---
description: "d"
tools: Read, Bash
sandbox:
  allowed_dirs: [".", "/tmp/work"]
  bash: restricted
  allow_commands: ["go test *", "git status"]
  max_command_timeout: 45s
---

body
`

func TestParseAgentMD_Sandbox_Dual(t *testing.T) {
	def, err := ParseAgentMD(writeTempAgent(t, dualWithSandbox))
	if err != nil {
		t.Fatal(err)
	}
	s := def.Sandbox
	if s == nil || len(s.AllowedDirs) != 2 || s.Bash != "restricted" || len(s.AllowCommands) != 2 || s.MaxCommandTimeout != "45s" {
		t.Fatalf("Sandbox = %+v", s)
	}
	cfg, err := s.ToConfig()
	if err != nil || cfg.MaxCommandTimeout != 45*time.Second || cfg.Bash != sandbox.BashRestricted {
		t.Fatalf("ToConfig = %+v, %v", cfg, err)
	}
}

func TestParseAgentMD_Sandbox_Single(t *testing.T) {
	md := "---\nname: sb\ndescription: d\nabbyfile:\n  tools: [Bash]\n  sandbox:\n    bash: unrestricted\n---\n\nbody\n"
	def, err := ParseAgentMD(writeTempAgent(t, md))
	if err != nil || def.Sandbox == nil || def.Sandbox.Bash != "unrestricted" {
		t.Fatalf("def.Sandbox = %+v, err %v", def.Sandbox, err)
	}
}

func TestParseAgentMD_Sandbox_Omitted(t *testing.T) {
	md := strings.Replace(dualWithSandbox, "sandbox:\n  allowed_dirs: [\".\", \"/tmp/work\"]\n  bash: restricted\n  allow_commands: [\"go test *\", \"git status\"]\n  max_command_timeout: 45s\n", "", 1)
	def, err := ParseAgentMD(writeTempAgent(t, md))
	if err != nil || def.Sandbox != nil {
		t.Fatalf("omitted sandbox must be nil, got %+v, %v", def.Sandbox, err)
	}
}

func TestParseAgentMD_Sandbox_Invalid(t *testing.T) {
	cases := map[string]string{
		"bash":     "sandbox:\n  bash: yolo\n",
		"entry":    "sandbox:\n  allow_commands: [\"go test | tee x\"]\n",
		"duration": "sandbox:\n  max_command_timeout: soon\n",
		"zero dur": "sandbox:\n  max_command_timeout: 0s\n",
		"no dirs":  "sandbox:\n  allowed_dirs: []\n",
	}
	for name, block := range cases {
		md := "---\nname: sb\n---\n\n---\ndescription: d\ntools: Bash\n" + block + "---\n\nbody\n"
		if _, err := ParseAgentMD(writeTempAgent(t, md)); err == nil || !strings.Contains(err.Error(), "sandbox") {
			t.Errorf("%s: err = %v, want a sandbox error", name, err)
		}
	}
}
