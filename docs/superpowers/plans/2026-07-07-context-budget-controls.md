# Context-Budget Controls Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give packaged Abbyfile agents controls that limit how much they inject into the LLM context window — mechanical tool-output shaping, optional isolation via an emitted sub-agent, and leaner instruction injection.

**Architecture:** Two composable layers over one config surface. Layer B (`pkg/tools/shaper.go`) caps tool output inside `Executor.Run` with head-tail / spill / passthrough strategies. Layer A (`pkg/subagent`) emits a `.claude/agents/<name>.md` sub-agent with a return protocol so the runtime can run the agent in its own context window and return a bounded summary. The instructions fix gates eager prompt injection in `pkg/mcp/bridge.go`. Limits are declared in agent frontmatter (`context_budget:`), overridable via `~/.abbyfile/<name>/config.yaml`, with safe defaults on.

**Tech Stack:** Go 1.24, Cobra, `gopkg.in/yaml.v3`, `github.com/modelcontextprotocol/go-sdk/mcp`, standard `go test` (table-driven, `-race`).

## Global Constraints

- Go 1.24.0 (`go.mod`); gofmt/goimports clean; `go test -race ./...` must pass.
- The binary MUST NOT call the Claude API. All binary-side shaping is mechanical/deterministic. LLM summarization happens only in the runtime-spawned sub-agent (Layer A).
- Backward compatible: sub-agent emission is opt-in via flag. Output shaping defaults on but is fully disableable (`on_overflow: passthrough` or a `0` limit).
- Follow existing patterns: functional options (`pkg/agent/options.go`), nil-able pointer overrides (`pkg/config`), table-driven tests, error wrapping with `%w`.
- Config precedence (highest first): per-tool override → `config.yaml` override → compiled frontmatter default → shipped default.
- Shipped defaults: `max_output_lines: 2000`, `max_output_bytes: 262144`, `on_overflow: head-tail`, `head_lines: 100`, `tail_lines: 40`, `summary_lines: 25`, `eager_instructions: false`.
- Commit after each task. Commit type prefixes: `feat`, `fix`, `test`, `docs`, `refactor`.

---

### Task 1: ContextBudget type + Shape() core in pkg/tools

**Files:**
- Create: `pkg/tools/shaper.go`
- Test: `pkg/tools/shaper_test.go`

**Interfaces:**
- Consumes: nothing (leaf task).
- Produces:
  - `type OverflowStrategy string` with consts `OverflowHeadTail = "head-tail"`, `OverflowSpill = "spill"`, `OverflowPassthrough = "passthrough"`.
  - `type ContextBudget struct { MaxOutputLines int; MaxOutputBytes int64; OnOverflow OverflowStrategy; HeadLines int; TailLines int; SummaryLines int; EagerInstructions bool; PerTool map[string]ContextBudget }`
  - `type SpillSink interface { Put(key, value string) (uri string, err error) }`
  - `type ShapeResult struct { Output string; Shaped bool; Strategy OverflowStrategy; OriginalLines int; OriginalBytes int64; SpillURI string }`
  - `func DefaultContextBudget() ContextBudget`
  - `func (b ContextBudget) effectiveFor(toolName string) ContextBudget`
  - `func (b ContextBudget) Shape(toolName, raw string, sink SpillSink) ShapeResult`

- [ ] **Step 1: Write the failing test**

```go
package tools

import "testing"

func TestShape_UnderCapPassthrough(t *testing.T) {
	b := ContextBudget{MaxOutputLines: 10, MaxOutputBytes: 1000, OnOverflow: OverflowHeadTail, HeadLines: 3, TailLines: 2}
	res := b.Shape("t", "a\nb\nc", nil)
	if res.Shaped {
		t.Fatalf("expected not shaped, got Shaped=true")
	}
	if res.Output != "a\nb\nc" {
		t.Fatalf("output mutated: %q", res.Output)
	}
}

func TestShape_HeadTailElidesMiddle(t *testing.T) {
	// 20 lines, cap at 5, keep 2 head + 2 tail.
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, string(rune('A'+i%26))+"-line")
	}
	raw := ""
	for i, l := range lines {
		if i > 0 {
			raw += "\n"
		}
		raw += l
	}
	b := ContextBudget{MaxOutputLines: 5, MaxOutputBytes: 100000, OnOverflow: OverflowHeadTail, HeadLines: 2, TailLines: 2}
	res := b.Shape("t", raw, nil)
	if !res.Shaped {
		t.Fatal("expected shaped")
	}
	if res.OriginalLines != 20 {
		t.Fatalf("OriginalLines = %d, want 20", res.OriginalLines)
	}
	if !contains(res.Output, "elided") {
		t.Fatalf("expected elision marker, got: %q", res.Output)
	}
	if !contains(res.Output, lines[0]) || !contains(res.Output, lines[19]) {
		t.Fatalf("head/tail lines missing from output: %q", res.Output)
	}
}

func TestShape_ByteBackstopDefeatsHugeLines(t *testing.T) {
	// One giant line: under line cap but over byte cap.
	huge := ""
	for i := 0; i < 5000; i++ {
		huge += "x"
	}
	b := ContextBudget{MaxOutputLines: 100, MaxOutputBytes: 1000, OnOverflow: OverflowHeadTail, HeadLines: 1, TailLines: 1}
	res := b.Shape("t", huge, nil)
	if !res.Shaped {
		t.Fatal("expected shaped by byte backstop")
	}
	if int64(len(res.Output)) > 1000 {
		t.Fatalf("output %d bytes exceeds byte cap 1000", len(res.Output))
	}
}

func TestShape_Passthrough_ExplicitStrategy(t *testing.T) {
	b := ContextBudget{MaxOutputLines: 1, MaxOutputBytes: 1, OnOverflow: OverflowPassthrough}
	res := b.Shape("t", "a\nb\nc\nd", nil)
	if res.Shaped {
		t.Fatal("passthrough must never shape")
	}
	if res.Output != "a\nb\nc\nd" {
		t.Fatalf("passthrough mutated output: %q", res.Output)
	}
}

func TestEffectiveFor_PerToolMerge(t *testing.T) {
	b := ContextBudget{MaxOutputLines: 100, OnOverflow: OverflowHeadTail, HeadLines: 5, TailLines: 5,
		PerTool: map[string]ContextBudget{"run_command": {OnOverflow: OverflowSpill}}}
	eff := b.effectiveFor("run_command")
	if eff.OnOverflow != OverflowSpill {
		t.Fatalf("per-tool OnOverflow not applied: %v", eff.OnOverflow)
	}
	if eff.MaxOutputLines != 100 {
		t.Fatalf("base MaxOutputLines lost in merge: %d", eff.MaxOutputLines)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/tools/ -run TestShape -v`
Expected: FAIL — `undefined: ContextBudget` / `undefined: OverflowHeadTail`.

- [ ] **Step 3: Write minimal implementation**

