# MCP 2026-07-28 Adaptation — Phase D (Efficiency) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every Abbyfile agent pays only for the context it needs. Broken lazy tool loading goes away. `get_instructions` exists only when the handshake points at it. Tools can opt in to Claude Code's larger inline-result limit. The before/after cost is measured and published.

**Architecture:** All protocol-facing changes live in `pkg/mcp/bridge.go`.
- `search_tools` and the lazy branch are deleted. `BridgeConfig.LazyToolLoading` and `agent.WithLazyToolLoading` stay as deprecated no-ops that warn.
- `get_instructions` is registered only when `EagerInstructions` is false. In that mode the stub text tells the model to call it.
- A new per-tool `inline_large` budget flag flows through the whole chain: frontmatter (`pkg/definition`) → generated code (`pkg/builder`) → `tools.ContextBudget` → `Executor.ResultSizeHint`. The bridge then emits it as `_meta["anthropic/maxResultSizeChars"]` on the tool definition.
- `benchmarks.MeasureHandshake` is fixed to measure the instructions the handshake actually sent. It captures a baseline first and the new numbers last.

**Tech Stack:** Go 1.26, go-sdk v1.8.0, cobra, standard `testing`.

**Spec:** `docs/superpowers/specs/2026-09-27-mcp-2026-07-28-adaptation-design.md` (Phase D section, defects D1 and D10, Migration note items 3–4).

## Global Constraints

- go-sdk stays at **v1.8.0**. No dependency changes in this phase.
- Do **not** set any `MCPGODEBUG` escape hatch. Do **not** disable `GOSUMDB` or use `GONOSUMDB`/`GOINSECURE`/`GOFLAGS=-insecure`.
- `eager_instructions` default stays **false**.
- Stub text (non-eager), verbatim, after the role line and a blank line: ``Call the `get_instructions` tool to load your full instructions before acting.``
- `get_instructions` description must not contain the word "Deprecated".
- `WithLazyToolLoading` and `BridgeConfig.LazyToolLoading` are kept for one release. They are marked `// Deprecated:`, have no effect, and log a warning when set to true.
- Result-size hint key: `anthropic/maxResultSizeChars`, placed in the **tool definition's** `_meta` in `tools/list` (not in `CallToolResult._meta`). This was verified against https://code.claude.com/docs/en/mcp.md ("MCP output limits and warnings"). The hint can only raise Claude Code's threshold, never lower it.
- Hint ceiling: **500000**. The hint is emitted only for tools with `context_budget.per_tool.<name>.inline_large: true`. It is never emitted by default, and no efficiency credit is claimed for it.
- Tool-level failures stay `CallToolResult{IsError:true}`. Logging goes to stderr via `slog`.
- Commit format: `<type>: <description>` + trailer `Co-Authored-By: Claude <noreply@anthropic.com>`. Never use `--no-verify`. Never rewrite existing commits.
- Out of scope: sandboxing (Phase B) and install/runtime config (Phase C). Added to Phase D at the user's request (2026-09-27): the memory spill-key bug (Task 6) and the stale Go-version and structured-output doc references (Task 7).

## Decisions taken in this plan

- **Cached `tools/list` after an `eager_instructions` flip.** Phase A marks `tools/list` cacheable for 1h with public scope. `get_instructions` presence now depends on a runtime config value. So a client holding a cached list after `config set context_budget.eager_instructions …` could see a stub naming a tool it has not re-listed. Resolution: keep the cache hints, because the list is static for the lifetime of a server process, and config is read only at process start. Also document in `context-budget.md` that you must restart the runtime session after flipping this value (Task 3).
- **`inline_large` bounds.** At build time, reject an effective cap that is unlimited (0) or above 500000, naming the tool. At runtime a `config set context_budget.max_output_bytes` can still change the base cap. There, the bridge clamps values above 500000 and omits the hint, with a warning, when the cap is ≤0.
- **Generated-code compatibility.** `InlineLarge: true` is emitted only for tools that opt in, so existing agents compile against older published abbyfile modules. v0.10.0 release checklist: the builder's `ModuleVersion` pin must be ≥ the release that adds `tools.ContextBudget.InlineLarge`.
- **D-1 cost.** Lazy loading was never wired from `serve-mcp`, so removing it changes no measured number. The benchmark doc says so rather than claiming a saving.

## Review Focus

1. **An agent with `eager_instructions: true` and a model hint.** A user expects the model preference to still reach the model once `get_instructions` is gone, now through the handshake instructions. → Task 3, Step 1 (`TestInstructionsModes`, `claude-opus-4-6` assertion).
2. **A user who flips `eager_instructions` at runtime with `config set`** instead of frontmatter. They expect `serve-mcp` to honour it: full instructions, no `get_instructions`. → Task 3, Step 7 (`TestServeMCPEagerInstructionsOverride`).
3. **A library caller still passing `WithLazyToolLoading(true)` / `LazyToolLoading: true`.** They expect the code to compile, every tool to be listed and callable, and a warning explaining why. → Task 2, Steps 1–2.
4. **`inline_large: true` on a tool whose effective byte cap is unlimited or above 500000.** At build time they expect a clear error naming the tool. At runtime, after a config override, they expect a clamp or omission, never a hint above the ceiling or a hint of 0. → Task 4, Steps 1, 3, 5.
5. **`inline_large` at the base `context_budget` level** (not under `per_tool`). They expect a parse error, not a silently ignored flag. → Task 4, Step 1 (`TestParseAgentMD_InlineLarge_BaseLevelRejected`).

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `benchmarks/measure.go` | Handshake measurement | Modify: measure the instructions actually sent (`InstructionsBytes/Tokens`, `HandshakeTokens`) |
| `benchmarks/context_cost_test.go` | Phase D before/after cost table | Create |
| `Makefile` | `bench-report` target | Modify: include `TestHandshakeContextCost` |
| `docs/guides/benchmarks.md` | Published numbers | Modify: "Per-session handshake cost" section (Task 1 before, Task 5 after) |
| `pkg/mcp/bridge.go` | Registry → MCP server | Modify: delete lazy path, conditional `get_instructions`, stub text, tool `_meta` hint |
| `pkg/mcp/bridge_test.go` | Bridge black-box tests | Modify: replace lazy test, add hint test |
| `pkg/mcp/instructions_test.go` | Eager/non-eager × era matrix | Create |
| `pkg/mcp/testdata/tools_list.golden.json` | Wire snapshot | Regenerate (description change) |
| `pkg/agent/options.go`, `pkg/agent/agent.go`, `pkg/agent/agent_test.go` | Deprecated option | Modify |
| `pkg/definition/agent.go`, `pkg/definition/agent_test.go` | `inline_large` parse + validation | Modify |
| `pkg/builder/builder.go`, `pkg/builder/templates/main.go.tmpl`, `pkg/builder/builder_test.go` | Emit `InlineLarge` in generated code | Modify |
| `pkg/tools/shaper.go`, `pkg/tools/executor.go`, `pkg/tools/executor_raw_test.go` | `InlineLarge` field, `ResultSizeHint` | Modify |
| `internal/integration/config_test.go` | Runtime eager override over real stdio | Modify |
| `benchmarks/bench_test.go` | `TestRedundancyAudit` note text | Modify |
| `docs/reference.md`, `docs/guides/mcp.md`, `docs/guides/prompts.md`, `docs/guides/context-budget.md` | User docs | Modify |
| `pkg/tools/spill.go`, `pkg/tools/spill_test.go`, `pkg/tools/spill_memory_test.go` | Memory spill sink | Modify: flatten keys (Task 6) |
| `docs/quickstart.md`, `docs/faq.md`, `docs/development.md`, `pkg/mcp/structured.go` | Stale version and null wording | Modify (Task 7) |

---

### Task 1: Measure what the handshake actually sends, and record the baseline

