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

// Fix round 1: Preserve comments and blank lines between entries
func TestUpsertTOMLServer_PreservesCommentsBetweenEntries(t *testing.T) {
	input := `[mcp_servers.agent]
command = "/old"

# This server is disabled for now
# [mcp_servers.old]
# command = "/ancient"

# Profile for fast work
[profiles.fast]
model = "o4-mini"
`
	out, _, _, err := upsertTOMLServer([]byte(input), "agent", map[string]any{"command": "/new"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// All trailing comments must be preserved
	for _, want := range []string{
		"# This server is disabled for now",
		"# [mcp_servers.old]",
		"# command = \"/ancient\"",
		"# Profile for fast work",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("lost comment %q:\n%s", want, s)
		}
	}
}

// Fix round 1: Preserve comments and blank lines on remove
func TestRemoveTOMLServer_PreservesCommentsBetweenEntries(t *testing.T) {
	input := `[mcp_servers.agent]
command = "/old"

# This server is disabled for now
# [mcp_servers.old]
# command = "/ancient"

# Profile for fast work
[profiles.fast]
model = "o4-mini"
`
	out, _, found, err := removeTOMLServer([]byte(input), "agent")
	if err != nil || !found {
		t.Fatalf("err=%v found=%v", err, found)
	}
	s := string(out)
	if strings.Contains(s, "[mcp_servers.agent]") {
		t.Errorf("agent block not removed:\n%s", s)
	}
	// All trailing comments must be preserved
	for _, want := range []string{
		"# This server is disabled for now",
		"# [mcp_servers.old]",
		"# command = \"/ancient\"",
		"# Profile for fast work",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("lost comment %q:\n%s", want, s)
		}
	}
}

