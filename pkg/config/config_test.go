package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	cfg, err := LoadFrom(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.IsZero() {
		t.Errorf("expected zero config for missing file, got %+v", cfg)
	}
}

func TestLoadFull(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `model: gpt-5
tool_timeout: 120s
memory_limits:
  max_keys: 500
  max_value_bytes: 1048576
  max_total_bytes: 10485760
  ttl: 72h
command_policy:
  allowed_prefixes:
    - "go "
    - "make "
  denied_substrings:
    - "rm -rf"
  max_output_bytes: 5242880
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Model == nil || *cfg.Model != "gpt-5" {
		t.Errorf("model: got %v, want gpt-5", cfg.Model)
	}
	if cfg.ToolTimeout == nil || *cfg.ToolTimeout != "120s" {
		t.Errorf("tool_timeout: got %v, want 120s", cfg.ToolTimeout)
	}

	if cfg.MemoryLimits == nil {
		t.Fatal("memory_limits is nil")
	}
	if cfg.MemoryLimits.MaxKeys == nil || *cfg.MemoryLimits.MaxKeys != 500 {
		t.Errorf("max_keys: got %v, want 500", cfg.MemoryLimits.MaxKeys)
	}
	if cfg.MemoryLimits.MaxValueBytes == nil || *cfg.MemoryLimits.MaxValueBytes != 1048576 {
		t.Errorf("max_value_bytes: got %v, want 1048576", cfg.MemoryLimits.MaxValueBytes)
	}
	if cfg.MemoryLimits.MaxTotalBytes == nil || *cfg.MemoryLimits.MaxTotalBytes != 10485760 {
		t.Errorf("max_total_bytes: got %v, want 10485760", cfg.MemoryLimits.MaxTotalBytes)
	}
	if cfg.MemoryLimits.TTL == nil || *cfg.MemoryLimits.TTL != "72h" {
		t.Errorf("ttl: got %v, want 72h", cfg.MemoryLimits.TTL)
	}

	if cfg.CommandPolicy == nil {
		t.Fatal("command_policy is nil")
	}
	if cfg.CommandPolicy.AllowedPrefixes == nil || len(*cfg.CommandPolicy.AllowedPrefixes) != 2 {
		t.Errorf("allowed_prefixes: got %v, want 2 entries", cfg.CommandPolicy.AllowedPrefixes)
	}
	if cfg.CommandPolicy.DeniedSubstrings == nil || len(*cfg.CommandPolicy.DeniedSubstrings) != 1 {
		t.Errorf("denied_substrings: got %v, want 1 entry", cfg.CommandPolicy.DeniedSubstrings)
	}
	if cfg.CommandPolicy.MaxOutputBytes == nil || *cfg.CommandPolicy.MaxOutputBytes != 5242880 {
		t.Errorf("max_output_bytes: got %v, want 5242880", cfg.CommandPolicy.MaxOutputBytes)
	}
}

func TestLoadPartial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("model: claude-sonnet-4-6\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Model == nil || *cfg.Model != "claude-sonnet-4-6" {
		t.Errorf("model: got %v, want claude-sonnet-4-6", cfg.Model)
	}
	if cfg.ToolTimeout != nil {
		t.Errorf("tool_timeout should be nil, got %v", *cfg.ToolTimeout)
	}
	if cfg.MemoryLimits != nil {
		t.Errorf("memory_limits should be nil, got %+v", cfg.MemoryLimits)
	}
	if cfg.CommandPolicy != nil {
		t.Errorf("command_policy should be nil, got %+v", cfg.CommandPolicy)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(":\ninvalid: [yaml: {broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFrom(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestWriteAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent", "config.yaml")

	model := "o3"
	timeout := "60s"
	maxKeys := 100
	cfg := &Config{
		Model:       &model,
		ToolTimeout: &timeout,
		MemoryLimits: &MemoryLimitsOverride{
			MaxKeys: &maxKeys,
		},
	}

	if err := WriteTo(path, cfg); err != nil {
		t.Fatalf("write error: %v", err)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if got.Model == nil || *got.Model != "o3" {
		t.Errorf("model round-trip: got %v, want o3", got.Model)
	}
	if got.ToolTimeout == nil || *got.ToolTimeout != "60s" {
		t.Errorf("tool_timeout round-trip: got %v, want 60s", got.ToolTimeout)
	}
	if got.MemoryLimits == nil || got.MemoryLimits.MaxKeys == nil || *got.MemoryLimits.MaxKeys != 100 {
		t.Errorf("max_keys round-trip: got %v, want 100", got.MemoryLimits)
	}
	// Unset fields should remain nil.
	if got.CommandPolicy != nil {
		t.Errorf("command_policy should be nil after round-trip")
	}
}

func TestWriteField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Write model field to new file.
	if err := WriteFieldTo(path, "model", "gpt-5"); err != nil {
		t.Fatalf("write field error: %v", err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Model == nil || *cfg.Model != "gpt-5" {
		t.Errorf("model: got %v, want gpt-5", cfg.Model)
	}

	// Write another field — model should be preserved.
	if err := WriteFieldTo(path, "tool_timeout", "90s"); err != nil {
		t.Fatalf("write field error: %v", err)
	}

	cfg, err = LoadFrom(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Model == nil || *cfg.Model != "gpt-5" {
		t.Errorf("model should be preserved: got %v", cfg.Model)
	}
	if cfg.ToolTimeout == nil || *cfg.ToolTimeout != "90s" {
		t.Errorf("tool_timeout: got %v, want 90s", cfg.ToolTimeout)
	}
}

func TestWriteFieldUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	err := WriteFieldTo(path, "name", "bad")
	if err == nil {
		t.Fatal("expected error for unsupported field")
	}
}

func TestLoadViaAgentName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Create config at expected path.
	agentDir := filepath.Join(dir, ".abbyfile", "test-agent")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "config.yaml"), []byte("model: haiku\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load("test-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Model == nil || *cfg.Model != "haiku" {
		t.Errorf("model: got %v, want haiku", cfg.Model)
	}
}

func TestResetField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Write config with two fields.
	model := "gpt-5"
	timeout := "90s"
	cfg := &Config{Model: &model, ToolTimeout: &timeout}
	if err := WriteTo(path, cfg); err != nil {
		t.Fatalf("write error: %v", err)
	}

	// Reset model — tool_timeout should remain.
	if err := ResetFieldTo(path, "model"); err != nil {
		t.Fatalf("reset error: %v", err)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if got.Model != nil {
		t.Errorf("model should be nil after reset, got %v", *got.Model)
	}
	if got.ToolTimeout == nil || *got.ToolTimeout != "90s" {
		t.Errorf("tool_timeout should be preserved, got %v", got.ToolTimeout)
	}
}

func TestResetFieldDeletesFileWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Write config with only model.
	model := "gpt-5"
	cfg := &Config{Model: &model}
	if err := WriteTo(path, cfg); err != nil {
		t.Fatalf("write error: %v", err)
	}

	// Reset model — file should be deleted.
	if err := ResetFieldTo(path, "model"); err != nil {
		t.Fatalf("reset error: %v", err)
	}

	if _, err := os.Stat(path); err == nil {
		t.Error("config file should be deleted when all fields are nil")
	}
}

func TestResetFieldUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("model: test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ResetFieldTo(path, "name")
	if err == nil {
		t.Fatal("expected error for unsupported field")
	}
}

func TestResetFieldMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.yaml")

	// Resetting a field on a missing file should be a no-op (file already absent).
	err := ResetFieldTo(path, "model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsZero(t *testing.T) {
	if !(&Config{}).IsZero() {
		t.Error("empty config should be zero")
	}
	model := "test"
	if (&Config{Model: &model}).IsZero() {
		t.Error("config with model should not be zero")
	}
}

func TestWriteResetField_ContextBudget(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"

	if err := WriteFieldTo(path, "context_budget.max_output_lines", "500"); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ContextBudget == nil || cfg.ContextBudget.MaxOutputLines == nil || *cfg.ContextBudget.MaxOutputLines != 500 {
		t.Fatalf("max_output_lines not persisted: %+v", cfg.ContextBudget)
	}

	if err := WriteFieldTo(path, "context_budget.on_overflow", "spill"); err != nil {
		t.Fatalf("write on_overflow: %v", err)
	}
	cfg, _ = LoadFrom(path)
	if cfg.ContextBudget.OnOverflow == nil || *cfg.ContextBudget.OnOverflow != "spill" {
		t.Fatalf("on_overflow not persisted: %+v", cfg.ContextBudget)
	}
	// max_output_lines must survive the second write (merge, not clobber).
	if cfg.ContextBudget.MaxOutputLines == nil || *cfg.ContextBudget.MaxOutputLines != 500 {
		t.Fatalf("second write clobbered max_output_lines")
	}

	if err := ResetFieldTo(path, "context_budget"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	cfg, _ = LoadFrom(path)
	if cfg.ContextBudget != nil {
		t.Fatalf("context_budget not reset: %+v", cfg.ContextBudget)
	}
}

func TestWriteField_ContextBudget_InvalidStrategy(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := WriteFieldTo(path, "context_budget.on_overflow", "bogus"); err == nil {
		t.Fatal("expected error for invalid on_overflow value")
	}
}

func TestWriteFieldSandbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	steps := []struct{ field, value string }{
		{"sandbox.allowed_dirs", ".,/tmp/work"},
		{"sandbox.bash", "unrestricted"},
		{"sandbox.allow_commands", `["go test *","echo a,b"]`},
		{"sandbox.max_command_timeout", "45s"},
	}
	for _, s := range steps {
		if err := WriteFieldTo(path, s.field, s.value); err != nil {
			t.Fatalf("%s: %v", s.field, err)
		}
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	sb := cfg.Sandbox
	if sb == nil || !reflect.DeepEqual(*sb.AllowedDirs, []string{".", "/tmp/work"}) || *sb.Bash != "unrestricted" ||
		!reflect.DeepEqual(*sb.AllowCommands, []string{"go test *", "echo a,b"}) || *sb.MaxCommandTimeout != "45s" {
		t.Fatalf("Sandbox = %+v", sb)
	}
	if cfg.IsZero() {
		t.Fatal("IsZero must consider Sandbox")
	}
}

func TestWriteFieldSandbox_EmptyAllowCommandsRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := WriteFieldTo(path, "sandbox.allow_commands", ""); err != nil {
		t.Fatal(err)
	}
	cfg, _ := LoadFrom(path)
	if cfg.Sandbox == nil || cfg.Sandbox.AllowCommands == nil || len(*cfg.Sandbox.AllowCommands) != 0 {
		t.Fatalf("empty allow_commands must round-trip as an explicit empty list, got %+v", cfg.Sandbox)
	}
}

func TestWriteFieldSandbox_Invalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	for field, value := range map[string]string{
		"sandbox.allowed_dirs":        "",
		"sandbox.bash":                "yolo",
		"sandbox.allow_commands":      "go test | tee",
		"sandbox.max_command_timeout": "0s",
	} {
		if err := WriteFieldTo(path, field, value); err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("%s=%q: err = %v, want error naming the field", field, value, err)
		}
	}
}

func TestResetSandbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := WriteFieldTo(path, "sandbox.bash", "unrestricted"); err != nil {
		t.Fatal(err)
	}
	if err := ResetFieldTo(path, "sandbox"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("resetting the only field must delete the file")
	}
}

func TestParseList(t *testing.T) {
	for in, want := range map[string][]string{
		"":               {},
		"a, b ,,c":       {"a", "b", "c"},
		`["x, y","z"]`:   {"x, y", "z"},
		`[]`:             {},
		`[""]`:           {},
		`["", " ", "a"]`: {"a"},
	} {
		got, err := ParseList(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("ParseList(%q) = %#v, %v; want %#v", in, got, err, want)
		}
	}
	if _, err := ParseList(`[broken`); err == nil {
		t.Error("broken JSON must fail")
	}
}

// Fix round 1, issue 1a: the JSON branch of ParseList must drop blank
// entries too, matching its doc comment — otherwise
// `config set sandbox.allowed_dirs '[""]'` used to slip a single blank
// entry past the len(dirs)==0 guard in WriteFieldTo.
func TestParseList_JSONBranchDropsBlankEntries(t *testing.T) {
	got, err := ParseList(`["", "  ", "a", "b"]`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("ParseList = %#v, want [a b]", got)
	}
}

// Fix round 1, issue 1: config set sandbox.allowed_dirs '[""]' (or
// '["", " "]') must be rejected, not silently accepted as a lockout.
func TestWriteFieldSandbox_AllowedDirsAllBlankRejected(t *testing.T) {
	for _, value := range []string{`[""]`, `["", " "]`} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		err := WriteFieldTo(path, "sandbox.allowed_dirs", value)
		if err == nil || !strings.Contains(err.Error(), "sandbox.allowed_dirs") {
			t.Errorf("value=%q: err = %v, want error naming sandbox.allowed_dirs", value, err)
		}
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("value=%q: config file must not be written on a rejected value", value)
		}
	}
}

func TestWriteFieldSandbox_AllowCommandsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	err := WriteFieldTo(path, "sandbox.allow_commands", "[broken")
	if err == nil || !strings.Contains(err.Error(), "sandbox.allow_commands") {
		t.Fatalf("err = %v, want error naming sandbox.allow_commands", err)
	}
}

// Fix round 1, issue 1b: WriteFieldTo must validate the *merged* sandbox
// override, not just the field being set, so a sibling field left invalid
// by a hand-edited config.yaml (from before this validation existed) is
// caught rather than persisted forward untouched.
func TestWriteFieldTo_MergedValidationCatchesHandEditedSibling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := "sandbox:\n  allowed_dirs: [\"\"]\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	err := WriteFieldTo(path, "sandbox.bash", "restricted")
	if err == nil || !strings.Contains(err.Error(), "sandbox.bash") {
		t.Fatalf("err = %v, want error naming sandbox.bash", err)
	}

	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(raw) != original {
		t.Errorf("config file must not be written when merged validation fails, got %q", raw)
	}
}

// --- Coverage: Write/WriteField/ResetField (agent-name based wrappers) and
// remaining WriteFieldTo/ResetFieldTo branches (fix round 1, issue 2). ---

func TestPath_HomeUnresolvable(t *testing.T) {
	t.Setenv("HOME", "")
	if p := Path("test-agent"); p != "" {
		t.Errorf("Path() = %q, want empty when HOME cannot be resolved", p)
	}
}

func TestLoad_HomeUnresolvable(t *testing.T) {
	t.Setenv("HOME", "")
	cfg, err := Load("test-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.IsZero() {
		t.Errorf("expected zero config when HOME cannot be resolved, got %+v", cfg)
	}
}

func TestLoadFrom_ReadError(t *testing.T) {
	dir := t.TempDir()
	// A directory can't be read as a file: os.ReadFile fails with something
	// other than os.ErrNotExist.
	if _, err := LoadFrom(dir); err == nil {
		t.Fatal("expected error reading a directory as a config file")
	}
}

func TestWrite_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	model := "gpt-5"
	if err := Write("test-agent", &Config{Model: &model}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	cfg, err := Load("test-agent")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model == nil || *cfg.Model != "gpt-5" {
		t.Errorf("model = %v, want gpt-5", cfg.Model)
	}
}

func TestWrite_HomeUnresolvable(t *testing.T) {
	t.Setenv("HOME", "")
	if err := Write("test-agent", &Config{}); err == nil {
		t.Fatal("expected error when HOME cannot be resolved")
	}
}

func TestWriteTo_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// blocker is a file, not a directory, so MkdirAll for a path under it must fail.
	path := filepath.Join(blocker, "sub", "config.yaml")
	if err := WriteTo(path, &Config{}); err == nil {
		t.Fatal("expected error creating config directory under a file")
	}
}

func TestWriteField_ViaAgentName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := WriteField("test-agent", "model", "opus"); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	cfg, err := Load("test-agent")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model == nil || *cfg.Model != "opus" {
		t.Errorf("model = %v, want opus", cfg.Model)
	}
}

func TestWriteField_HomeUnresolvable(t *testing.T) {
	t.Setenv("HOME", "")
	if err := WriteField("test-agent", "model", "opus"); err == nil {
		t.Fatal("expected error when HOME cannot be resolved")
	}
}

func TestResetField_ViaAgentName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := WriteField("test-agent", "model", "opus"); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	if err := ResetField("test-agent", "model"); err != nil {
		t.Fatalf("ResetField: %v", err)
	}
	cfg, err := Load("test-agent")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != nil {
		t.Errorf("model should be nil after reset, got %v", *cfg.Model)
	}
}

func TestResetField_HomeUnresolvable(t *testing.T) {
	t.Setenv("HOME", "")
	if err := ResetField("test-agent", "model"); err == nil {
		t.Fatal("expected error when HOME cannot be resolved")
	}
}

func TestResetFieldTo_LoadError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(":\ninvalid: [yaml: {broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ResetFieldTo(path, "model"); err == nil {
		t.Fatal("expected error resetting a field in an unparsable config file")
	}
}

func TestWriteField_ContextBudget_AllFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	valid := map[string]string{
		"context_budget.max_output_lines":   "10",
		"context_budget.max_output_bytes":   "2048",
		"context_budget.head_lines":         "5",
		"context_budget.tail_lines":         "5",
		"context_budget.summary_lines":      "3",
		"context_budget.eager_instructions": "true",
	}
	for field, value := range valid {
		if err := WriteFieldTo(path, field, value); err != nil {
			t.Errorf("%s=%q: unexpected error: %v", field, value, err)
		}
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ContextBudget == nil {
		t.Fatal("context_budget is nil after writing all sub-fields")
	}
	if cfg.ContextBudget.MaxOutputLines == nil || *cfg.ContextBudget.MaxOutputLines != 10 {
		t.Errorf("max_output_lines = %v, want 10", cfg.ContextBudget.MaxOutputLines)
	}
	if cfg.ContextBudget.MaxOutputBytes == nil || *cfg.ContextBudget.MaxOutputBytes != 2048 {
		t.Errorf("max_output_bytes = %v, want 2048", cfg.ContextBudget.MaxOutputBytes)
	}
	if cfg.ContextBudget.HeadLines == nil || *cfg.ContextBudget.HeadLines != 5 {
		t.Errorf("head_lines = %v, want 5", cfg.ContextBudget.HeadLines)
	}
	if cfg.ContextBudget.TailLines == nil || *cfg.ContextBudget.TailLines != 5 {
		t.Errorf("tail_lines = %v, want 5", cfg.ContextBudget.TailLines)
	}
	if cfg.ContextBudget.SummaryLines == nil || *cfg.ContextBudget.SummaryLines != 3 {
		t.Errorf("summary_lines = %v, want 3", cfg.ContextBudget.SummaryLines)
	}
	if cfg.ContextBudget.EagerInstructions == nil || !*cfg.ContextBudget.EagerInstructions {
		t.Errorf("eager_instructions = %v, want true", cfg.ContextBudget.EagerInstructions)
	}
}

func TestWriteField_ContextBudget_InvalidValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	invalid := map[string]string{
		"context_budget.max_output_lines":   "not-a-number",
		"context_budget.max_output_bytes":   "not-a-number",
		"context_budget.head_lines":         "not-a-number",
		"context_budget.tail_lines":         "not-a-number",
		"context_budget.summary_lines":      "not-a-number",
		"context_budget.eager_instructions": "not-a-bool",
	}
	for field, value := range invalid {
		if err := WriteFieldTo(path, field, value); err == nil {
			t.Errorf("%s=%q: expected error", field, value)
		}
	}
}
