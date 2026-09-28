package runtimecfg

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// bracketKeyFixture has a Codex project-trust table whose quoted key contains
// brackets. It must not be mistaken for part of [mcp_servers.agent].
const bracketKeyFixture = "[mcp_servers.agent]\ncommand = \"/old\"\n\n[projects.\"/Users/me/a[1]\"]\ntrust_level = \"trusted\"\n\n[other]\nx = 1\n"

// fakeHeaderFixture has header-like lines inside multi-line strings. The
// segmenter treats "[mcp_servers.agent.env]" as a target header, so an edit
// would drop keep.b and the [z] table; the whole-document guard must refuse.
const fakeHeaderFixture = "[mcp_servers.agent]\ncommand = \"/old\"\n\n[keep]\na = \"\"\"\n[mcp_servers.agent.env]\n\"\"\"\nb = \"\"\"\n[z]\n\"\"\"\n"

func assertKeepsBracketKeyTables(t *testing.T, out []byte) {
	t.Helper()
	var doc map[string]any
	if _, err := toml.Decode(string(out), &doc); err != nil {
		t.Fatalf("output is not valid TOML: %v\n%s", err, out)
	}
	projects, _ := doc["projects"].(map[string]any)
	proj, _ := projects["/Users/me/a[1]"].(map[string]any)
	if proj["trust_level"] != "trusted" {
		t.Errorf("lost projects.\"/Users/me/a[1]\".trust_level:\n%s", out)
	}
	other, _ := doc["other"].(map[string]any)
	if other["x"] != int64(1) {
		t.Errorf("lost other.x:\n%s", out)
	}
}

func TestUpsertTOMLServer_KeepsTableWithBracketsInQuotedKey(t *testing.T) {
	out, _, _, err := upsertTOMLServer([]byte(bracketKeyFixture), "agent", map[string]any{"command": "/new"})
	if err != nil {
		t.Fatal(err)
	}
	assertKeepsBracketKeyTables(t, out)
	got, ok, _ := lookupTOMLServer(out, "agent")
	if !ok || got["command"] != "/new" {
		t.Errorf("entry = %v", got)
	}
}

func TestRemoveTOMLServer_KeepsTableWithBracketsInQuotedKey(t *testing.T) {
	out, _, found, err := removeTOMLServer([]byte(bracketKeyFixture), "agent")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("entry not found")
	}
	assertKeepsBracketKeyTables(t, out)
	if _, ok, _ := lookupTOMLServer(out, "agent"); ok {
		t.Errorf("entry still present:\n%s", out)
	}
}

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

func TestParseTOMLHeader(t *testing.T) {
	cases := []struct {
		line  string
		ok    bool
		array bool
		key   string
	}{
		{`[mcp_servers.agent]`, true, false, `mcp_servers.agent`},
		{`  [ mcp_servers.agent ]  # c`, true, false, `mcp_servers.agent`},
		{`[[mcp_servers.agent.x]]`, true, true, `mcp_servers.agent.x`},
		{`[projects."/Users/me/a[1]"]`, true, false, `projects."/Users/me/a[1]"`},
		{`[projects.'/x/]y[']`, true, false, `projects.'/x/]y['`},
		{`[a."q\"]"]`, true, false, `a."q\"]"`},
		{`[a]]`, false, false, ""},
		{`[[a]`, false, false, ""},
		{`[a] x`, false, false, ""},
		{`[a[1]]`, false, false, ""},
		{`[]`, false, false, ""},
		{`["unterminated]`, false, false, ""},
		{`x = [1]`, false, false, ""},
		{`[1, 2],`, false, false, ""},
	}
	for _, c := range cases {
		key, array, ok := parseTOMLHeader(c.line)
		if ok != c.ok || (ok && (array != c.array || key != c.key)) {
			t.Errorf("parseTOMLHeader(%q) = %q, %v, %v; want %q, %v, %v", c.line, key, array, ok, c.key, c.array, c.ok)
		}
	}
}
