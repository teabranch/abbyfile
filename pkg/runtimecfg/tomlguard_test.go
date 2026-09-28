package runtimecfg

import (
	"strings"
	"testing"
)

// fakeHeaderFixture has header-like lines inside multi-line strings. The
// segmenter treats "[mcp_servers.agent.env]" as a target header, so an edit
// would drop keep.b and the [z] table; the whole-document guard must refuse.
const fakeHeaderFixture = "[mcp_servers.agent]\ncommand = \"/old\"\n\n[keep]\na = \"\"\"\n[mcp_servers.agent.env]\n\"\"\"\nb = \"\"\"\n[z]\n\"\"\"\n"

func TestTOMLEdit_GuardRefusesWhenOtherPartsWouldChange(t *testing.T) {
	const want = "edit would change other parts of the file; abby did not modify it"
	out, _, _, err := upsertTOMLServer([]byte(fakeHeaderFixture), "agent", map[string]any{"command": "/new"})
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("upsert err = %v, out:\n%s", err, out)
	}
	out, _, _, err = removeTOMLServer([]byte(fakeHeaderFixture), "agent")
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("remove err = %v, out:\n%s", err, out)
	}
}

func TestGuardTOMLEdit(t *testing.T) {
	cases := []struct {
		name, original, edited string
		wantErr, wantEntry     bool
	}{
		{"entry removed and mcp_servers emptied", "[mcp_servers.agent]\ncommand = \"/a\"\n", "", false, false},
		{"empty mcp_servers table left behind", "[mcp_servers.agent]\ncommand = \"/a\"\n", "[mcp_servers]\n", false, false},
		{"entry changed only", "x = 1\n[mcp_servers.agent]\ncommand = \"/a\"\n", "x = 1\n[mcp_servers.agent]\ncommand = \"/b\"\n", false, true},
		{"sibling server lost", "[mcp_servers.a]\ncommand = \"/a\"\n[mcp_servers.agent]\ncommand = \"/b\"\n", "[mcp_servers.agent]\ncommand = \"/b\"\n", true, false},
		{"whitespace-only line inside a string changed", "[keep]\ns = \"\"\"\n  \n\"\"\"\n", "[keep]\ns = \"\"\"\n\n\"\"\"\n", true, false},
		{"edited text does not parse", "x = 1\n", "x = \n", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ok, err := guardTOMLEdit(c.original, c.edited, "agent")
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "abby did not modify it") {
				t.Errorf("err = %v", err)
			}
			if err == nil && ok != c.wantEntry {
				t.Errorf("entry present = %v, want %v", ok, c.wantEntry)
			}
		})
	}
}