// Fix round 1: Array-of-tables ([[ ]]) not duplicated on multiple upserts
func TestUpsertTOMLServer_ArrayOfTablesNotDuplicated(t *testing.T) {
	input := `[mcp_servers.agent]
command = "/old"

[[mcp_servers.agent.transports]]
type = "stdio"

[[mcp_servers.agent.transports]]
type = "sse"
`
	set := map[string]any{"command": "/new"}

	// First upsert
	out1, _, _, err := upsertTOMLServer([]byte(input), "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	count1 := strings.Count(string(out1), "[[mcp_servers.agent.transports]]")
	if count1 != 2 {
		t.Errorf("after first upsert: expected 2 transports, got %d:\n%s", count1, out1)
	}

	// Second upsert (should be idempotent)
	out2, _, _, err := upsertTOMLServer(out1, "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	count2 := strings.Count(string(out2), "[[mcp_servers.agent.transports]]")
	if count2 != 2 {
		t.Errorf("after second upsert: expected 2 transports, got %d:\n%s", count2, out2)
	}

	// Verify both have same content
	if string(out1) != string(out2) {
		t.Errorf("upserts not idempotent:\nfirst:\n%s\nsecond:\n%s", out1, out2)
	}
}

// Fix round 1: Remove succeeds with array-of-tables
func TestRemoveTOMLServer_WithArrayOfTables(t *testing.T) {
	input := `[mcp_servers.agent]
command = "/old"

[[mcp_servers.agent.transports]]
type = "stdio"

[[mcp_servers.agent.transports]]
type = "sse"

[profiles.fast]
model = "o4-mini"
`
	out, _, found, err := removeTOMLServer([]byte(input), "agent")
	if err != nil || !found {
		t.Fatalf("err=%v found=%v", err, found)
	}
	s := string(out)
	if strings.Contains(s, "[mcp_servers.agent]") || strings.Contains(s, "[[mcp_servers.agent.transports]]") {
		t.Errorf("agent block or transports not fully removed:\n%s", s)
	}
	if !strings.Contains(s, "[profiles.fast]") {
		t.Errorf("profiles.fast lost:\n%s", s)
	}
}

// Fix round 1: CRLF line endings are preserved
func TestUpsertTOMLServer_PreservesCRLF(t *testing.T) {
	input := "[mcp_servers.agent]\r\ncommand = \"/old\"\r\n"
	out, _, _, err := upsertTOMLServer([]byte(input), "agent", map[string]any{"command": "/new"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "\r\n") {
		t.Errorf("CRLF lost, got LF only:\n%q", s)
	}
	if strings.Contains(s, "\n") && !strings.Contains(s, "\r\n") {
		t.Errorf("mixed line endings:\n%q", s)
	}
}

// Fix round 1: Idempotence - same entry upserted twice yields byte-identical output
func TestUpsertTOMLServer_Idempotence(t *testing.T) {
	set := map[string]any{"command": "/new/agent", "args": []string{"serve-mcp"}, "cwd": "/proj", "tool_timeout_sec": int64(130)}

	// First upsert
	out1, _, _, err := upsertTOMLServer([]byte(codexFixture), "agent", set)
	if err != nil {
		t.Fatal(err)
	}

	// Second upsert with same data
	out2, _, _, err := upsertTOMLServer(out1, "agent", set)
	if err != nil {
		t.Fatal(err)
	}

	// Must be byte-identical (idempotent)
	if string(out1) != string(out2) {
		t.Errorf("upserts not idempotent:\nfirst:\n%s\n\nsecond:\n%s", out1, out2)
	}
}

// Fix round 2: Non-contiguous layout — agent blocks separated by profiles table
func TestUpsertTOMLServer_NonContiguousLayout(t *testing.T) {
	input := `[mcp_servers.agent]
command = "/old"

# about profiles
[profiles.fast]
model = "o4-mini"

[mcp_servers.agent.env]
TOKEN = "t"
`
	out, _, _, err := upsertTOMLServer([]byte(input), "agent", map[string]any{"command": "/new"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// Verify profiles section is intact
	for _, want := range []string{"[profiles.fast]", "model = \"o4-mini\"", "# about profiles"} {
		if !strings.Contains(s, want) {
			t.Errorf("lost %q:\n%s", want, s)
		}
	}
	// Verify agent.env is still there
	if !strings.Contains(s, "[mcp_servers.agent.env]") || !strings.Contains(s, "TOKEN = \"t\"") {
		t.Errorf("env lost:\n%s", s)
	}
}

// Fix round 2: Non-contiguous layout — remove preserves profiles table
func TestRemoveTOMLServer_NonContiguousLayout(t *testing.T) {
	input := `[mcp_servers.agent]
command = "/old"

# about profiles
[profiles.fast]
model = "o4-mini"

[mcp_servers.agent.env]
TOKEN = "t"
`
	out, _, found, err := removeTOMLServer([]byte(input), "agent")
	if err != nil || !found {
		t.Fatalf("err=%v found=%v", err, found)
	}
	s := string(out)
	// Verify profiles section is intact
	for _, want := range []string{"[profiles.fast]", "model = \"o4-mini\"", "# about profiles"} {
		if !strings.Contains(s, want) {
			t.Errorf("lost %q:\n%s", want, s)
		}
	}
	// Verify agent is completely gone
	if strings.Contains(s, "[mcp_servers.agent]") || strings.Contains(s, "TOKEN = \"t\"") {
		t.Errorf("agent not fully removed:\n%s", s)
	}
}

// Fix round 2: CRLF is preserved through multiple upserts; no double-carriage-return
func TestUpsertTOMLServer_CRLFMultipleUpserts(t *testing.T) {
	// Create CRLF version of codexFixture
	crlfFixture := strings.ReplaceAll(codexFixture, "\n", "\r\n")
	set := map[string]any{"command": "/new/agent"}

	// First upsert
	out1, _, _, err := upsertTOMLServer([]byte(crlfFixture), "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	s1 := string(out1)

	// Verify no double carriage returns
	if strings.Contains(s1, "\r\r") {
		t.Errorf("double carriage return found:\n%q", s1)
	}

	// Verify all line endings are CRLF
	if !strings.Contains(s1, "\r\n") {
		t.Errorf("CRLF lost in first upsert")
	}

	// Verify raw toml.Decode works (no normalization needed)
	var doc map[string]any
	if _, err := toml.Decode(s1, &doc); err != nil {
		t.Fatalf("raw toml.Decode failed (CRLF not preserved correctly): %v\n%q", err, s1)
	}

	// Second upsert
	out2, _, _, err := upsertTOMLServer(out1, "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	s2 := string(out2)

	// Third upsert
	out3, _, _, err := upsertTOMLServer(out2, "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	s3 := string(out3)

	// Outputs 2 and 3 must be byte-identical (idempotent)
	if s2 != s3 {
		t.Errorf("CRLF upserts not idempotent after second upsert:\nout2:\n%q\nout3:\n%q", s2, s3)
	}

	// All must have CRLF, no double-CR
	for i, s := range []string{s1, s2, s3} {
		if strings.Contains(s, "\r\r") {
			t.Errorf("upsert %d: double carriage return found", i+1)
		}
		if !strings.Contains(s, "\r\n") {
			t.Errorf("upsert %d: CRLF lost", i+1)
		}
	}
}

// Fix round 3: Remove with gaps leaves exactly one blank line between neighbours (segment model)
func TestRemoveTOMLServer_GapSeam(t *testing.T) {
	input := `[mcp_servers.other]
command = "/bin/other"

[mcp_servers.agent]
command = "/old"

[mcp_servers.agent.env]
TOKEN = "t"

[profiles.fast]
model = "o4-mini"
`
	out, _, found, err := removeTOMLServer([]byte(input), "agent")
	if err != nil || !found {
		t.Fatalf("err=%v found=%v", err, found)
	}
	s := string(out)
	// Verify [profiles.fast] is present and other table too
	if !strings.Contains(s, "[profiles.fast]") {
		t.Errorf("[profiles.fast] lost:\n%s", s)
	}
	if !strings.Contains(s, "[mcp_servers.other]") {
		t.Errorf("[mcp_servers.other] lost:\n%s", s)
	}
	// Verify exactly one blank line between them
	if !strings.Contains(s, "[mcp_servers.other]\ncommand = \"/bin/other\"\n\n[profiles.fast]") {
		t.Errorf("not exactly one blank line between neighbours:\n%s", s)
	}
}

// Fix round 2: Remove of last block ends with exactly one newline
func TestRemoveTOMLServer_LastBlockEndsWithNewline(t *testing.T) {
	input := `[mcp_servers.agent]
command = "/old"

[mcp_servers.agent.env]
TOKEN = "t"
`
	out, _, found, err := removeTOMLServer([]byte(input), "agent")
	if err != nil || !found {
		t.Fatalf("err=%v found=%v", err, found)
	}
	s := string(out)
	// Output should be empty or just whitespace, ending with exactly one newline if non-empty
	if len(s) > 0 && s != "\n" {
		t.Errorf("non-empty file after removing only entry should be just newline, got: %q", s)
	}
	if len(s) > 0 && !strings.HasSuffix(s, "\n") {
		t.Errorf("output does not end with newline: %q", s)
	}
	// Count trailing newlines
	if len(s) > 0 {
		trailingNewlines := len(s) - len(strings.TrimRight(s, "\n"))
		if trailingNewlines != 1 {
			t.Errorf("expected exactly 1 trailing newline, got %d: %q", trailingNewlines, s)
		}
	}
}

// Fix round 2: Array-of-tables idempotence with 3 upserts
func TestUpsertTOMLServer_ArrayOfTablesIdempotence3Upserts(t *testing.T) {
	input := `[mcp_servers.agent]
command = "/old"

[[mcp_servers.agent.transports]]
type = "stdio"

[[mcp_servers.agent.transports]]
type = "sse"
`
	set := map[string]any{"command": "/new"}

	// First upsert
	out1, _, _, err := upsertTOMLServer([]byte(input), "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	count1 := strings.Count(string(out1), "[[mcp_servers.agent.transports]]")
	if count1 != 2 {
		t.Errorf("after first upsert: expected 2 transports, got %d", count1)
	}

	// Second upsert
	out2, _, _, err := upsertTOMLServer(out1, "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	count2 := strings.Count(string(out2), "[[mcp_servers.agent.transports]]")
	if count2 != 2 {
		t.Errorf("after second upsert: expected 2 transports, got %d", count2)
	}

	// Third upsert
	out3, _, _, err := upsertTOMLServer(out2, "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	count3 := strings.Count(string(out3), "[[mcp_servers.agent.transports]]")
	if count3 != 2 {
		t.Errorf("after third upsert: expected 2 transports, got %d", count3)
	}

	// All must be identical (idempotent from first upsert onward)
	if string(out1) != string(out2) {
		t.Errorf("out1 != out2 (not idempotent)")
	}
	if string(out2) != string(out3) {
		t.Errorf("out2 != out3 (not idempotent)")
	}
}

// Fix round 2: CRLF remove preserves all-CRLF
func TestRemoveTOMLServer_PreservesCRLF(t *testing.T) {
	input := "[mcp_servers.agent]\r\ncommand = \"/old\"\r\n\r\n[profiles.fast]\r\nmodel = \"o4-mini\"\r\n"
	out, _, found, err := removeTOMLServer([]byte(input), "agent")
	if err != nil || !found {
		t.Fatalf("err=%v found=%v", err, found)
	}
	s := string(out)
	if !strings.Contains(s, "\r\n") {
		t.Errorf("CRLF lost in remove")
	}
	if strings.Contains(s, "\r\r") {
		t.Errorf("double carriage return: %q", s)
	}
	// Raw toml.Decode should work on the output
	var doc map[string]any
	if _, err := toml.Decode(s, &doc); err != nil {
		t.Fatalf("raw toml.Decode failed: %v\n%q", err, s)
	}
}

// Fix round 3: Loss case — comment and next header between subtables
func TestUpsertTOMLServer_LooseLossCase(t *testing.T) {
	input := "[mcp_servers.agent]\ncommand=\"old\"\n# note\n[mcp_servers.agent.env]\nA=\"1\"\n[tail]\n[next]\nq = 1\n"
	out, _, _, err := upsertTOMLServer([]byte(input), "agent", map[string]any{"command": "/new"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// Verify # note is preserved
	if !strings.Contains(s, "# note") {
		t.Errorf("lost comment: %s", s)
	}
	// Verify [tail] is preserved
	if !strings.Contains(s, "[tail]") {
		t.Errorf("lost [tail]: %s", s)
	}
	// Verify [next] is preserved
	if !strings.Contains(s, "[next]") {
		t.Errorf("lost [next]: %s", s)
	}
	// Verify decoding works and has tail, next
	var doc map[string]any
	if _, err := toml.Decode(s, &doc); err != nil {
		t.Fatalf("toml.Decode failed: %v\n%s", err, s)
	}
	if _, ok := doc["tail"]; !ok {
		t.Errorf("[tail] not in decoded output: %v", doc)
	}
	if _, ok := doc["next"]; !ok {
		t.Errorf("[next] not in decoded output: %v", doc)
	}
}

// Fix round 3: No-blank-line non-contiguous layout
func TestUpsertTOMLServer_NoBlankLineNonContiguous(t *testing.T) {
	input := "[mcp_servers.agent]\ncommand=\"o\"\n[profiles.fast]\nmodel = \"x\"\n[mcp_servers.agent.env]\nA = \"1\"\n[tail]\nk = 1\n"
	out, _, _, err := upsertTOMLServer([]byte(input), "agent", map[string]any{"command": "/new"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	var doc map[string]any
	if _, err := toml.Decode(s, &doc); err != nil {
		t.Fatalf("toml.Decode failed: %v\n%s", err, s)
	}
	// Check all tables are present
	if profiles, ok := doc["profiles"].(map[string]any); !ok || profiles["fast"] == nil {
		t.Errorf("profiles.fast missing: %v", doc)
	}
	if tail, ok := doc["tail"].(map[string]any); !ok || tail["k"] == nil {
		t.Errorf("tail.k missing: %v", doc)
	}
}

// Fix round 3: No-blank-line non-contiguous layout remove
func TestRemoveTOMLServer_NoBlankLineNonContiguous(t *testing.T) {
	input := "[mcp_servers.agent]\ncommand=\"o\"\n[profiles.fast]\nmodel = \"x\"\n[mcp_servers.agent.env]\nA = \"1\"\n[tail]\nk = 1\n"
	out, _, found, err := removeTOMLServer([]byte(input), "agent")
	if err != nil || !found {
		t.Fatalf("err=%v found=%v", err, found)
	}
	s := string(out)
	var doc map[string]any
	if _, err := toml.Decode(s, &doc); err != nil {
		t.Fatalf("toml.Decode failed: %v\n%s", err, s)
	}
	// Check all tables are still present
	if profiles, ok := doc["profiles"].(map[string]any); !ok || profiles["fast"] == nil {
		t.Errorf("profiles.fast missing after remove: %v", doc)
	}
	if tail, ok := doc["tail"].(map[string]any); !ok || tail["k"] == nil {
		t.Errorf("tail.k missing after remove: %v", doc)
	}
}

// Fix round 3: Idempotence on codexFixture
func TestUpsertTOMLServer_IdempotenceCodexFixture(t *testing.T) {
	set := map[string]any{"command": "/new/agent"}
	out1, _, _, err := upsertTOMLServer([]byte(codexFixture), "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	out2, _, _, err := upsertTOMLServer(out1, "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	if string(out1) != string(out2) {
		t.Errorf("idempotence failed on codexFixture")
	}
}

// Fix round 3: Idempotence on empty file
func TestUpsertTOMLServer_IdempotenceEmpty(t *testing.T) {
	set := map[string]any{"command": "/x"}
	out1, _, _, err := upsertTOMLServer([]byte(""), "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	out2, _, _, err := upsertTOMLServer(out1, "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	if string(out1) != string(out2) {
		t.Errorf("idempotence failed on empty file")
	}
}

// Fix round 3: Idempotence on single-line file
func TestUpsertTOMLServer_IdempotenceSingleLine(t *testing.T) {
	input := "model = \"o3\"\n"
	set := map[string]any{"command": "/x"}
	out1, _, _, err := upsertTOMLServer([]byte(input), "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	out2, _, _, err := upsertTOMLServer(out1, "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	if string(out1) != string(out2) {
		t.Errorf("idempotence failed on single-line file")
	}
}

// Fix round 3: Idempotence on loss-case input
func TestUpsertTOMLServer_IdempotenceLossCase(t *testing.T) {
	input := "[mcp_servers.agent]\ncommand=\"old\"\n# note\n[mcp_servers.agent.env]\nA=\"1\"\n[tail]\n[next]\nq = 1\n"
	set := map[string]any{"command": "/new"}
	out1, _, _, err := upsertTOMLServer([]byte(input), "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	out2, _, _, err := upsertTOMLServer(out1, "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	if string(out1) != string(out2) {
		t.Errorf("idempotence failed on loss-case input")
	}
}

// Fix round 3: Remove with env+tools leaves one blank line
func TestRemoveTOMLServer_GapBlankLine(t *testing.T) {
	input := "[mcp_servers.other]\ncommand = \"/bin/other\"\n\n[mcp_servers.agent]\ncommand = \"/old\"\n\n[mcp_servers.agent.env]\nTOKEN = \"t\"\n\n[mcp_servers.agent.tools.search]\napproval_mode = \"prompt\"\n\n[profiles.fast]\nmodel = \"o4-mini\"\n"
	out, _, found, err := removeTOMLServer([]byte(input), "agent")
	if err != nil || !found {
		t.Fatalf("err=%v found=%v", err, found)
	}
	s := string(out)
	// Verify exactly one blank line between other and profiles
	if !strings.Contains(s, "[mcp_servers.other]\ncommand = \"/bin/other\"\n\n[profiles.fast]") {
		t.Errorf("not exactly one blank line between neighbours: %s", s)
	}
}

// Fix round 3: Remove of leading entry leaves no leading blank
func TestRemoveTOMLServer_NoLeadingBlank(t *testing.T) {
	input := "[mcp_servers.agent]\ncommand = \"/old\"\n\n[profiles.fast]\nmodel = \"o4-mini\"\n"
	out, _, found, err := removeTOMLServer([]byte(input), "agent")
	if err != nil || !found {
		t.Fatalf("err=%v found=%v", err, found)
	}
	s := string(out)
	if strings.HasPrefix(s, "\n") {
		t.Errorf("output starts with blank line: %q", s)
	}
	if !strings.HasPrefix(s, "[profiles.fast]") {
		t.Errorf("expected [profiles.fast] at start: %s", s)
	}
}

// Fix round 3: Remove of only entry returns empty string
func TestRemoveTOMLServer_OnlyEntryEmpty(t *testing.T) {
	input := "[mcp_servers.agent]\ncommand = \"/old\"\n\n[mcp_servers.agent.env]\nA = \"1\"\n"
	out, _, found, err := removeTOMLServer([]byte(input), "agent")
	if err != nil || !found {
		t.Fatalf("err=%v found=%v", err, found)
	}
	if string(out) != "" {
		t.Errorf("expected empty string, got: %q", string(out))
	}
}

// Fix round 3: Comment before header with blank line preserved
func TestUpsertTOMLServer_CommentBeforeHeader(t *testing.T) {
	input := "# about agent\n\n[mcp_servers.agent]\ncommand = \"x\"\n"
	out, _, _, err := upsertTOMLServer([]byte(input), "agent", map[string]any{"command": "/new"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// Verify comment and exactly one blank line before header
	if !strings.Contains(s, "# about agent\n\n[mcp_servers.agent]") {
		t.Errorf("comment or blank line lost: %s", s)
	}
}

// Fix round 3: CRLF no double-CR and raw decode succeeds
func TestUpsertTOMLServer_CRLFNoDoubleCarriage(t *testing.T) {
	crlfFixture := strings.ReplaceAll(codexFixture, "\n", "\r\n")
	set := map[string]any{"command": "/new/agent"}

	// Multiple upserts
	var out []byte = []byte(crlfFixture)
	for i := 0; i < 3; i++ {
		var err error
		out, _, _, err = upsertTOMLServer(out, "agent", set)
		if err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
		s := string(out)
		if strings.Contains(s, "\r\r") {
			t.Errorf("upsert %d: double carriage return found", i)
		}
		// Raw decode must work
		var doc map[string]any
		if _, err := toml.Decode(s, &doc); err != nil {
			t.Fatalf("upsert %d: raw toml.Decode failed: %v", i, err)
		}
	}

	// Outputs should be identical after first upsert (idempotent)
	out1, _, _, _ := upsertTOMLServer([]byte(crlfFixture), "agent", set)
	out2, _, _, _ := upsertTOMLServer(out1, "agent", set)
	if string(out1) != string(out2) {
		t.Errorf("CRLF idempotence failed")
	}
}

// Fix round 3: CRLF loss case
func TestUpsertTOMLServer_CRLFLossCase(t *testing.T) {
	input := "[mcp_servers.agent]\r\ncommand=\"old\"\r\n# note\r\n[mcp_servers.agent.env]\r\nA=\"1\"\r\n[tail]\r\n[next]\r\nq = 1\r\n"
	out, _, _, err := upsertTOMLServer([]byte(input), "agent", map[string]any{"command": "/new"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "\r\r") {
		t.Errorf("double carriage return found: %q", s)
	}
	if !strings.Contains(s, "\r\n") {
		t.Errorf("CRLF lost: %q", s)
	}
	// Raw decode must work
	var doc map[string]any
	if _, err := toml.Decode(s, &doc); err != nil {
		t.Fatalf("raw toml.Decode failed: %v", err)
	}
	// Idempotence
	out2, _, _, err := upsertTOMLServer(out, "agent", map[string]any{"command": "/new"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(out2) {
		t.Errorf("CRLF idempotence failed")
	}
}