```go
package tools

import (
	"fmt"
	"strings"
)

// OverflowStrategy selects how oversized tool output is shaped.
type OverflowStrategy string

const (
	OverflowHeadTail    OverflowStrategy = "head-tail"
	OverflowSpill       OverflowStrategy = "spill"
	OverflowPassthrough OverflowStrategy = "passthrough"
)

// ContextBudget caps how much tool output enters the context window.
type ContextBudget struct {
	MaxOutputLines    int
	MaxOutputBytes    int64
	OnOverflow        OverflowStrategy
	HeadLines         int
	TailLines         int
	SummaryLines      int
	EagerInstructions bool
	PerTool           map[string]ContextBudget
}

// SpillSink persists overflow output and returns a fetchable URI.
type SpillSink interface {
	Put(key, value string) (uri string, err error)
}

// ShapeResult is the outcome of shaping one tool's output.
type ShapeResult struct {
	Output        string
	Shaped        bool
	Strategy      OverflowStrategy
	OriginalLines int
	OriginalBytes int64
	SpillURI      string
}

// DefaultContextBudget returns the shipped defaults (protection on).
func DefaultContextBudget() ContextBudget {
	return ContextBudget{
		MaxOutputLines:    2000,
		MaxOutputBytes:    262144,
		OnOverflow:        OverflowHeadTail,
		HeadLines:         100,
		TailLines:         40,
		SummaryLines:      25,
		EagerInstructions: false,
	}
}

// effectiveFor merges a per-tool override (if any) over the base budget.
func (b ContextBudget) effectiveFor(toolName string) ContextBudget {
	pt, ok := b.PerTool[toolName]
	if !ok {
		return b
	}
	eff := b
	if pt.MaxOutputLines != 0 {
		eff.MaxOutputLines = pt.MaxOutputLines
	}
	if pt.MaxOutputBytes != 0 {
		eff.MaxOutputBytes = pt.MaxOutputBytes
	}
	if pt.OnOverflow != "" {
		eff.OnOverflow = pt.OnOverflow
	}
	if pt.HeadLines != 0 {
		eff.HeadLines = pt.HeadLines
	}
	if pt.TailLines != 0 {
		eff.TailLines = pt.TailLines
	}
	eff.PerTool = nil
	return eff
}

// Shape applies the budget to raw tool output.
func (b ContextBudget) Shape(toolName, raw string, sink SpillSink) ShapeResult {
	eff := b.effectiveFor(toolName)
	lines := strings.Split(raw, "\n")
	res := ShapeResult{
		Output:        raw,
		Strategy:      eff.OnOverflow,
		OriginalLines: len(lines),
		OriginalBytes: int64(len(raw)),
	}

	overLines := eff.MaxOutputLines > 0 && len(lines) > eff.MaxOutputLines
	overBytes := eff.MaxOutputBytes > 0 && int64(len(raw)) > eff.MaxOutputBytes
	if !overLines && !overBytes {
		return res // passthrough: under both caps.
	}
	if eff.OnOverflow == OverflowPassthrough {
		return res // explicit opt-out.
	}

	head := eff.HeadLines
	tail := eff.TailLines
	if head+tail == 0 {
		head = 1 // never produce an empty preview.
	}
	if head+tail >= len(lines) {
		head, tail = len(lines), 0
	}
	elidedLines := len(lines) - head - tail
	preview := b.assemble(lines, head, tail, res.OriginalLines, res.OriginalBytes, elidedLines)

	// Byte backstop: hard-truncate the assembled preview if still too big.
	if eff.MaxOutputBytes > 0 && int64(len(preview)) > eff.MaxOutputBytes {
		cut := int(eff.MaxOutputBytes)
		if cut > len(preview) {
			cut = len(preview)
		}
		preview = preview[:cut]
	}

	switch eff.OnOverflow {
	case OverflowSpill:
		if sink != nil {
			key := fmt.Sprintf("spill/%s", toolName)
			if uri, err := sink.Put(key, raw); err == nil {
				res.SpillURI = uri
				res.Output = preview + fmt.Sprintf("\n\nFull output saved to %s. Fetch it if you need the elided detail.", uri)
				res.Shaped = true
				return res
			}
		}
		// Degrade to head-tail when no sink or spill failed.
		res.Output = preview + "\n(spill unavailable — output truncated)"
		res.Strategy = OverflowHeadTail
		res.Shaped = true
		return res
	default: // OverflowHeadTail
		res.Output = preview
		res.Strategy = OverflowHeadTail
		res.Shaped = true
		return res
	}
}

// assemble builds the head + marker + tail preview.
func (b ContextBudget) assemble(lines []string, head, tail, totalLines int, totalBytes int64, elided int) string {
	var sb strings.Builder
	for i := 0; i < head && i < len(lines); i++ {
		sb.WriteString(lines[i])
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("[… %d lines / %d bytes elided — full output not returned …]\n", elided, totalBytes))
	for i := len(lines) - tail; i < len(lines); i++ {
		if i < 0 {
			continue
		}
		sb.WriteString(lines[i])
		if i < len(lines)-1 {
			sb.WriteString("\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/tools/ -run 'TestShape|TestEffectiveFor' -v`
