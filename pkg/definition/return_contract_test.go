package definition

import (
	"strings"
	"testing"
)

func TestParseAgentMD_ReturnContract_Dual(t *testing.T) {
	md := `---
name: my-agent
---

---
description: "test"
tools: Read
return_contract:
  fields:
    - name: verdict
      description: pass or fail, and why
    - name: proof
---

Prompt body.`
	def, err := ParseAgentMD(writeTempAgent(t, md))
	if err != nil {
		t.Fatalf("ParseAgentMD: %v", err)
	}
	want := []ReturnField{{Name: "verdict", Description: "pass or fail, and why"}, {Name: "proof"}}
	if len(def.ReturnFields) != 2 || def.ReturnFields[0] != want[0] || def.ReturnFields[1] != want[1] {
		t.Fatalf("ReturnFields = %+v, want %+v", def.ReturnFields, want)
	}
}

func TestParseAgentMD_ReturnContract_Single(t *testing.T) {
	md := `---
name: my-agent
description: test
abbyfile:
  tools: [Read]
  return_contract:
    fields:
      - name: verdict
---

Prompt body.`
	def, err := ParseAgentMD(writeTempAgent(t, md))
	if err != nil {
		t.Fatalf("ParseAgentMD: %v", err)
	}
	if len(def.ReturnFields) != 1 || def.ReturnFields[0].Name != "verdict" {
		t.Fatalf("ReturnFields = %+v, want [verdict]", def.ReturnFields)
	}
}

func TestParseAgentMD_ReturnContract_Invalid(t *testing.T) {
	cases := map[string]string{
		"missing name": "    - description: x",
		"bad name":     "    - name: has space",
		"duplicate":    "    - name: verdict\n    - name: verdict",
	}
	for label, fields := range cases {
		t.Run(label, func(t *testing.T) {
			md := "---\nname: a\ndescription: t\nabbyfile:\n  return_contract:\n    fields:\n" +
				strings.ReplaceAll(fields, "    -", "      -") + "\n---\n\nBody."
			if _, err := ParseAgentMD(writeTempAgent(t, md)); err == nil || !strings.Contains(err.Error(), "return_contract") {
				t.Fatalf("err = %v, want a return_contract error", err)
			}
		})
	}
}