**Why first:** `MeasureHandshake` reports `PromptTokens` from `cfg.Loader.Load()`. That is the full prompt regardless of `EagerInstructions`, so eager and non-eager look identical today. The baseline must come from the code **before** Tasks 2–4, which is current `main` (53dd5a6).

**Files:**
- Modify: `benchmarks/measure.go` (`HandshakePayload`, `MeasureHandshake`)
- Create: `benchmarks/context_cost_test.go`
- Modify: `Makefile` (`bench-report`)
- Modify: `docs/guides/benchmarks.md` (new section after "### Multi-Turn Cost Projection")

**Interfaces:**
- Consumes: `benchmarks.MeasureHandshake(cfg agentmcp.BridgeConfig) (*HandshakePayload, error)`, `builtins.All()`, `memory.NewFileStoreAt`, `memory.NewManager`, `mgr.Tools()`.
- Produces: three new fields, `HandshakePayload.InstructionsBytes int`, `HandshakePayload.InstructionsTokens int` and `HandshakePayload.HandshakeTokens int` (= `TotalSchemaTokens + InstructionsTokens`). Also `representativeConfig(t *testing.T, eager bool) agentmcp.BridgeConfig` and `TestHandshakeContextCost` in `benchmarks/context_cost_test.go`; Task 5 adds assertions to them. Existing `PromptTokens`/`TotalTokens` are unchanged, because other tests depend on them.

- [ ] **Step 1: Write the failing test**

Create `benchmarks/context_cost_test.go`:

```go
package benchmarks_test

import (
	"testing"

	"github.com/teabranch/abbyfile/benchmarks"
	"github.com/teabranch/abbyfile/pkg/builtins"
	agentmcp "github.com/teabranch/abbyfile/pkg/mcp"
	"github.com/teabranch/abbyfile/pkg/memory"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// representativeConfig is the agent Phase D measures: all builtins, memory
// enabled, and the benchmark system prompt (testdata/system.md, ~3.4 KB).
func representativeConfig(t *testing.T, eager bool) agentmcp.BridgeConfig {
	t.Helper()
	store, err := memory.NewFileStoreAt(t.TempDir(), memory.Limits{})
	if err != nil {
		t.Fatalf("creating file store: %v", err)
	}
	mgr := memory.NewManager(store)

	registry := tools.NewRegistry()
	for _, def := range append(builtins.All(), mgr.Tools()...) {
		if err := registry.Register(def); err != nil {
			t.Fatalf("register %s: %v", def.Name, err)
		}
	}
	cfg := bridgeConfig(t, registry, mgr)
	cfg.Description = "Benchmark agent"
	cfg.EagerInstructions = eager
	return cfg
}

// TestHandshakeContextCost reports the per-session context cost (tools/list
// schemas + handshake instructions) in both eager modes. Numbers are
// recorded in docs/guides/benchmarks.md ("Per-session handshake cost").
func TestHandshakeContextCost(t *testing.T) {
	results := map[bool]*benchmarks.HandshakePayload{}
	for _, eager := range []bool{false, true} {
		p, err := benchmarks.MeasureHandshake(representativeConfig(t, eager))
		if err != nil {
			t.Fatalf("eager=%t: %v", eager, err)
		}
		results[eager] = p
		names := make([]string, len(p.Tools))
		for i, tm := range p.Tools {
			names[i] = tm.Name
		}
		t.Logf("| eager_instructions=%t | %d tools | tools/list ~%d tokens | instructions ~%d tokens | total ~%d tokens | %v",
			eager, len(p.Tools), p.TotalSchemaTokens, p.InstructionsTokens, p.HandshakeTokens, names)
	}

	stub, eager := results[false], results[true]
	if stub.InstructionsTokens == 0 || eager.InstructionsTokens == 0 {
		t.Fatalf("instructions not measured: stub=%d eager=%d", stub.InstructionsTokens, eager.InstructionsTokens)
	}
	if eager.InstructionsTokens <= stub.InstructionsTokens {
		t.Errorf("eager instructions (%d tokens) should exceed the stub (%d tokens)",
			eager.InstructionsTokens, stub.InstructionsTokens)
	}
	if stub.HandshakeTokens != stub.TotalSchemaTokens+stub.InstructionsTokens {
		t.Errorf("HandshakeTokens = %d, want schema+instructions = %d",
			stub.HandshakeTokens, stub.TotalSchemaTokens+stub.InstructionsTokens)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./benchmarks/ -run TestHandshakeContextCost -v`
Expected: compile FAIL, `p.InstructionsTokens undefined` / `p.HandshakeTokens undefined`.

- [ ] **Step 3: Implement the measurement**

In `benchmarks/measure.go`, add three fields to `HandshakePayload` after `PromptTokens`:

```go
	// InstructionsBytes/Tokens measure the instructions the server actually
	// sent (server/discover or initialize), which is the stub when
	// EagerInstructions is false. PromptBytes/Tokens measure the full prompt.
	InstructionsBytes  int `json:"instructions_bytes"`
	InstructionsTokens int `json:"instructions_tokens"`
	// HandshakeTokens is the per-session cost the client pays up front:
	// tools/list schemas + handshake instructions.
	HandshakeTokens int `json:"handshake_tokens"`
```

In `MeasureHandshake`, after the `payload := &HandshakePayload{}` tool loop and before `// Measure system prompt.`, add:

```go
	if ir := session.InitializeResult(); ir != nil {
		payload.InstructionsBytes = len(ir.Instructions)
		payload.InstructionsTokens = EstimateTokens(ir.Instructions)
	}
```

At the end, next to the `TotalTokens` line, add:

```go
	payload.HandshakeTokens = payload.TotalSchemaTokens + payload.InstructionsTokens
```

- [ ] **Step 4: Run it to verify it passes and capture the baseline**

Run: `go test ./benchmarks/ -run TestHandshakeContextCost -v`
Expected: PASS, with two `|`-rows logged. Both rows list `get_instructions`; neither lists `search_tools`. Copy both rows; they are the **before** numbers.

- [ ] **Step 5: Add to `bench-report` and record the baseline**

In `Makefile`, append `|TestHandshakeContextCost` inside the `-run "…"` regex of `bench-report`.

In `docs/guides/benchmarks.md`, insert after the "### Multi-Turn Cost Projection" section, before "### Claude Code Baseline Analysis":

```markdown
### Per-session handshake cost (v0.10.0, Phase D)

What a client pays before the first tool call: `tools/list` schemas plus the
server `instructions` actually delivered by `server/discover` / `initialize`.
Representative agent: all 6 builtins + memory (5 tools), `benchmarks/testdata/system.md`.
Measured with `go test ./benchmarks/ -run TestHandshakeContextCost -v` (bytes/4 estimate).

| Build | `eager_instructions` | Tools | tools/list | Instructions | Total |
|---|---|---|---|---|---|
| v0.9.x (before) | false | <N> | ~<S> | ~<I> | ~<T> |
| v0.9.x (before) | true | <N> | ~<S> | ~<I> | ~<T> |
```

Replace each `<…>` with the numbers from Step 4. (This is a doc table filled from measured output, not a code placeholder.)

- [ ] **Step 6: Run the whole benchmarks package**

