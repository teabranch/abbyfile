package runtimecfg

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

const codexFixture = `# user settings
model = "o3"  # keep me

[mcp_servers.other]
command = "/bin/other"

[mcp_servers.agent]
command = "/old/agent"
args = ["serve-mcp"]
enabled = false

[mcp_servers.agent.env]
TOKEN = "t"

[mcp_servers.agent.tools.search]
approval_mode = "prompt"

[profiles.fast]
model = "o4-mini"
`

// Review Focus #1.
func TestUpsertTOMLServer_PreservesUnownedKeysAndComments(t *testing.T) {
	set := map[string]any{"command": "/new/agent", "args": []string{"serve-mcp"}, "cwd": "/proj", "tool_timeout_sec": int64(130)}
	out, before, after, err := upsertTOMLServer([]byte(codexFixture), "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"# user settings", "# keep me", "[mcp_servers.other]", "[profiles.fast]", `model = "o4-mini"`} {
		if !strings.Contains(s, want) {
			t.Errorf("lost %q:\n%s", want, s)
		}
	}
	if before["command"] != "/old/agent" {
		t.Errorf("before = %v", before)
	}
	if after["enabled"] != false {
		t.Errorf("unowned key enabled lost: %v", after)
	}
	var doc map[string]any
	if _, err := toml.Decode(s, &doc); err != nil {
		t.Fatalf("output is not valid TOML: %v\n%s", err, s)
	}
	got, ok, _ := lookupTOMLServer(out, "agent")
	if !ok || got["command"] != "/new/agent" || got["cwd"] != "/proj" || got["enabled"] != false {
		t.Errorf("round trip = %v", got)
	}
	env, _ := got["env"].(map[string]any)
	tools, _ := got["tools"].(map[string]any)
	if env["TOKEN"] != "t" || tools["search"] == nil {
		t.Errorf("subtables lost: env=%v tools=%v", env, tools)
	}
	if strings.Count(s, "[mcp_servers.agent]") != 1 {
		t.Errorf("block duplicated:\n%s", s)
	}
}

func TestUpsertTOMLServer_AppendsWhenAbsentAndQuotesNames(t *testing.T) {
	out, before, _, err := upsertTOMLServer([]byte("model = \"o3\"\n"), "my.agent", map[string]any{"command": "/x"})
	if err != nil || before != nil {
		t.Fatalf("err=%v before=%v", err, before)
	}
	if !strings.Contains(string(out), `[mcp_servers."my.agent"]`) {
		t.Errorf("name not quoted:\n%s", out)
	}
	if got, ok, _ := lookupTOMLServer(out, "my.agent"); !ok || got["command"] != "/x" {
		t.Errorf("lookup = %v %v", got, ok)
	}
}

// Files written by abby <= v0.11 via the BurntSushi encoder (indented, one [mcp_servers] header).
func TestUpsertTOMLServer_LegacyEncoderOutput(t *testing.T) {
	legacy := "[mcp_servers]\n  [mcp_servers.agent]\n    command = \"/old\"\n    args = [\"serve-mcp\"]\n"
	out, _, _, err := upsertTOMLServer([]byte(legacy), "agent", map[string]any{"command": "/new"})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if _, err := toml.Decode(string(out), &doc); err != nil {
		t.Fatalf("invalid TOML: %v\n%s", err, out)
	}
	if got, _, _ := lookupTOMLServer(out, "agent"); got["command"] != "/new" {
		t.Errorf("got %v\n%s", got, out)
	}
}

func TestUpsertTOMLServer_RefusesInlineForm(t *testing.T) {
	in := "[mcp_servers]\nagent = { command = \"/x\" }\n"
	if _, _, _, err := upsertTOMLServer([]byte(in), "agent", map[string]any{"command": "/y"}); err == nil || !strings.Contains(err.Error(), "inline") {
		t.Fatalf("err = %v, want inline-form refusal", err)
	}
}

func TestUpsertTOMLServer_RejectsInvalidFile(t *testing.T) {
	if _, _, _, err := upsertTOMLServer([]byte("model = \n"), "a", map[string]any{"command": "/x"}); err == nil {
		t.Fatal("unparsable TOML must be refused")
	}
}

func TestRemoveTOMLServer(t *testing.T) {
	out, before, found, err := removeTOMLServer([]byte(codexFixture), "agent")
	if err != nil || !found || before["command"] != "/old/agent" {
		t.Fatalf("remove = %v %v %v", found, before, err)
	}
	s := string(out)
	if strings.Contains(s, "mcp_servers.agent") || !strings.Contains(s, "[mcp_servers.other]") || !strings.Contains(s, "# keep me") {
		t.Errorf("remove output:\n%s", s)
	}
	if _, _, found, _ := removeTOMLServer([]byte(codexFixture), "missing"); found {
		t.Error("missing must be found=false")
	}
}