Expected: PASS (all 5 tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w pkg/tools/shaper.go pkg/tools/shaper_test.go
git add pkg/tools/shaper.go pkg/tools/shaper_test.go
git commit -m "feat: add ContextBudget output shaper (head-tail/spill/passthrough)"
```

---

### Task 2: Wire the shaper into Executor.Run

**Files:**
- Modify: `pkg/tools/executor.go` (struct fields ~19-24; `NewExecutor` ~44-56; both success returns at ~158-162 and the builtin return ~76; error path ~144-149)
- Test: `pkg/tools/executor_shaping_test.go` (create)

**Interfaces:**
- Consumes: `ContextBudget`, `SpillSink`, `ShapeResult` from Task 1.
- Produces:
  - `func WithContextBudget(b ContextBudget, sink SpillSink) ExecutorOption`
  - Executor applies `budget.Shape(def.Name, output, sink)` to successful stdout/stderr output and to shaped error messages before returning.

- [ ] **Step 1: Write the failing test**

```go
package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecutor_ShapesBuiltinOutput(t *testing.T) {
	// Builtin tool that returns 50 lines.
	var big strings.Builder
	for i := 0; i < 50; i++ {
		big.WriteString("line\n")
	}
	def := BuiltinTool("bigcat", "returns many lines", map[string]any{"type": "object"},
		func(input map[string]any) (string, error) { return big.String(), nil })

	budget := ContextBudget{MaxOutputLines: 10, MaxOutputBytes: 100000, OnOverflow: OverflowHeadTail, HeadLines: 2, TailLines: 2}
	e := NewExecutor(time.Second, nil, WithContextBudget(budget, nil))

	out, err := e.Run(context.Background(), def, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "elided") {
		t.Fatalf("expected shaped output with elision marker, got:\n%s", out)
	}
	if got := strings.Count(out, "\n"); got > 10 {
		t.Fatalf("shaped output has %d newlines, expected <= 10", got)
	}
}

func TestExecutor_NoBudget_LeavesOutputUnchanged(t *testing.T) {
	def := BuiltinTool("echo3", "3 lines", map[string]any{"type": "object"},
		func(input map[string]any) (string, error) { return "a\nb\nc", nil })
	e := NewExecutor(time.Second, nil) // no WithContextBudget
	out, err := e.Run(context.Background(), def, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "a\nb\nc" {
		t.Fatalf("output changed without budget: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/tools/ -run TestExecutor_Shapes -v`
Expected: FAIL — `undefined: WithContextBudget`.

- [ ] **Step 3: Write minimal implementation**

In `pkg/tools/executor.go`, add fields to the `Executor` struct:

```go
type Executor struct {
	timeout       time.Duration
	logger        *slog.Logger
	defaultPolicy *CommandPolicy
	hook          ExecutionHook
	budget        *ContextBudget // nil = no shaping
	spillSink     SpillSink
}
```

Add the option:

```go
// WithContextBudget enables output shaping using the given budget and sink.
// A nil sink means spill degrades to head-tail truncation.
func WithContextBudget(b ContextBudget, sink SpillSink) ExecutorOption {
	return func(e *Executor) {
		bc := b
		e.budget = &bc
		e.spillSink = sink
	}
}
```

Add a private helper and apply it at every output return point:

```go
// shape applies the configured budget (if any) to output before returning.
func (e *Executor) shape(toolName, output string) string {
	if e.budget == nil {
		return output
	}
	return e.budget.Shape(toolName, output, e.spillSink).Output
}
```

Then change the return points:
- Builtin success (currently `return result, nil`): `return e.shape(def.Name, result), nil`
- CLI success (currently `return strings.TrimSpace(result), nil`): `return e.shape(def.Name, strings.TrimSpace(result)), nil`
- CLI failure error message: wrap the trimmed stderr with `e.shape(def.Name, strings.TrimSpace(errMsg))` before embedding it in the returned error.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/tools/ -run 'TestExecutor_Shapes|TestExecutor_NoBudget' -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w pkg/tools/executor.go pkg/tools/executor_shaping_test.go
git add pkg/tools/executor.go pkg/tools/executor_shaping_test.go
git commit -m "feat: apply ContextBudget shaping in tool executor"
```

---

### Task 3: SpillSink implementations (memory + temp file)

**Files:**
- Create: `pkg/tools/spill.go`
- Test: `pkg/tools/spill_test.go`

**Interfaces:**
- Consumes: `SpillSink` interface from Task 1; `*memory.Manager` (`Set(key, value string) error` from `pkg/memory/memory.go:33`).
- Produces:
  - `func NewMemorySink(agentName string, set func(key, value string) error) SpillSink`
  - `func NewTempFileSink(agentName string) SpillSink`

Note: `NewMemorySink` takes a `set` func (not `*memory.Manager` directly) to avoid a `pkg/tools` → `pkg/memory` import cycle, since `pkg/memory` imports `pkg/tools`.

- [ ] **Step 1: Write the failing test**

```go
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemorySink_Put(t *testing.T) {
	stored := map[string]string{}
	sink := NewMemorySink("my-agent", func(k, v string) error {
		stored[k] = v
		return nil
	})
	uri, err := sink.Put("spill/run_command", "big output")
	if err != nil {
		t.Fatalf("Put error: %v", err)
	}
	if !strings.HasPrefix(uri, "memory://my-agent/") {
		t.Fatalf("unexpected URI: %q", uri)
	}
	if len(stored) != 1 {
		t.Fatalf("expected 1 stored key, got %d", len(stored))
	}
}

func TestTempFileSink_Put(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	sink := NewTempFileSink("my-agent")
	uri, err := sink.Put("spill/run_command", "big output")
	if err != nil {
		t.Fatalf("Put error: %v", err)
	}
	if !strings.HasPrefix(uri, "file://") {
		t.Fatalf("unexpected URI: %q", uri)
	}
	path := strings.TrimPrefix(uri, "file://")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading spill file: %v", err)
	}
	if string(data) != "big output" {
		t.Fatalf("spill content mismatch: %q", string(data))
	}
	if !strings.Contains(path, filepath.Join(".abbyfile", "my-agent", "spill")) {
		t.Fatalf("spill path not under agent dir: %q", path)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/tools/ -run 'TestMemorySink|TestTempFileSink' -v`
Expected: FAIL — `undefined: NewMemorySink`.

- [ ] **Step 3: Write minimal implementation**

```go
package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

type memorySink struct {
	agentName string
	set       func(key, value string) error
}

// NewMemorySink returns a SpillSink that writes overflow into agent memory.
func NewMemorySink(agentName string, set func(key, value string) error) SpillSink {
	return &memorySink{agentName: agentName, set: set}
}

func (s *memorySink) Put(key, value string) (string, error) {
	fullKey := key + "-" + shortHash(value)
	if err := s.set(fullKey, value); err != nil {
		return "", fmt.Errorf("spill to memory: %w", err)
	}
	return fmt.Sprintf("memory://%s/%s", s.agentName, fullKey), nil
}

type tempFileSink struct {
	agentName string
}

// NewTempFileSink returns a SpillSink that writes overflow under
// ~/.abbyfile/<name>/spill/ and returns a file:// URI.
func NewTempFileSink(agentName string) SpillSink {
	return &tempFileSink{agentName: agentName}
}

func (s *tempFileSink) Put(key, value string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home: %w", err)
	}
	dir := filepath.Join(home, ".abbyfile", s.agentName, "spill")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating spill dir: %w", err)
	}
	name := filepath.Base(key) + "-" + shortHash(value) + ".txt"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		return "", fmt.Errorf("writing spill file: %w", err)
	}
	return "file://" + path, nil
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/tools/ -run 'TestMemorySink|TestTempFileSink' -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w pkg/tools/spill.go pkg/tools/spill_test.go
git add pkg/tools/spill.go pkg/tools/spill_test.go
git commit -m "feat: add memory and temp-file spill sinks"
```

---

### Task 4: Parse context_budget frontmatter block

**Files:**
- Modify: `pkg/definition/agent.go` (add `ContextBudget` to `AgentDef` ~33-42; add fields to `frontmatter2` ~53-60 and `abbyfileBlock` ~72-77; parse in `parseDualFormat` and `parseSingleFormat`; add `validateContextBudget`)
- Test: `pkg/definition/agent_test.go` (add cases; if file absent, create)

**Interfaces:**
- Consumes: nothing (definition layer is standalone; it uses its own struct, not `tools.ContextBudget`, to avoid importing `pkg/tools`).
- Produces:
  - `type ContextBudgetDef struct { MaxOutputLines int; MaxOutputBytes int64; OnOverflow string; HeadLines int; TailLines int; SummaryLines int; EagerInstructions *bool; PerTool map[string]ContextBudgetDef }` with yaml tags `max_output_lines`, `max_output_bytes`, `on_overflow`, `head_lines`, `tail_lines`, `summary_lines`, `eager_instructions`, `per_tool`.
  - `AgentDef.ContextBudget *ContextBudgetDef`
  - `func validateContextBudget(cb *ContextBudgetDef) error`

Note: `EagerInstructions` is `*bool` so "unset" is distinguishable from "explicitly false"; builder applies the shipped default (false) when nil.

- [ ] **Step 1: Write the failing test**

```go
package definition

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempAgent(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "agent.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseAgentMD_ContextBudget_Dual(t *testing.T) {
	md := `---
name: my-agent
memory: project
---

---
description: "test"
tools: Read
context_budget:
  max_output_lines: 500
  on_overflow: spill
  head_lines: 50
  tail_lines: 10
  per_tool:
    run_command:
      on_overflow: head-tail
---

Prompt body.`
	def, err := ParseAgentMD(writeTempAgent(t, md))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if def.ContextBudget == nil {
		t.Fatal("ContextBudget is nil")
	}
	if def.ContextBudget.MaxOutputLines != 500 {
		t.Fatalf("MaxOutputLines = %d, want 500", def.ContextBudget.MaxOutputLines)
	}
	if def.ContextBudget.OnOverflow != "spill" {
		t.Fatalf("OnOverflow = %q, want spill", def.ContextBudget.OnOverflow)
	}
	pt, ok := def.ContextBudget.PerTool["run_command"]
	if !ok || pt.OnOverflow != "head-tail" {
		t.Fatalf("per_tool run_command override missing/wrong: %+v", def.ContextBudget.PerTool)
	}
}

func TestParseAgentMD_ContextBudget_InvalidStrategy(t *testing.T) {
	md := `---
name: my-agent
---

---
description: "test"
tools: Read
context_budget:
  on_overflow: bogus
---

Body.`
	_, err := ParseAgentMD(writeTempAgent(t, md))
	if err == nil {
		t.Fatal("expected error for invalid on_overflow")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/definition/ -run TestParseAgentMD_ContextBudget -v`
Expected: FAIL — `def.ContextBudget undefined`.

- [ ] **Step 3: Write minimal implementation**

Add the type (near `SkillDef`):

```go
// ContextBudgetDef is the parsed context_budget frontmatter block.
type ContextBudgetDef struct {
	MaxOutputLines    int                         `yaml:"max_output_lines"`
	MaxOutputBytes    int64                       `yaml:"max_output_bytes"`
	OnOverflow        string                      `yaml:"on_overflow"`
	HeadLines         int                         `yaml:"head_lines"`
	TailLines         int                         `yaml:"tail_lines"`
	SummaryLines      int                         `yaml:"summary_lines"`
	EagerInstructions *bool                       `yaml:"eager_instructions"`
	PerTool           map[string]ContextBudgetDef `yaml:"per_tool"`
}
```

Add `ContextBudget *ContextBudgetDef` to `AgentDef`. Add `ContextBudget *ContextBudgetDef` with tag `yaml:"context_budget"` to both `frontmatter2` and `abbyfileBlock`. In `parseDualFormat`, after skills:

```go
if err := validateContextBudget(fm2.ContextBudget); err != nil {
	return nil, err
}
def.ContextBudget = fm2.ContextBudget
```

In `parseSingleFormat`, inside the `if sfm.Abbyfile != nil` block:

```go
if err := validateContextBudget(sfm.Abbyfile.ContextBudget); err != nil {
	return nil, err
}
def.ContextBudget = sfm.Abbyfile.ContextBudget
```

Add the validator:

```go
// validateContextBudget checks a context_budget block for legal values.
func validateContextBudget(cb *ContextBudgetDef) error {
	if cb == nil {
		return nil
	}
	check := func(c *ContextBudgetDef) error {
		switch c.OnOverflow {
		case "", "head-tail", "spill", "passthrough":
		default:
			return fmt.Errorf("context_budget: invalid on_overflow %q (want head-tail, spill, or passthrough)", c.OnOverflow)
		}
		if c.MaxOutputLines < 0 || c.MaxOutputBytes < 0 || c.HeadLines < 0 || c.TailLines < 0 || c.SummaryLines < 0 {
			return fmt.Errorf("context_budget: numeric fields must be non-negative")
		}
		return nil
	}
	if err := check(cb); err != nil {
		return err
	}
	for name, pt := range cb.PerTool {
		ptCopy := pt
		if err := check(&ptCopy); err != nil {
			return fmt.Errorf("context_budget.per_tool[%q]: %w", name, err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/definition/ -run TestParseAgentMD_ContextBudget -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w pkg/definition/agent.go pkg/definition/agent_test.go
git add pkg/definition/agent.go pkg/definition/agent_test.go
git commit -m "feat: parse context_budget frontmatter block"
```

---

### Task 5: ContextBudget config overrides + config get/set/reset

**Files:**
- Modify: `pkg/config/config.go` (add `ContextBudget` field + override struct + update `IsZero`)
- Modify: `pkg/config/loader.go` (`WriteFieldTo` ~90-97, `ResetFieldTo` ~120-131 — support dotted `context_budget.*` keys)
- Test: `pkg/config/config_test.go` (add cases)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type ContextBudgetOverride struct { MaxOutputLines *int; MaxOutputBytes *int64; OnOverflow *string; HeadLines *int; TailLines *int; SummaryLines *int; EagerInstructions *bool }` with matching yaml tags + `omitempty`.
  - `Config.ContextBudget *ContextBudgetOverride` (yaml `context_budget,omitempty`).
  - `WriteFieldTo` / `ResetFieldTo` handle keys `context_budget.max_output_lines`, `context_budget.max_output_bytes`, `context_budget.on_overflow`, `context_budget.head_lines`, `context_budget.tail_lines`, `context_budget.summary_lines`, `context_budget.eager_instructions`, and bare `context_budget` (reset only).

- [ ] **Step 1: Write the failing test**

```go
package config

import "testing"

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/config/ -run TestWriteResetField_ContextBudget -v`
Expected: FAIL — `cfg.ContextBudget undefined`.

- [ ] **Step 3: Write minimal implementation**

In `config.go`, add the override struct and field:

```go
// ContextBudgetOverride holds optional overrides for context budget limits.
type ContextBudgetOverride struct {
	MaxOutputLines    *int    `yaml:"max_output_lines,omitempty"`
	MaxOutputBytes    *int64  `yaml:"max_output_bytes,omitempty"`
	OnOverflow        *string `yaml:"on_overflow,omitempty"`
	HeadLines         *int    `yaml:"head_lines,omitempty"`
	TailLines         *int    `yaml:"tail_lines,omitempty"`
	SummaryLines      *int    `yaml:"summary_lines,omitempty"`
	EagerInstructions *bool   `yaml:"eager_instructions,omitempty"`
}
```

Add `ContextBudget *ContextBudgetOverride` with tag `yaml:"context_budget,omitempty"` to `Config`, and extend `IsZero` to include `&& c.ContextBudget == nil`.

In `loader.go`, import `strconv`. In `WriteFieldTo`, before the `default:` case, add handling that ensures `cfg.ContextBudget` is non-nil then sets the sub-field:

```go
case "context_budget.max_output_lines":
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("max_output_lines must be an integer: %w", err)
	}
	ensureBudget(cfg)
	cfg.ContextBudget.MaxOutputLines = &n
case "context_budget.max_output_bytes":
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("max_output_bytes must be an integer: %w", err)
	}
	ensureBudget(cfg)
	cfg.ContextBudget.MaxOutputBytes = &n
case "context_budget.on_overflow":
	switch value {
	case "head-tail", "spill", "passthrough":
	default:
		return fmt.Errorf("on_overflow must be head-tail, spill, or passthrough")
	}
	ensureBudget(cfg)
	v := value
	cfg.ContextBudget.OnOverflow = &v
case "context_budget.head_lines":
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("head_lines must be an integer: %w", err)
	}
	ensureBudget(cfg)
	cfg.ContextBudget.HeadLines = &n
case "context_budget.tail_lines":
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("tail_lines must be an integer: %w", err)
	}
	ensureBudget(cfg)
	cfg.ContextBudget.TailLines = &n
case "context_budget.summary_lines":
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("summary_lines must be an integer: %w", err)
	}
	ensureBudget(cfg)
	cfg.ContextBudget.SummaryLines = &n
case "context_budget.eager_instructions":
	b, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("eager_instructions must be true or false: %w", err)
	}
	ensureBudget(cfg)
	cfg.ContextBudget.EagerInstructions = &b
```

Add the helper at the end of `loader.go`:

```go
func ensureBudget(cfg *Config) {
	if cfg.ContextBudget == nil {
		cfg.ContextBudget = &ContextBudgetOverride{}
	}
}
```

In `ResetFieldTo`, add `case "context_budget": cfg.ContextBudget = nil`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/config/ -run 'TestWriteResetField_ContextBudget|TestWriteField_ContextBudget' -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w pkg/config/config.go pkg/config/loader.go pkg/config/config_test.go
git add pkg/config/config.go pkg/config/loader.go pkg/config/config_test.go
git commit -m "feat: add context_budget config overrides with dotted keys"
```

---

### Task 6: Agent wiring — build budget, apply overrides, pass to executor & bridge

**Files:**
- Modify: `pkg/agent/agent.go` (add `budget tools.ContextBudget` field ~24-47; init default in `New` ~50-53; extend `applyConfigOverrides` ~99-144; build sink + `WithContextBudget` in `Execute` ~199-205; pass `EagerInstructions` into `cli.Options`)
- Modify: `pkg/agent/options.go` (add `WithContextBudget`)
- Modify: `internal/cli/root.go` (add `EagerInstructions bool` to `Options` struct ~44-56)
- Modify: `internal/cli/serve_mcp.go` (accept + pass `eagerInstructions`)
- Test: `pkg/agent/agent_test.go` (add case)

**Interfaces:**
- Consumes: `tools.ContextBudget`, `tools.WithContextBudget`, `tools.NewMemorySink`, `tools.NewTempFileSink` (Tasks 1-3); `config.ContextBudgetOverride` (Task 5); `mgr.Set` (`pkg/memory/memory.go:33`).
- Produces:
  - `func WithContextBudget(b tools.ContextBudget) agent.Option`
  - Agent constructs an effective `tools.ContextBudget` (default → compiled → config override), builds a `SpillSink` (memory if enabled, else temp file), passes `WithContextBudget` to the executor, and threads `budget.EagerInstructions` to the bridge.

- [ ] **Step 1: Write the failing test**

```go
// in pkg/agent/agent_test.go
func TestApplyConfigOverrides_ContextBudget(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	lines := 42
	over := "spill"
	if err := config.WriteTo(cfgPath, &config.Config{
		ContextBudget: &config.ContextBudgetOverride{
			MaxOutputLines: &lines,
			OnOverflow:     &over,
		},
	}); err != nil {
		t.Fatal(err)
	}

	a, err := New(
		WithName("t"), WithVersion("0.0.1"),
		WithPromptFS(testPromptFS, "testdata/system.md"),
		WithConfigPath(cfgPath),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.budget.MaxOutputLines != 42 {
		t.Fatalf("MaxOutputLines = %d, want 42", a.budget.MaxOutputLines)
	}
	if a.budget.OnOverflow != tools.OverflowSpill {
		t.Fatalf("OnOverflow = %v, want spill", a.budget.OnOverflow)
	}
	// Untouched fields keep the shipped default.
	if a.budget.HeadLines != 100 {
		t.Fatalf("HeadLines = %d, want default 100", a.budget.HeadLines)
	}
}
```

Note: reuse whatever embedded FS the existing agent tests use for `WithPromptFS`; match the pattern already in `agent_test.go` (adjust `testPromptFS`/path names to the file's existing helper).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/agent/ -run TestApplyConfigOverrides_ContextBudget -v`
Expected: FAIL — `a.budget undefined`.

- [ ] **Step 3: Write minimal implementation**

In `agent.go`, add field `budget tools.ContextBudget` to `Agent`. In `New`, initialize before applying options:

```go
a := &Agent{
	toolTimeout: 30 * time.Second,
	budget:      tools.DefaultContextBudget(),
}
```

Add to `applyConfigOverrides`:

```go
if cfg.ContextBudget != nil {
	cb := cfg.ContextBudget
	if cb.MaxOutputLines != nil {
		a.budget.MaxOutputLines = *cb.MaxOutputLines
	}
	if cb.MaxOutputBytes != nil {
		a.budget.MaxOutputBytes = *cb.MaxOutputBytes
	}
	if cb.OnOverflow != nil {
		a.budget.OnOverflow = tools.OverflowStrategy(*cb.OnOverflow)
	}
	if cb.HeadLines != nil {
		a.budget.HeadLines = *cb.HeadLines
	}
	if cb.TailLines != nil {
		a.budget.TailLines = *cb.TailLines
	}
	if cb.SummaryLines != nil {
		a.budget.SummaryLines = *cb.SummaryLines
	}
	if cb.EagerInstructions != nil {
		a.budget.EagerInstructions = *cb.EagerInstructions
	}
}
```

In `Execute`, after building `execOpts` and after `mgr` is initialized, build the sink and append the budget option:

```go
var sink tools.SpillSink
if mgr != nil {
	sink = tools.NewMemorySink(a.name, mgr.Set)
} else {
	sink = tools.NewTempFileSink(a.name)
}
execOpts = append(execOpts, tools.WithContextBudget(a.budget, sink))
```

(Move the sink construction to a point after `mgr` is assigned but before `NewRunToolCommand`/`NewServeMCPCommand` are created.)

Set `cliOpts.EagerInstructions = a.budget.EagerInstructions` when building `cliOpts`.

In `options.go`, add:

```go
// WithContextBudget sets the compiled-in context budget for the agent.
func WithContextBudget(b tools.ContextBudget) Option {
	return func(a *Agent) { a.budget = b }
}
```

In `internal/cli/root.go`, add `EagerInstructions bool` to `Options`. In `internal/cli/serve_mcp.go`, add an `eagerInstructions bool` parameter and set `LazyToolLoading`/instructions behavior accordingly — pass it into `mcp.BridgeConfig` (new field, wired in Task 7). Update the `NewServeMCPCommand` call in `agent.go:209` to pass `a.budget.EagerInstructions`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/agent/ -run TestApplyConfigOverrides_ContextBudget -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w pkg/agent/agent.go pkg/agent/options.go pkg/agent/agent_test.go internal/cli/root.go internal/cli/serve_mcp.go
git add pkg/agent/agent.go pkg/agent/options.go pkg/agent/agent_test.go internal/cli/root.go internal/cli/serve_mcp.go
git commit -m "feat: wire context budget through agent runtime and executor"
```

---

### Task 7: Instructions fix — gate eager prompt injection in the MCP bridge

**Files:**
- Modify: `pkg/mcp/bridge.go` (`BridgeConfig` ~22-33 add `EagerInstructions bool`; `ServeTransport` ~58-68 gate instructions)
- Modify: `internal/cli/serve_mcp.go` (pass `EagerInstructions` into `BridgeConfig`)
- Test: `pkg/mcp/bridge_test.go` (add case)

**Interfaces:**
- Consumes: `BridgeConfig` (existing); `EagerInstructions` from Task 6's serve-mcp wiring.
- Produces:
  - `BridgeConfig.EagerInstructions bool`
  - `func (b *Bridge) handshakeInstructions() string` — returns full prompt when eager, else a short stub.
  - Full prompt remains reachable via the existing `system` prompt and `get_instructions` tool (unchanged).

- [ ] **Step 1: Write the failing test**

```go
// in pkg/mcp/bridge_test.go — follow the existing in-memory transport test setup.
func TestBridge_NonEagerInstructions_ReturnsStub(t *testing.T) {
	b := NewBridge(BridgeConfig{
		Name:            "t",
		Version:         "0.0.1",
		Description:     "A test agent",
		Loader:          testLoaderWithPrompt(t, "FULL SYSTEM PROMPT BODY THAT IS LONG"),
		Registry:        tools.NewRegistry(),
		EagerInstructions: false,
	})
	got := b.handshakeInstructions()
	if strings.Contains(got, "FULL SYSTEM PROMPT BODY") {
		t.Fatalf("non-eager handshake leaked full prompt: %q", got)
	}
	if !strings.Contains(got, "get_instructions") {
		t.Fatalf("stub should mention get_instructions: %q", got)
	}
}

func TestBridge_EagerInstructions_ReturnsFullPrompt(t *testing.T) {
	b := NewBridge(BridgeConfig{
		Name:            "t",
		Version:         "0.0.1",
		Loader:          testLoaderWithPrompt(t, "FULL SYSTEM PROMPT BODY THAT IS LONG"),
		Registry:        tools.NewRegistry(),
		EagerInstructions: true,
	})
	got := b.handshakeInstructions()
	if !strings.Contains(got, "FULL SYSTEM PROMPT BODY") {
		t.Fatalf("eager handshake missing full prompt: %q", got)
	}
}
```

Note: implement `testLoaderWithPrompt` using the same prompt-loader construction the existing bridge tests use (embedded FS or override path); match the file's existing helpers.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/mcp/ -run TestBridge_.*Instructions -v`
Expected: FAIL — `b.handshakeInstructions undefined`.

- [ ] **Step 3: Write minimal implementation**

Add `EagerInstructions bool` to `BridgeConfig`. Add the method:

```go
// handshakeInstructions returns what the MCP handshake advertises. When
// EagerInstructions is false, it returns a short stub and keeps the full
// prompt available on demand via the `system` prompt / get_instructions tool.
func (b *Bridge) handshakeInstructions() string {
	full, _ := b.cfg.Loader.Load()
	full = b.appendModelHint(full)
	if b.cfg.EagerInstructions {
		return full
	}
	role := b.cfg.Description
	if role == "" {
		role = b.cfg.Name
	}
	return role + "\n\nFull instructions are available via the `system` prompt or the `get_instructions` tool."
}
```

In `ServeTransport`, replace lines ~60-61:

```go
instructions := b.handshakeInstructions()
```

(remove the old `instructions, _ := b.cfg.Loader.Load()` + `appendModelHint` pair, since `handshakeInstructions` now owns both paths).

In `internal/cli/serve_mcp.go`, set `EagerInstructions: eagerInstructions` in the `mcp.BridgeConfig` literal.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/mcp/ -run TestBridge_.*Instructions -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w pkg/mcp/bridge.go internal/cli/serve_mcp.go pkg/mcp/bridge_test.go
git add pkg/mcp/bridge.go internal/cli/serve_mcp.go pkg/mcp/bridge_test.go
git commit -m "feat: gate eager instruction injection at MCP handshake"
```

---

### Task 8: Pass ContextBudget through code generation (builder)

**Files:**
- Modify: `pkg/builder/builder.go` (`templateData` ~42-51; `GenerateSource` ~190-199 map fields)
- Modify: `pkg/builder/templates/main.go.tmpl` (~67-76 emit `agent.WithContextBudget(...)`)
- Test: `pkg/builder/builder_test.go` (add case)

**Interfaces:**
- Consumes: `definition.ContextBudgetDef` (Task 4).
- Produces: generated `main.go` calls `agent.WithContextBudget(tools.ContextBudget{...})` reflecting frontmatter, so compiled binaries carry authored defaults. Requires the `tools` import in generated `main.go` whenever a budget or custom tools are present.

- [ ] **Step 1: Write the failing test**

```go
// in pkg/builder/builder_test.go
func TestGenerateSource_EmitsContextBudget(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name: "b", Version: "0.0.1", Description: "d", Tools: []string{"Read"},
		PromptBody: "body",
		ContextBudget: &definition.ContextBudgetDef{
			MaxOutputLines: 500,
			OnOverflow:     "spill",
			HeadLines:      50,
		},
	}
	if err := GenerateSource(dir, def, "v0.9.1", ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "agent.WithContextBudget(") {
		t.Fatalf("main.go missing WithContextBudget:\n%s", s)
	}
	if !strings.Contains(s, "MaxOutputLines: 500") {
		t.Fatalf("main.go missing MaxOutputLines value:\n%s", s)
	}
	if !strings.Contains(s, `OnOverflow: tools.OverflowStrategy("spill")`) {
		t.Fatalf("main.go missing OnOverflow:\n%s", s)
	}
}

func TestGenerateSource_NoBudget_NoWithContextBudget(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{Name: "b", Version: "0.0.1", Tools: []string{"Read"}, PromptBody: "x"}
	if err := GenerateSource(dir, def, "v0.9.1", ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if strings.Contains(string(data), "WithContextBudget") {
		t.Fatal("main.go should omit WithContextBudget when no budget declared")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/builder/ -run TestGenerateSource_.*Budget -v`
Expected: FAIL — generated `main.go` lacks `WithContextBudget`.

- [ ] **Step 3: Write minimal implementation**

Add to `templateData`:

```go
type budgetData struct {
	MaxOutputLines    int
	MaxOutputBytes    int64
	OnOverflow        string
	HeadLines         int
	TailLines         int
	SummaryLines      int
	EagerInstructions bool
	PerTool           map[string]budgetData
}
```

Add `Budget *budgetData` to `templateData`. In `GenerateSource`, map `def.ContextBudget` into it when non-nil (copy scalar fields + per-tool). Because the template needs the `tools` import when a budget exists, change the import guard: emit the `tools` import when `.CustomTools` **or** `.Budget` is set.

In `main.go.tmpl`, update the import block condition to `{{- if or .CustomTools .Budget}}` for the `tools` import, and add inside the `agent.New(...)` call:

```gotemplate
		{{- if .Budget}}
		agent.WithContextBudget(tools.ContextBudget{
			MaxOutputLines:    {{.Budget.MaxOutputLines}},
			MaxOutputBytes:    {{.Budget.MaxOutputBytes}},
			OnOverflow:        tools.OverflowStrategy({{printf "%q" .Budget.OnOverflow}}),
			HeadLines:         {{.Budget.HeadLines}},
			TailLines:         {{.Budget.TailLines}},
			SummaryLines:      {{.Budget.SummaryLines}},
			EagerInstructions: {{.Budget.EagerInstructions}},
			{{- if .Budget.PerTool}}
			PerTool: map[string]tools.ContextBudget{
				{{- range $k, $v := .Budget.PerTool}}
				{{printf "%q" $k}}: {
					MaxOutputLines: {{$v.MaxOutputLines}},
					MaxOutputBytes: {{$v.MaxOutputBytes}},
					OnOverflow:     tools.OverflowStrategy({{printf "%q" $v.OnOverflow}}),
					HeadLines:      {{$v.HeadLines}},
					TailLines:      {{$v.TailLines}},
				},
				{{- end}}
			},
			{{- end}}
		}),
		{{- end}}
```

Note: when `OnOverflow` is empty in frontmatter, map it to `"head-tail"` in `GenerateSource` so generated code never sets an empty strategy (which would break the shaper's overflow branch). Apply the shipped defaults for any zero-valued scalar when `Budget` is non-nil, so a partial frontmatter block still yields a complete budget.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/builder/ -run TestGenerateSource_ -v`
Expected: PASS (both new tests plus existing generate tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w pkg/builder/builder.go pkg/builder/builder_test.go
git add pkg/builder/builder.go pkg/builder/builder_test.go pkg/builder/templates/main.go.tmpl
git commit -m "feat: emit WithContextBudget in generated agent source"
```

---

### Task 9: Layer A — emit .claude/agents/<name>.md sub-agent

**Files:**
- Create: `pkg/subagent/subagent.go`
- Test: `pkg/subagent/subagent_test.go`
- Modify: `cmd/abby/build.go` (add `--subagent` flag ~40-71; call `subagent.Generate` in the plugin/subagent block ~150-173)

**Interfaces:**
- Consumes: `definition.AgentDef` (fields `Name`, `Description`, `Tools`, `PromptBody`, `ContextBudget`); model hint if present.
- Produces:
  - `type GenerateConfig struct { OutputDir string; Model string }`
  - `func Generate(def *definition.AgentDef, cfg GenerateConfig) (string, error)` — writes `<OutputDir>/.claude/agents/<name>.md`, returns the written path.
  - `func summaryLines(def *definition.AgentDef) int` — returns `def.ContextBudget.SummaryLines` if > 0, else 25.

- [ ] **Step 1: Write the failing test**

```go
package subagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/definition"
)

func TestGenerate_WritesSubagentMarkdown(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name:        "reviewer",
		Description: "Reviews Go code",
		Tools:       []string{"Read", "Grep"},
		PromptBody:  "You review Go code carefully.",
	}
	path, err := Generate(def, GenerateConfig{OutputDir: dir, Model: "opus"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := filepath.Join(dir, ".claude", "agents", "reviewer.md")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, sub := range []string{
		"name: reviewer",
		"description: Reviews Go code",
		"model: opus",
		"You review Go code carefully.",
		"## Return Protocol",
		"isolated context window",
		"25-line",
	} {
		if !strings.Contains(s, sub) {
			t.Fatalf("output missing %q:\n%s", sub, s)
		}
	}
}

func TestSummaryLines_DefaultAndOverride(t *testing.T) {
	if got := summaryLines(&definition.AgentDef{}); got != 25 {
		t.Fatalf("default summaryLines = %d, want 25", got)
	}
	def := &definition.AgentDef{ContextBudget: &definition.ContextBudgetDef{SummaryLines: 10}}
	if got := summaryLines(def); got != 10 {
		t.Fatalf("override summaryLines = %d, want 10", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/subagent/ -v`
Expected: FAIL — package/function undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// Package subagent emits a Claude Code sub-agent markdown file so a packaged
// agent can run in its own context window and return a bounded summary.
package subagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/teabranch/abbyfile/pkg/definition"
)

// GenerateConfig configures sub-agent emission.
type GenerateConfig struct {
	OutputDir string // parent dir; file goes to <OutputDir>/.claude/agents/<name>.md
	Model     string // optional model hint
}

// summaryLines returns the return-protocol summary cap.
func summaryLines(def *definition.AgentDef) int {
	if def.ContextBudget != nil && def.ContextBudget.SummaryLines > 0 {
		return def.ContextBudget.SummaryLines
	}
	return 25
}

// Generate writes .claude/agents/<name>.md and returns the file path.
func Generate(def *definition.AgentDef, cfg GenerateConfig) (string, error) {
	agentsDir := filepath.Join(cfg.OutputDir, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return "", fmt.Errorf("creating agents dir: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("name: " + def.Name + "\n")
	if def.Description != "" {
		sb.WriteString("description: " + def.Description + "\n")
	}
	if len(def.Tools) > 0 {
		sb.WriteString("tools: " + strings.Join(def.Tools, ", ") + "\n")
	}
	if cfg.Model != "" {
		sb.WriteString("model: " + cfg.Model + "\n")
	}
	sb.WriteString("---\n\n")
	sb.WriteString(def.PromptBody)
	sb.WriteString("\n\n## Return Protocol\n")
	sb.WriteString(fmt.Sprintf(
		"You run in an isolated context window. When you finish, return ONLY:\n"+
			"1. A ≤%d-line summary of what you did and the outcome.\n"+
			"2. Concrete artifacts the caller needs (file paths, IDs, final values).\n"+
			"Do NOT paste raw tool output, file dumps, or logs into your final message —\n"+
			"they stay in your context, not the caller's. If the caller needs full detail,\n"+
			"reference where it lives (a path or memory:// URI) instead of inlining it.\n",
		summaryLines(def)))

	path := filepath.Join(agentsDir, def.Name+".md")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return "", fmt.Errorf("writing sub-agent file: %w", err)
	}
	return path, nil
}
```

In `cmd/abby/build.go`: add `var subagentFlag bool` and `cmd.Flags().BoolVar(&subagentFlag, "subagent", false, "Also emit a Claude Code sub-agent (.claude/agents/<name>.md)")`. Pass it through `runBuild`. In the generation block, emit when `pluginOutput || subagentFlag`:

```go
if pluginOutput || subagentFlag {
	for name, def := range defs {
		p, err := subagent.Generate(def, subagent.GenerateConfig{
			OutputDir: outputDir,
			Model:     "", // model hint not yet threaded from Abbyfile; wire when available
		})
		if err != nil {
			return fmt.Errorf("generating sub-agent for %s: %w", name, err)
		}
		fmt.Fprintf(os.Stderr, "→ %s\n", p)
	}
}
```

Add the `subagent` import to `cmd/abby/build.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/subagent/ -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w pkg/subagent/subagent.go pkg/subagent/subagent_test.go cmd/abby/build.go
git add pkg/subagent/subagent.go pkg/subagent/subagent_test.go cmd/abby/build.go
git commit -m "feat: emit Claude Code sub-agent with return protocol"
```

---

### Task 10: config get displays context_budget; end-to-end integration test

**Files:**
- Modify: `internal/cli/config.go` (`printAllFields` ~133-169; `printField` ~105-130 — add `context_budget` rows/case)
- Modify: `internal/cli/config.go` `CompiledDefaults` ~13-16 — add budget defaults for display
- Modify: `pkg/agent/agent.go` `compiledDefaults()` ~91-96 — populate budget defaults
- Test: `internal/integration/config_test.go` or new `internal/integration/budget_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1-9.
- Produces: `config get` shows `context_budget.*` effective values; an integration test proves shaping caps real tool output end-to-end.

- [ ] **Step 1: Write the failing test**

```go
// internal/integration/budget_test.go — follow the build+run harness the other
// integration tests use (build a binary from a temp Abbyfile, invoke run-tool).
func TestIntegration_ContextBudgetCapsToolOutput(t *testing.T) {
	// Build an agent whose custom tool emits 1000 lines, budget capped at 20.
	// Then invoke run-tool and assert the output contains the elision marker
	// and far fewer than 1000 lines.
	// (Reuse helpers from the existing integration tests: buildTestAgent, etc.)
}
```

Match the existing integration harness in `internal/integration/`. The assertion: captured `run-tool` stdout contains `"elided"` and `strings.Count(out, "\n") < 100`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/integration/ -run TestIntegration_ContextBudgetCapsToolOutput -v`
Expected: FAIL (before display/wiring is complete, or the harness assertion is unmet).

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/config.go`, extend `CompiledDefaults`:

```go
type CompiledDefaults struct {
	Model             string
	ToolTimeout       time.Duration
	MaxOutputLines    int
	MaxOutputBytes    int64
	OnOverflow        string
	EagerInstructions bool
}
```

In `printAllFields`, add rows for `context_budget.max_output_lines`, `context_budget.max_output_bytes`, `context_budget.on_overflow`, `context_budget.eager_instructions`, each showing the effective value and source (`compiled` vs `override`) using `cfg.ContextBudget`. Add matching cases to `printField`.

In `pkg/agent/agent.go` `compiledDefaults()`, populate the new fields from `a.budget`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/integration/ -run TestIntegration_ContextBudgetCapsToolOutput -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/cli/config.go pkg/agent/agent.go internal/integration/budget_test.go
git add internal/cli/config.go pkg/agent/agent.go internal/integration/budget_test.go
git commit -m "feat: show context_budget in config get; add e2e shaping test"
```

---

### Task 11: Full suite, validate wiring, and docs

**Files:**
- Modify: `internal/cli/validate.go` (add a check that a declared `context_budget.on_overflow` is legal, mirroring build-time validation, if the validate command inspects frontmatter — otherwise skip and note it)
- Create: `docs/guides/context-budget.md`
- Modify: `docs/concepts.md` (isolation note), `README.md` (comparison table row)

**Interfaces:**
- Consumes: all prior tasks.
- Produces: green `go test -race ./...`; user-facing docs.

- [ ] **Step 1: Run the full test suite with race detector**

Run: `go test -race ./...`
Expected: PASS across all packages. Fix any failures before proceeding (common: gofmt, unused imports, the `tools` import guard in the generated template).

- [ ] **Step 2: Build the CLI and smoke-test end to end**

```bash
go build -o /tmp/abby ./cmd/abby
cd $(mktemp -d) && mkdir -p agents
cat > Abbyfile <<'EOF'
version: "1"
agents:
  demo:
    path: agents/demo.md
    version: 0.1.0
EOF
cat > agents/demo.md <<'EOF'
---
name: demo
---

---
description: "demo agent"
tools: Read
context_budget:
  max_output_lines: 50
  on_overflow: head-tail
---

You are a demo agent.
EOF
/tmp/abby build --module-dir <path-to-repo> --subagent
```

Expected: build succeeds; `build/demo` exists; `build/.claude/agents/demo.md` exists and contains `## Return Protocol`. `./build/demo config get` shows `context_budget.max_output_lines: 50 (compiled)`.

- [ ] **Step 3: Write the docs**

Create `docs/guides/context-budget.md` covering: the two layers, the `context_budget:` frontmatter block (all fields + `per_tool`), the three strategies with examples, shipped defaults, consumer overrides via `config set context_budget.*`, the `--subagent` flag, and the `eager_instructions` behavior. Update `docs/concepts.md` so the `Context isolation: No` line notes isolation is available via `--subagent`. Add a context-budget row to the `README.md` comparison table.

- [ ] **Step 4: Verify docs build/links (if the site tooling is available)**

Run: `grep -r "context-budget.md" docs/ README.md` to confirm cross-links resolve; visually confirm the new guide renders.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/cli/validate.go
git add internal/cli/validate.go docs/guides/context-budget.md docs/concepts.md README.md
git commit -m "docs: document context-budget controls and sub-agent emission"
```

---

## Self-Review Notes

- **Spec coverage:** Data model → Tasks 1,4,5,6; Shaper (head-tail/spill/passthrough, byte backstop, sink degradation, error shaping) → Tasks 1,2,3; SpillSink (memory + temp file) → Task 3; Layer A sub-agent + return protocol → Task 9; instructions fix → Task 7; MaxOutputBytes reconciliation → covered by the single effective byte cap in Tasks 1/6 (the dead policy cap is superseded by `ContextBudget.MaxOutputBytes`; a follow-up may alias `CommandPolicy.MaxOutputBytes` into the budget when the budget leaves bytes unset — noted here, not required for the floor); config get/set/reset → Tasks 5,10; defaults on → Task 6; docs/benchmarks → Task 11.
- **Import-cycle guard:** `pkg/memory` imports `pkg/tools`, so `NewMemorySink` takes a `set func` rather than `*memory.Manager` (Task 3). `pkg/definition` uses its own `ContextBudgetDef` and does not import `pkg/tools` (Task 4).
- **Type consistency:** `ContextBudget` (runtime, `pkg/tools`) vs `ContextBudgetDef` (frontmatter, `pkg/definition`) vs `ContextBudgetOverride` (config, `pkg/config`) vs `budgetData` (template, `pkg/builder`) are intentionally distinct structs at layer boundaries; Task 6/8 map between them explicitly.
- **Open follow-up (not blocking):** benchmark comparison (`benchmarks/`) mentioned in the spec is deferred; add after the feature lands so numbers reflect the shipped shaper.
```