Run: `go test ./benchmarks/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add benchmarks/measure.go benchmarks/context_cost_test.go Makefile docs/guides/benchmarks.md
git commit -m "test: measure delivered handshake instructions and record Phase D baseline

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: Remove lazy tool loading (D-1)

**Files:**
- Modify: `pkg/mcp/bridge.go` (delete `addSearchToolsTool` and the lazy branch; deprecate `BridgeConfig.LazyToolLoading`)
- Modify: `pkg/mcp/bridge_test.go` (replace `TestBridgeLazyToolLoadingSearchTools`)
- Modify: `pkg/agent/options.go`, `pkg/agent/agent.go`, `pkg/agent/agent_test.go`
- Modify: `docs/reference.md:57-59`, `docs/reference.md:464`

**Interfaces:**
- Consumes: `startBridgeWithConfig`, `extractText` (in `pkg/mcp/bridge_test.go`).
- Produces: `BridgeConfig.LazyToolLoading bool` stays, deprecated and ignored. `agent.WithLazyToolLoading(bool) Option` stays, deprecated and a no-op apart from its warning. No `search_tools` tool exists anywhere.

- [ ] **Step 1: Replace the lazy bridge test with a failing one**

In `pkg/mcp/bridge_test.go`, delete `TestBridgeLazyToolLoadingSearchTools` in full and add in its place:

```go
func TestBridgeLazyToolLoadingIgnored(t *testing.T) { // Review Focus #3
	registry := tools.NewRegistry()
	_ = registry.Register(tools.BuiltinTool("echo", "Echo back the input message",
		map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}},
		func(input map[string]any) (string, error) {
			msg, _ := input["message"].(string)
			return "echo: " + msg, nil
		}))

	var logBuf bytes.Buffer
	session, _ := startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name:            "test-agent",
		Version:         "v0.1.0",
		Registry:        registry,
		Executor:        tools.NewExecutor(30*time.Second, nil),
		Loader:          newTestLoader(t),
		Logger:          slog.New(slog.NewTextHandler(&logBuf, nil)),
		LazyToolLoading: true,
	})
	ctx := context.Background()

	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range list.Tools {
		names[tool.Name] = true
	}
	if names["search_tools"] {
		t.Error("search_tools must not be registered")
	}
	if !names["echo"] {
		t.Errorf("echo must be listed even with LazyToolLoading set; got %v", names)
	}

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: "echo", Arguments: map[string]any{"message": "hi"}})
	if err != nil || res.IsError {
		t.Fatalf("echo call: err=%v isError=%v", err, res != nil && res.IsError)
	}
	if got := extractText(res); got != "echo: hi" {
		t.Errorf("echo = %q", got)
	}
	if !strings.Contains(logBuf.String(), "LazyToolLoading is deprecated") {
		t.Errorf("expected deprecation warning, log was: %q", logBuf.String())
	}
}
```

Add `"bytes"` and `"log/slog"` to the import block of `bridge_test.go`.

- [ ] **Step 2: Write the failing agent test**

Append to `pkg/agent/agent_test.go`:

```go
func TestWithLazyToolLoading_DeprecatedWarns(t *testing.T) { // Review Focus #3
	var buf bytes.Buffer
	_, err := New(
		WithName("test"), WithVersion("1.0.0"),
		WithPromptFS(testFS, "testdata/system.md"),
		WithConfigPath(filepath.Join(t.TempDir(), "config.yaml")),
		WithLogger(slog.New(slog.NewTextHandler(&buf, nil))),
		WithLazyToolLoading(true),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !strings.Contains(buf.String(), "WithLazyToolLoading is deprecated") {
		t.Errorf("expected deprecation warning, log was: %q", buf.String())
	}
}
```

Add `"bytes"`, `"log/slog"` and `"strings"` to its imports if they are absent.

- [ ] **Step 3: Run both to verify they fail**

Run: `go test ./pkg/mcp/ -run TestBridgeLazyToolLoadingIgnored -v; go test ./pkg/agent/ -run TestWithLazyToolLoading_DeprecatedWarns -v`
Expected: the bridge test FAILs with `echo must be listed …` and `search_tools must not be registered`. The agent test FAILs with `expected deprecation warning`.

- [ ] **Step 4: Implement the bridge change**

In `pkg/mcp/bridge.go`:

Replace the `LazyToolLoading` field line in `BridgeConfig` with:

```go
	// Deprecated: lazy tool loading was removed in v0.10.0. It listed tools
	// it never registered, and MCP 2026-07-28 requires a tools/list that
	// does not change per connection. The field is ignored; setting it
	// logs a warning. It will be removed in a future release.
	LazyToolLoading bool
```

In `ServeTransport`, replace the whole `if b.cfg.LazyToolLoading { … } else { … }` block with:

```go
	if b.cfg.LazyToolLoading {
		b.logger.Warn("BridgeConfig.LazyToolLoading is deprecated and ignored; all tools are registered")
	}

	// Register each abbyfile tool as an MCP tool.
	for _, def := range b.cfg.Registry.All() {
		b.addTool(server, def)
	}
	// Register the special get_instructions tool (backward compatibility).
	b.addGetInstructionsTool(server)
```

Delete the entire `addSearchToolsTool` function and its doc comment. `strings` is still imported for `strings.TrimPrefix` in `addMemoryResources`.

- [ ] **Step 5: Implement the agent change**

In `pkg/agent/options.go`, replace the `WithLazyToolLoading` doc comment and function with:

```go
// WithLazyToolLoading is kept for source compatibility only.
//
// Deprecated: lazy tool loading was removed in v0.10.0; every tool is always
// registered. Passing true logs a warning. It will be removed in a future
// release.
func WithLazyToolLoading(enabled bool) Option {
	return func(a *Agent) { a.lazyToolLoading = enabled }
}
```

In `pkg/agent/agent.go`, change the field comment (line ~42) to `lazyToolLoading bool // deprecated: only triggers a warning`. Then, directly after the `if a.logger == nil { … }` block in `New`, add:

```go
	if a.lazyToolLoading {
		a.logger.Warn("WithLazyToolLoading is deprecated and has no effect; all tools are registered")
	}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./pkg/mcp/ ./pkg/agent/ -v -run 'TestBridgeLazyToolLoadingIgnored|TestWithLazyToolLoading_DeprecatedWarns'`
Expected: PASS.

- [ ] **Step 7: Update the reference docs**

In `docs/reference.md`, replace the `### WithLazyToolLoading(enabled bool) Option` section body (line 59) with:

```markdown
**Deprecated (v0.10.0), no effect.** Lazy loading via a `search_tools` meta-tool was removed: it advertised tools it never registered, and MCP 2026-07-28 requires a fixed `tools/list`. Clients such as Claude Code already defer MCP tool definitions through their own tool search. Passing `true` logs a warning. The option will be removed in a future release.
```

In the `BridgeConfig` block (line ~464), change the field line to:

```go
    LazyToolLoading bool            // Deprecated: ignored (logs a warning)
```

- [ ] **Step 8: Run the full test suite and check for leftovers**

Run: `go test ./... && grep -rn 'search_tools\|addSearchToolsTool' --include='*.go' --include='*.md' . | grep -v docs/superpowers`
Expected: tests PASS. The grep prints only the `docs/reference.md` sentence from Step 7.

- [ ] **Step 9: Commit**

```bash
git add pkg/mcp/bridge.go pkg/mcp/bridge_test.go pkg/agent/options.go pkg/agent/agent.go pkg/agent/agent_test.go docs/reference.md
git commit -m "feat: remove lazy tool loading and search_tools

WithLazyToolLoading and BridgeConfig.LazyToolLoading are kept as deprecated
no-ops that log a warning. Fixes D1.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: Register `get_instructions` only when the handshake points at it (D-2)

**Files:**
- Modify: `pkg/mcp/bridge.go` (`ServeTransport`, `addGetInstructionsTool`, `handshakeInstructions`, `BridgeConfig.EagerInstructions` comment)
- Create: `pkg/mcp/instructions_test.go`
- Modify: `pkg/mcp/testdata/tools_list.golden.json` (regenerated)
- Modify: `internal/integration/config_test.go`
- Modify: `benchmarks/bench_test.go` (`TestRedundancyAudit` note)
- Modify: `docs/guides/context-budget.md:175-187`, `docs/guides/mcp.md:17-22`, `docs/guides/prompts.md:44-50`

**Interfaces:**
- Consumes: `startBridgeEra(t, cfg, protocolVersion string) *gomcp.ClientSession`, `newTestLoader` (prompt text `You are a test agent for MCP bridge testing.`), `sess.InitializeResult().Instructions`.
- Produces: an unexported `const instructionsStub` in `pkg/mcp/bridge.go`. Behaviour: `EagerInstructions == false` → `get_instructions` is listed and the instructions are the role line plus the stub. `EagerInstructions == true` → `get_instructions` is absent and the instructions are the full prompt plus the model hint.

- [ ] **Step 1: Write the failing matrix test**

Create `pkg/mcp/instructions_test.go`:

```go
package mcp_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	agentmcp "github.com/teabranch/abbyfile/pkg/mcp"
	"github.com/teabranch/abbyfile/pkg/tools"
)

const fullPrompt = "You are a test agent for MCP bridge testing."

var backtickName = regexp.MustCompile("`([A-Za-z0-9_.-]+)`")

// TestInstructionsModes pins D-2 for both protocol eras: get_instructions
// exists iff the handshake carries the stub, and the stub names only tools
// that exist.
func TestInstructionsModes(t *testing.T) {
	for _, era := range []string{"", "2025-11-25"} {
		for _, eager := range []bool{false, true} {
			t.Run(fmt.Sprintf("era=%q/eager=%t", era, eager), func(t *testing.T) {
				sess := startBridgeEra(t, agentmcp.BridgeConfig{
					Name:              "test-agent",
					Version:           "v0.1.0",
					Description:       "A test agent",
					Model:             "claude-opus-4-6",
					Registry:          tools.NewRegistry(),
					Executor:          tools.NewExecutor(30*time.Second, nil),
					Loader:            newTestLoader(t),
					EagerInstructions: eager,
				}, era)

				list, err := sess.ListTools(context.Background(), nil)
				if err != nil {
					t.Fatalf("list tools: %v", err)
				}
				listed := map[string]string{}
				for _, tool := range list.Tools {
					listed[tool.Name] = tool.Description
				}
				instr := sess.InitializeResult().Instructions

				if eager {
					if _, ok := listed["get_instructions"]; ok {
						t.Error("eager mode must not register get_instructions")
					}
					if !strings.Contains(instr, fullPrompt) {
						t.Errorf("eager instructions missing full prompt: %q", instr)
					}
					if !strings.Contains(instr, "claude-opus-4-6") { // Review Focus #1
						t.Errorf("eager instructions missing model hint: %q", instr)
					}
					if strings.Contains(instr, "get_instructions") {
						t.Errorf("eager instructions must not mention get_instructions: %q", instr)
					}
					return
				}

				desc, ok := listed["get_instructions"]
				if !ok {
					t.Fatal("non-eager mode must register get_instructions")
				}
				if strings.Contains(desc, "Deprecated") {
					t.Errorf("get_instructions description still says Deprecated: %q", desc)
				}
				want := "A test agent\n\nCall the `get_instructions` tool to load your full instructions before acting."
				if instr != want {
					t.Errorf("stub = %q, want %q", instr, want)
				}
				for _, m := range backtickName.FindAllStringSubmatch(instr, -1) {
					if _, ok := listed[m[1]]; !ok {
						t.Errorf("stub names %q, which is not in tools/list", m[1])
					}
				}
			})
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./pkg/mcp/ -run TestInstructionsModes -v`
Expected: the `eager=true` subtests FAIL with `eager mode must not register get_instructions`. The `eager=false` subtests FAIL on the `Deprecated` description and on the stub text.

- [ ] **Step 3: Implement**

In `pkg/mcp/bridge.go`:

1. Replace the `EagerInstructions` field comment in `BridgeConfig` with:

```go
	// EagerInstructions, when true, sends the full custom instructions in the
	// handshake (server/discover or initialize) and does not register
	// get_instructions. When false (the default), the handshake carries a
	// short stub and get_instructions serves the full text on demand.
	EagerInstructions bool
```

2. In `ServeTransport`, replace

```go
	// Register the special get_instructions tool (backward compatibility).
	b.addGetInstructionsTool(server)
```

with

```go
	// A non-eager handshake carries only a stub pointing at get_instructions,
	// so the tool must exist. An eager handshake already carries the full
	// text, so the tool would only cost context.
	if !b.cfg.EagerInstructions {
		b.addGetInstructionsTool(server)
	}
```

3. In `addGetInstructionsTool`, change the doc comment to `// addGetInstructionsTool registers the get_instructions tool that returns
// the agent's full instructions. Only registered when EagerInstructions is false.` and the `Description` to:

```go
		Description: "Load this agent's full instructions (system prompt). Call this before acting.",
```

4. Add above `handshakeInstructions`:

```go
// instructionsStub is appended to the role line in a non-eager handshake.
// It must name only tools that are registered in that mode.
const instructionsStub = "Call the `get_instructions` tool to load your full instructions before acting."
```

Then replace the doc comment and the final `return` of `handshakeInstructions` with:

```go
// handshakeInstructions returns what the MCP handshake advertises. When
// EagerInstructions is false, it returns the role line plus instructionsStub;
// get_instructions serves the full prompt on demand.
```

```go
	return role + "\n\n" + instructionsStub
```

- [ ] **Step 4: Run the package tests; regenerate and review the golden**

Run: `go test ./pkg/mcp/ -run TestInstructionsModes -v`
Expected: PASS (4 subtests).

Run: `go test ./pkg/mcp/ -run . -update && git diff pkg/mcp/testdata/tools_list.golden.json`
Expected: the only diff is the `get_instructions` `description` string. If anything else changed, stop and investigate.

Run: `go test ./pkg/mcp/`
Expected: PASS. This includes `TestBridge_NonEagerInstructions_ReturnsStub` (the stub still contains `get_instructions`) and `TestBridgeListTools` (non-eager, 2 tools).

- [ ] **Step 5: Update the benchmark note**

In `benchmarks/bench_test.go` `TestRedundancyAudit`, replace

```go
	t.Logf("  Note: get_instructions is kept for backward compatibility; real cost is schema only")
```

with

```go
	t.Logf("  Note: get_instructions is registered only when eager_instructions is false; its cost is schema only")
```

- [ ] **Step 6: Write the failing integration test**

Append to `internal/integration/config_test.go`. This mirrors `TestServeMCPModelHint` directly above it:

```go
func TestServeMCPEagerInstructionsOverride(t *testing.T) { // Review Focus #2
	tmpHome := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "config", "set", "context_budget.eager_instructions", "true")
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("config set: %v\n%s", err, out)
	}

	mcpCmd := exec.CommandContext(ctx, binaryPath, "serve-mcp")
	mcpCmd.Env = append(os.Environ(), "HOME="+tmpHome)
	client := gomcp.NewClient(&gomcp.Implementation{Name: "eager-integration-test", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &gomcp.CommandTransport{Command: mcpCmd}, nil)
	if err != nil {
		t.Fatalf("connecting to serve-mcp: %v", err)
	}
	defer session.Close()

	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range list.Tools {
		if tool.Name == "get_instructions" {
			t.Error("get_instructions must be absent when eager_instructions is overridden to true")
		}
	}
	instr := session.InitializeResult().Instructions
	if strings.Contains(instr, "Call the `get_instructions` tool") {
		t.Errorf("eager override still sent the stub: %q", instr)
	}
	if len(instr) < 100 {
		t.Errorf("eager instructions look truncated (%d bytes): %q", len(instr), instr)
	}
}
```

Add `"strings"` to the file's imports if absent.

- [ ] **Step 7: Run the integration suite**

Run: `go test -tags integration -run 'TestServeMCP' -v ./internal/integration/`
Expected: PASS. That covers the new test plus `TestServeMCPModelHint`, which is non-eager and therefore still calls `get_instructions`. If the new test fails on the `len(instr)` check, read the integration agent's prompt under `internal/integration/testdata` and assert on a distinctive sentence from it instead of the length.

- [ ] **Step 8: Update the user docs**

`docs/guides/context-budget.md`: replace the `## Instructions Behavior (`eager_instructions`)` section body (lines 177–187) with:

````markdown
By default (`eager_instructions: false`), the MCP handshake (`server/discover`, or `initialize` for older clients) does **not** send the full system prompt as the server's `instructions`. It sends a short stub:

```
<description or agent name>

Call the `get_instructions` tool to load your full instructions before acting.
```

In this mode the agent registers a `get_instructions` tool that returns the full prompt (plus the model hint, if any). The `system` MCP prompt also returns it, but prompts are user-invoked (slash commands in Claude Code), so the model reaches the prompt through the tool.

Set `eager_instructions: true` (in frontmatter, or with `config set context_budget.eager_instructions true`) to send the full prompt in the handshake. `get_instructions` is then **not** registered, since it would only duplicate the handshake and cost context on every turn. This suits agents with short instructions.

Changing this value changes `tools/list`. Servers read config only at start-up, and clients may cache `tools/list` for up to an hour. So after `config set`, restart the runtime session (for example, restart Claude Code) so that it re-lists tools.
````

`docs/guides/mcp.md`: in line 17, replace `Additionally, a `get_instructions` tool is always registered for backward compatibility.` with `Additionally, a `get_instructions` tool is registered when `eager_instructions` is false (the default); see [Context Budget](context-budget.md#instructions-behavior-eager_instructions).`. In line 22, replace `-- returns the system prompt` with `-- returns the system prompt (non-eager agents only)`.

`docs/guides/prompts.md`: replace item 3 (line 47) with `3. **`get_instructions` MCP tool** -- returns the prompt text; registered only when `eager_instructions` is false (the default), in which case the handshake carries a stub pointing at it`. Replace line 1 of that list with `1. **MCP server instructions** -- sent in the handshake (`server/discover` or `initialize`): the full prompt when `eager_instructions` is true, otherwise a short stub`. Change `All four channels return the same text (or its override).` to `The tool, flag and prompt template return the same text (or its override).`.

- [ ] **Step 9: Run everything**

Run: `go vet ./... && go test ./... && go test -tags integration ./internal/integration/`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add pkg/mcp/bridge.go pkg/mcp/instructions_test.go pkg/mcp/testdata/tools_list.golden.json internal/integration/config_test.go benchmarks/bench_test.go docs/guides/context-budget.md docs/guides/mcp.md docs/guides/prompts.md
git commit -m "feat: register get_instructions only in non-eager mode

The non-eager handshake stub now tells the model to call get_instructions;
eager agents no longer pay for a tool that duplicates the handshake. Fixes D10.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 4: Opt-in `anthropic/maxResultSizeChars` hint via `per_tool.<name>.inline_large` (D-3)

**Background:** Claude Code reads `_meta["anthropic/maxResultSizeChars"]` from each tool **definition** in `tools/list`. It uses the value to raise that tool's inline-result threshold, independent of `MAX_MCP_OUTPUT_TOKENS` (default ~25k tokens), up to a 500000-character ceiling. It never lowers the threshold. Our default byte cap (262144) already exceeds 25k tokens, so the hint is strictly opt-in.

**Files:**
- Modify: `pkg/definition/agent.go` (`ContextBudgetDef.InlineLarge`, `validateContextBudget`)
- Modify: `pkg/definition/agent_test.go`
- Modify: `pkg/tools/shaper.go` (`ContextBudget.InlineLarge`)
- Modify: `pkg/tools/executor.go` (`MaxResultSizeCharsCeiling`, `ResultSizeHint`)
- Modify: `pkg/tools/executor_raw_test.go`
- Modify: `pkg/builder/builder.go` (`budgetData.InlineLarge`, per-tool mapping and comment), `pkg/builder/templates/main.go.tmpl`, `pkg/builder/builder_test.go`
- Modify: `pkg/mcp/bridge.go` (`addTool`), `pkg/mcp/bridge_test.go`
- Modify: `docs/guides/context-budget.md` (per-tool row, line 66, plus a new subsection)

**Interfaces:**
- Consumes: `tools.ContextBudget.effectiveFor(name)` (unexported, same package), `Executor.budget`.
- Produces:
  - `definition.ContextBudgetDef.InlineLarge bool` (yaml `inline_large`).
  - `tools.ContextBudget.InlineLarge bool`, honoured only inside `PerTool` entries.
  - `const tools.MaxResultSizeCharsCeiling = 500000`.
  - `func (e *Executor) ResultSizeHint(toolName string) (chars int, requested bool)`.
  - An unexported `const metaMaxResultSizeChars = "anthropic/maxResultSizeChars"` in `pkg/mcp`.
  - Generated `main.go` renders `InlineLarge: true` inside a per-tool entry only when it is set, and never mentions the field otherwise.

- [ ] **Step 1: Write the failing definition tests**

Append to `pkg/definition/agent_test.go`:

```go
func inlineLargeMD(budgetYAML string) string {
	return "---\nname: my-agent\n---\n\n---\ndescription: \"test\"\ntools: Read, Bash\ncontext_budget:\n" + budgetYAML + "---\n\nBody."
}

func TestParseAgentMD_InlineLarge_PerTool(t *testing.T) {
	def, err := ParseAgentMD(writeTempAgent(t, inlineLargeMD("  per_tool:\n    run_command:\n      inline_large: true\n      max_output_bytes: 300000\n")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !def.ContextBudget.PerTool["run_command"].InlineLarge {
		t.Fatalf("inline_large not parsed: %+v", def.ContextBudget.PerTool)
	}
}

func TestParseAgentMD_InlineLarge_InheritsDefaultCap(t *testing.T) {
	// No max_output_bytes anywhere → effective cap is the 262144 default: valid.
	if _, err := ParseAgentMD(writeTempAgent(t, inlineLargeMD("  per_tool:\n    run_command:\n      inline_large: true\n"))); err != nil {
		t.Fatalf("parse: %v", err)
	}
}

func TestParseAgentMD_InlineLarge_BaseLevelRejected(t *testing.T) { // Review Focus #5
	_, err := ParseAgentMD(writeTempAgent(t, inlineLargeMD("  inline_large: true\n")))
	if err == nil || !strings.Contains(err.Error(), "per_tool") {
		t.Fatalf("want error pointing at per_tool, got %v", err)
	}
}

func TestParseAgentMD_InlineLarge_CapBounds(t *testing.T) { // Review Focus #4
	cases := map[string]string{
		"per-tool above ceiling": "  per_tool:\n    run_command:\n      inline_large: true\n      max_output_bytes: 600000\n",
		"inherited unlimited":    "  max_output_bytes: 0\n  per_tool:\n    run_command:\n      inline_large: true\n",
		"inherited above":        "  max_output_bytes: 900000\n  per_tool:\n    run_command:\n      inline_large: true\n",
	}
	for name, yml := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseAgentMD(writeTempAgent(t, inlineLargeMD(yml)))
			if err == nil || !strings.Contains(err.Error(), "run_command") || !strings.Contains(err.Error(), "500000") {
				t.Fatalf("want error naming run_command and 500000, got %v", err)
			}
		})
	}
}
```

Add `"strings"` to the test file's imports if absent.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/definition/ -run InlineLarge -v`
Expected: compile FAIL, `InlineLarge undefined`.

- [ ] **Step 3: Implement definition parsing and validation**

In `pkg/definition/agent.go`, add to `ContextBudgetDef` after `EagerInstructions`:

```go
	// InlineLarge (per_tool only) asks Claude Code to keep larger results of
	// this tool inline, via _meta["anthropic/maxResultSizeChars"] set to the
	// tool's effective byte cap. Rejected at the base level.
	InlineLarge bool `yaml:"inline_large"`
```

Add constants near `validateContextBudget`:

```go
// Mirrors tools.DefaultContextBudget().MaxOutputBytes and
// tools.MaxResultSizeCharsCeiling; pkg/definition must not import pkg/tools.
const (
	defaultMaxOutputBytes     = 262144
	maxResultSizeCharsCeiling = 500000
)
```

In `validateContextBudget`, after the base `check(cb)` call and before the `for name, pt := range cb.PerTool` loop, add:

```go
	if cb.InlineLarge {
		return fmt.Errorf("context_budget: inline_large is only valid under per_tool.<tool name>")
	}
```

Inside that loop, after the `check(&ptCopy)` error return, add:

```go
		if pt.InlineLarge {
			if err := checkInlineLargeCap(name, cb, pt); err != nil {
				return err
			}
		}
```

Then add the helper below `validateContextBudget`:

```go
// checkInlineLargeCap resolves the tool's effective byte cap the way
// tools.ContextBudget.effectiveFor does (a per-tool 0 inherits the base)
// and requires 1..maxResultSizeCharsCeiling.
func checkInlineLargeCap(name string, base *ContextBudgetDef, pt ContextBudgetDef) error {
	var limit int64 = defaultMaxOutputBytes
	if base.MaxOutputBytes != nil {
		limit = *base.MaxOutputBytes
	}
	if pt.MaxOutputBytes != nil && *pt.MaxOutputBytes != 0 {
		limit = *pt.MaxOutputBytes
	}
	if limit <= 0 || limit > maxResultSizeCharsCeiling {
		return fmt.Errorf("context_budget.per_tool[%q]: inline_large needs an effective max_output_bytes between 1 and %d (got %d; 0 means unlimited)",
			name, maxResultSizeCharsCeiling, limit)
	}
	return nil
}
```

- [ ] **Step 4: Run to verify the definition tests pass**

Run: `go test ./pkg/definition/ -v -run 'InlineLarge|ContextBudget'`
Expected: PASS.

- [ ] **Step 5: Write the failing executor test**

Append to `pkg/tools/executor_raw_test.go`:

```go
func TestResultSizeHint(t *testing.T) { // Review Focus #4 (runtime side)
	if n, req := NewExecutor(0, nil).ResultSizeHint("x"); n != 0 || req {
		t.Fatalf("no budget: got (%d,%v), want (0,false)", n, req)
	}
	var nilExec *Executor
	if n, req := nilExec.ResultSizeHint("x"); n != 0 || req {
		t.Fatalf("nil executor: got (%d,%v), want (0,false)", n, req)
	}
	b := DefaultContextBudget()
	b.PerTool = map[string]ContextBudget{
		"big":     {InlineLarge: true, MaxOutputBytes: 300000},
		"inherit": {InlineLarge: true},
		"huge":    {InlineLarge: true, MaxOutputBytes: 900000},
		"plain":   {MaxOutputBytes: 300000},
	}
	e := NewExecutor(0, nil, WithContextBudget(b, nil))
	cases := []struct {
		tool    string
		want    int
		wantReq bool
	}{
		{"big", 300000, true},
		{"inherit", 262144, true},
		{"huge", MaxResultSizeCharsCeiling, true}, // clamped
		{"plain", 0, false},
		{"absent", 0, false},
	}
	for _, c := range cases {
		if n, req := e.ResultSizeHint(c.tool); n != c.want || req != c.wantReq {
			t.Errorf("%s: got (%d,%v), want (%d,%v)", c.tool, n, req, c.want, c.wantReq)
		}
	}

	unl := DefaultContextBudget()
	unl.MaxOutputBytes = 0 // e.g. a runtime `config set context_budget.max_output_bytes 0`
	unl.PerTool = map[string]ContextBudget{"t": {InlineLarge: true}}
	if n, req := NewExecutor(0, nil, WithContextBudget(unl, nil)).ResultSizeHint("t"); n != 0 || !req {
		t.Fatalf("unlimited cap: got (%d,%v), want (0,true)", n, req)
	}
}
```

- [ ] **Step 6: Run to verify failure**

Run: `go test ./pkg/tools/ -run TestResultSizeHint -v`
Expected: compile FAIL, `unknown field InlineLarge` / `e.ResultSizeHint undefined`.

- [ ] **Step 7: Implement the tools side**

In `pkg/tools/shaper.go`, add to `ContextBudget` after `PerTool`:

```go
	// InlineLarge is honoured only inside PerTool entries: it opts the tool
	// into advertising its byte cap as Claude Code's maxResultSizeChars.
	InlineLarge bool
```

`effectiveFor` needs no change, because `ResultSizeHint` reads the per-tool entry directly.

In `pkg/tools/executor.go`, after `MaxOutputBytes`, add:

```go
// MaxResultSizeCharsCeiling is the largest anthropic/maxResultSizeChars value
// Claude Code accepts.
const MaxResultSizeCharsCeiling = 500000

// ResultSizeHint returns the anthropic/maxResultSizeChars value to advertise
// for toolName. requested reports whether the tool opted in with a per-tool
// InlineLarge. chars is 0 when nothing should be sent: no opt-in, no budget,
// or an unlimited (<=0) effective cap. Caps above the ceiling are clamped.
func (e *Executor) ResultSizeHint(toolName string) (chars int, requested bool) {
	// Called at tool-registration time, so a nil Executor must not panic.
	if e == nil || e.budget == nil {
		return 0, false
	}
	pt, ok := e.budget.PerTool[toolName]
	if !ok || !pt.InlineLarge {
		return 0, false
	}
	limit := e.budget.effectiveFor(toolName).MaxOutputBytes
	if limit <= 0 {
		return 0, true
	}
	return int(min(limit, MaxResultSizeCharsCeiling)), true
}
```

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./pkg/tools/ -v -run 'TestResultSizeHint|TestMaxOutputBytes|TestEffectiveFor'`
Expected: PASS.

- [ ] **Step 9: Write the failing builder test**

Append to `pkg/builder/builder_test.go`:

```go
func TestGenerateSource_PerToolInlineLarge(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name: "p", Version: "0.0.1", Description: "d", Tools: []string{"Bash"},
		PromptBody: "body",
		ContextBudget: &definition.ContextBudgetDef{
			PerTool: map[string]definition.ContextBudgetDef{
				"run_command": {InlineLarge: true},
			},
		},
	}
	if err := GenerateSource(dir, def, "v0.9.1", ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "InlineLarge: true") {
		t.Fatalf("generated main.go lacks InlineLarge: true\n%s", src)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "main.go", src, parser.AllErrors); err != nil {
		t.Fatalf("generated main.go is not valid Go: %v\n%s", err, src)
	}
}

// Agents that don't opt in must not reference the field at all, so they
// still compile against an abbyfile module that predates it.
func TestGenerateSource_PerToolWithoutInlineLarge_OmitsField(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name: "p", Version: "0.0.1", Description: "d", Tools: []string{"Bash"},
		PromptBody: "body",
		ContextBudget: &definition.ContextBudgetDef{
			PerTool: map[string]definition.ContextBudgetDef{
				"run_command": {OnOverflow: "spill"},
			},
		},
	}
	if err := GenerateSource(dir, def, "v0.9.1", ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "InlineLarge") {
		t.Fatalf("generated main.go must not mention InlineLarge when unset\n%s", src)
	}
}
```

Add `"strings"` to the imports if absent.

- [ ] **Step 10: Run to verify failure**

Run: `go test ./pkg/builder/ -run TestGenerateSource_PerToolInlineLarge -v`
Expected: FAIL, `generated main.go lacks InlineLarge: true`.

- [ ] **Step 11: Implement the builder side**

In `pkg/builder/builder.go`, add `InlineLarge bool` to `budgetData` after `EagerInstructions`. In the per-tool mapping, set `InlineLarge: pt.InlineLarge,` in the `ptData := budgetData{…}` literal. Then rewrite the "intentionally not set" comment's first sentence to read: `SummaryLines and EagerInstructions are intentionally not set here: the per-tool template block renders only MaxOutputLines, MaxOutputBytes, OnOverflow, HeadLines, TailLines and InlineLarge.`

In `pkg/builder/templates/main.go.tmpl`, inside the per-tool entry after `TailLines: {{$v.TailLines}},`, add a **guarded** line:

```
					{{- if $v.InlineLarge}}
					InlineLarge: true,
					{{- end}}
```

(Indent it to match the `TailLines` line.) The guard is required. Generated agents compile against the published abbyfile version pinned in `go.mod.tmpl` (`require github.com/teabranch/abbyfile {{.ModuleVersion}}`). An unconditional field would break every agent with a `per_tool` block whenever that pin predates this release. With the guard, generated code for agents that don't opt in is byte-identical.

- [ ] **Step 12: Run to verify it passes**

Run: `go test ./pkg/builder/ -v`
Expected: PASS, including the existing `TestGenerateSource_BudgetProducesParseableGo`.

- [ ] **Step 13: Write the failing bridge test**

Append to `pkg/mcp/bridge_test.go`:

```go
func TestBridgeResultSizeHintMeta(t *testing.T) {
	r := tools.NewRegistry()
	noop := func(map[string]any) (string, error) { return "", nil }
	for _, name := range []string{"big", "plain"} {
		_ = r.Register(tools.BuiltinTool(name, name, map[string]any{"type": "object"}, noop))
	}
	b := tools.DefaultContextBudget()
	b.PerTool = map[string]tools.ContextBudget{"big": {InlineLarge: true, MaxOutputBytes: 300000}}

	session, _ := startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name: "test-agent", Version: "v0.1.0", Registry: r,
		Executor: tools.NewExecutor(30*time.Second, nil, tools.WithContextBudget(b, nil)),
		Loader:   newTestLoader(t),
	})
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range list.Tools {
		got, has := tool.Meta["anthropic/maxResultSizeChars"]
		switch tool.Name {
		case "big":
			if n, ok := got.(float64); !ok || n != 300000 {
				t.Errorf("big _meta = %#v, want anthropic/maxResultSizeChars=300000", tool.Meta)
			}
		default:
			if has {
				t.Errorf("%s must not carry maxResultSizeChars: %#v", tool.Name, tool.Meta)
			}
		}
	}
}
```

- [ ] **Step 14: Run to verify failure**

Run: `go test ./pkg/mcp/ -run TestBridgeResultSizeHintMeta -v`
Expected: FAIL, `big _meta = map[]…`.

- [ ] **Step 15: Implement the bridge side**

In `pkg/mcp/bridge.go`, add below the `BridgeConfig` type:

```go
// metaMaxResultSizeChars is the Claude Code tool-definition _meta key that
// raises a tool's inline-result threshold (https://code.claude.com/docs/en/mcp.md).
const metaMaxResultSizeChars = "anthropic/maxResultSizeChars"
```

In `addTool`, after the annotations block and before `// Capture def for the closure.`, add:

```go
	if chars, requested := b.cfg.Executor.ResultSizeHint(def.Name); chars > 0 {
		tool.Meta = gomcp.Meta{metaMaxResultSizeChars: chars}
	} else if requested {
		b.logger.Warn("inline_large ignored: tool's max_output_bytes is unlimited", "tool", def.Name)
	}
```

- [ ] **Step 16: Run the package, including the wire golden**

Run: `go test ./pkg/mcp/ -v -run 'TestBridgeResultSizeHintMeta|TestWire'`
Expected: PASS. The golden is unchanged, because no default tool carries `_meta`.

- [ ] **Step 17: Document it**

In `docs/guides/context-budget.md`, change the `per_tool` row (line 66) so the honoured-field list reads ``Only `max_output_lines`, `max_output_bytes`, `on_overflow`, `head_lines`, `tail_lines`, and `inline_large` are honored per-tool``. Then add this subsection after the `## Instructions Behavior` section:

````markdown
## Large inline results (`inline_large`, Claude Code)

Claude Code keeps an MCP tool result inline up to about 25k tokens (`MAX_MCP_OUTPUT_TOKENS`). Larger results are saved to a file and replaced with a pointer. For a tool whose large output you want kept inline, opt in per tool:

```yaml
context_budget:
  per_tool:
    run_command:
      inline_large: true
      max_output_bytes: 300000
```

The agent then advertises `_meta["anthropic/maxResultSizeChars"]` on that tool's definition, set to the tool's effective `max_output_bytes`. Claude Code only ever *raises* its threshold for it. The hint puts **more** into context, so it is off by default and exists purely as an escape hatch.

Rules: `inline_large` is valid only under `per_tool`. The effective cap must be between 1 and 500000; `abby build` rejects anything else. If a runtime `config set context_budget.max_output_bytes` later raises the cap, the hint is clamped to 500000. If it sets the cap to 0 (unlimited), the hint is dropped and a warning is logged.
````

- [ ] **Step 18: Run everything**

Run: `go vet ./... && go test ./... && go test -tags integration ./internal/integration/`
Expected: PASS.

- [ ] **Step 19: Commit**

```bash
git add pkg/definition pkg/tools/shaper.go pkg/tools/executor.go pkg/tools/executor_raw_test.go pkg/builder pkg/mcp/bridge.go pkg/mcp/bridge_test.go docs/guides/context-budget.md
git commit -m "feat: opt-in anthropic/maxResultSizeChars hint via per_tool inline_large

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 5: Measure after, lock the invariants, publish the numbers (D-4)

**Files:**
- Modify: `benchmarks/context_cost_test.go`
- Modify: `docs/guides/benchmarks.md` (the table from Task 1)

**Interfaces:**
- Consumes: `representativeConfig`, `TestHandshakeContextCost`, `HandshakePayload.{Tools,TotalSchemaTokens,InstructionsTokens,HandshakeTokens}` from Task 1.
- Produces: regression assertions that fail if `search_tools` returns, or if `get_instructions` presence stops tracking the eager mode.

- [ ] **Step 1: Add the Phase D invariants to the test**

In `benchmarks/context_cost_test.go`, append at the end of `TestHandshakeContextCost`:

```go
	has := func(p *benchmarks.HandshakePayload, name string) bool {
		for _, tm := range p.Tools {
			if tm.Name == name {
				return true
			}
		}
		return false
	}
	for mode, p := range results {
		if has(p, "search_tools") {
			t.Errorf("eager=%t: search_tools is listed (D-1 regression)", mode)
		}
	}
	if !has(stub, "get_instructions") {
		t.Error("non-eager agent must list get_instructions (its stub points at it)")
	}
	if has(eager, "get_instructions") {
		t.Error("eager agent must not list get_instructions (D-2 regression)")
	}
	if eager.TotalSchemaTokens >= stub.TotalSchemaTokens {
		t.Errorf("eager tools/list (%d tokens) should be smaller than non-eager (%d) by the get_instructions schema",
			eager.TotalSchemaTokens, stub.TotalSchemaTokens)
	}
```

- [ ] **Step 2: Verify the invariants would have caught the old behaviour**

First run `git log --oneline -4` and note the SHA of the Task 2 commit (`feat: remove lazy tool loading and search_tools`), the last commit before D-2. Then swap in only that version of the bridge. Step 1's edit is in `benchmarks/`, so it is untouched, and no stash is needed:

Run: `git checkout <task-2-sha> -- pkg/mcp/bridge.go && go test ./benchmarks/ -run TestHandshakeContextCost -v; git checkout HEAD -- pkg/mcp/bridge.go && git status --short`
Expected: the test FAILs with `eager agent must not list get_instructions (D-2 regression)`. After the restore, `git status --short` lists only `benchmarks/context_cost_test.go`.

- [ ] **Step 3: Run it on the current code**

Run: `go test ./benchmarks/ -run TestHandshakeContextCost -v`
Expected: PASS. Copy the two `|`-rows; they are the **after** numbers.

- [ ] **Step 4: Publish**

In `docs/guides/benchmarks.md`, add two rows to the "Per-session handshake cost" table, `v0.10.0 (after)` with `false` and `true`, filled from Step 3. Below the table, add:

```markdown
- **Lazy loading removal (D-1):** no change in these numbers. `search_tools` was never wired into `serve-mcp`, so no shipped agent used it; removing it fixes uncallable tools rather than saving context.
- **`get_instructions` (D-2):** eager agents drop one tool schema from every `tools/list`. For non-eager agents the stub and the tool description changed wording, so the size shifts slightly; state the measured delta from the table. Their stub now tells the model to call the tool.
- **`inline_large` (D-3):** opt-in, and it raises how much output stays inline. No saving is claimed.
```

- [ ] **Step 5: Run the full suite and the benchmark report**

Run: `go test ./... && make bench-report`
Expected: PASS. `bench-report` output includes `TestHandshakeContextCost`.

- [ ] **Step 6: Commit**

```bash
git add benchmarks/context_cost_test.go docs/guides/benchmarks.md
git commit -m "test: lock Phase D handshake invariants and publish before/after cost

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 6: Fix memory spill keys so `on_overflow: spill` actually spills

**Bug:** `ContextBudget.Shape` spills under key `spill/<tool>`. `memorySink.Put` stores it as `spill/<tool>-<hash>`, but `memory.validateKey` rejects any key containing `/` or `\`. So when memory is enabled (`pkg/agent/agent.go:243`), every spill fails, and the result silently degrades to head-tail with `(spill unavailable — output truncated)`. The existing `TestMemorySink_Put` hides this, because its fake `set` accepts any key.

**Files:**
- Modify: `pkg/tools/spill.go` (`memorySink.Put`)
- Modify: `pkg/tools/spill_test.go` (`TestMemorySink_Put`)
- Create: `pkg/tools/spill_memory_test.go` (package `tools_test`, which avoids the `memory` → `tools` import cycle)

**Interfaces:**
- Consumes: `memory.NewFileStoreAt(dir string, limits memory.Limits)`, `memory.NewManager`, `(*memory.Manager).Set/Get`, `tools.NewMemorySink`, `tools.DefaultContextBudget`, `ContextBudget.Shape`.
- Produces: memory spill keys of the form `spill-<tool>-<12 hex>`, containing no path separators. URIs are `memory://<agent>/spill-<tool>-<hash>`, which the existing `memory://<name>/{key}` resource template can read. The `SpillSink` interface and `tempFileSink` are unchanged.

- [ ] **Step 1: Write the failing end-to-end test against the real store**

Create `pkg/tools/spill_memory_test.go`:

```go
package tools_test

import (
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/memory"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// TestSpillToRealMemoryStore uses the real memory.Manager, whose key
// validation rejects path separators. A fake set func hid that bug.
func TestSpillToRealMemoryStore(t *testing.T) {
	store, err := memory.NewFileStoreAt(t.TempDir(), memory.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	mgr := memory.NewManager(store)
	sink := tools.NewMemorySink("my-agent", mgr.Set)

	b := tools.DefaultContextBudget()
	b.OnOverflow = tools.OverflowSpill
	b.MaxOutputLines = 10
	raw := strings.Repeat("line of output\n", 50)

	res := b.Shape("run_command", raw, sink)
	if res.Strategy != tools.OverflowSpill || res.SpillURI == "" {
		t.Fatalf("spill degraded: strategy=%q uri=%q output tail=%q",
			res.Strategy, res.SpillURI, res.Output[max(0, len(res.Output)-80):])
	}
	const prefix = "memory://my-agent/"
	if !strings.HasPrefix(res.SpillURI, prefix+"spill-run_command-") {
		t.Fatalf("SpillURI = %q", res.SpillURI)
	}
	got, err := mgr.Get(strings.TrimPrefix(res.SpillURI, prefix))
	if err != nil {
		t.Fatalf("reading spilled key back: %v", err)
	}
	if got != raw {
		t.Fatalf("spilled value mismatch: %d bytes, want %d", len(got), len(raw))
	}
}
```

In `pkg/tools/spill_test.go` `TestMemorySink_Put`, add this after the `stored[capturedKey]` check:

```go
	if strings.ContainsAny(capturedKey, "/\\") {
		t.Fatalf("memory key %q contains a path separator; memory.validateKey rejects it", capturedKey)
	}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/tools/ -run 'TestSpillToRealMemoryStore|TestMemorySink_Put' -v`
Expected: both FAIL. `TestSpillToRealMemoryStore` reports `spill degraded: strategy="head-tail"`, and `TestMemorySink_Put` reports `contains a path separator`.

- [ ] **Step 3: Implement**

In `pkg/tools/spill.go`, add `"strings"` to the imports, and replace the first line of `memorySink.Put` with:

```go
	// Memory keys are flat file names (memory.validateKey rejects path
	// separators), so flatten the shaper's "spill/<tool>" key.
	flat := strings.NewReplacer("/", "-", `\`, "-").Replace(key)
	fullKey := flat + "-" + shortHash(value)
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/tools/ -v -run 'Spill|MemorySink|TempFileSink'`
Expected: PASS. The `tempFileSink` tests are unchanged and still pass.

- [ ] **Step 5: Run everything**

Run: `go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/tools/spill.go pkg/tools/spill_test.go pkg/tools/spill_memory_test.go
git commit -m "fix: flatten memory spill keys so spill no longer always falls back

memory.validateKey rejects '/', so every spill/<tool>-<hash> write failed
and on_overflow: spill silently degraded to head-tail when memory was on.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 7: Fix stale Go-version and structured-output doc references

**Files:**
- Modify: `docs/quickstart.md:23`, `docs/faq.md:145`, `docs/development.md:12`
- Modify: `pkg/mcp/structured.go:11-15` (doc comment), `docs/guides/mcp.md:60-62`
- Leave alone: `docs/guides/memory.md:28`. `"Project uses Go 1.24"` there is example memory content, not a version requirement.

**Interfaces:** none (docs and comments only).

- [ ] **Step 1: Confirm the source of truth**

Run: `grep '^go ' go.mod`
Expected: `go 1.26.0`. If it differs, use the value it prints in Step 2.

- [ ] **Step 2: Fix the Go version lines**

- `docs/quickstart.md:23`: `- **Go 1.24+** (only needed …)` → `- **Go 1.26+** (only needed …)`. Keep the rest of the line.
- `docs/faq.md:145`: → ``Go 1.26 or later. The `go.mod` specifies `go 1.26.0`.``
- `docs/development.md:12`: `- Go 1.24+` → `- Go 1.26+`

- [ ] **Step 3: Fix the structured-output wording (null is rejected)**

`structuredResult` returns `isError` for JSON `null`. In `pkg/mcp/structured.go`, change the doc-comment sentence `The raw output must be a single JSON value (any type, per SEP-2106).` to `The raw output must be a single non-null JSON value (object, array, string, number or boolean, per SEP-2106); null is an error.`

In `docs/guides/mcp.md`, change `Their output must be one JSON value within the tool's` to `Their output must be one non-null JSON value within the tool's`.

- [ ] **Step 4: Verify no stale references remain**

Run: `grep -rn 'Go 1\.24\|go 1\.24' docs README.md index.md | grep -v docs/superpowers; go vet ./pkg/mcp/`
Expected: the grep prints only `docs/guides/memory.md:28`, and vet is clean.

- [ ] **Step 5: Commit**

```bash
git add docs/quickstart.md docs/faq.md docs/development.md docs/guides/mcp.md pkg/mcp/structured.go
git commit -m "docs: fix stale Go 1.24 references and document that null structured output is rejected

Co-Authored-By: Claude <noreply@anthropic.com>"
```
