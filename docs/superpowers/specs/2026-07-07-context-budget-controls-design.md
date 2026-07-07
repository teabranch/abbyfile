# Context-Budget Controls for Abbyfile Agents

**Date:** 2026-07-07
**Status:** Design — approved for planning
**Author:** Danny Teller

## Problem

A packaged Abbyfile agent can inject too much text into the LLM runtime's
context window. When it does, the runtime approaches auto-compact and risks
data loss. Two sources dominate:

1. **Tool output.** Whatever a tool prints to stdout flows straight into the
   context as an MCP `CallToolResult`. There is no cap today.
2. **Instructions.** The full system prompt is injected eagerly at the MCP
   handshake and stays resident for the whole session — even though the same
   text is already available on demand via the `system` prompt and the
   `get_instructions` tool.

### Correcting the mental model

The original goal framed a packaged agent as a Claude Code **sub-agent** — its
own context window that returns a *summary* to the main agent. The code does
not work that way today:

- A packaged agent is an **MCP server** running inside the **main** agent's
  context window. Per `docs/concepts.md`, `Context isolation: No`. Tool output
  flows directly into the shared window; there is no separate window and no
  summary step.
- `abby build --plugin` emits `.claude-plugin/plugin.json`, `.mcp.json`, a
  binary copy, and skills. It does **not** emit a `.claude/agents/<name>.md`
  sub-agent anywhere.

So "control the packaged agent's context window" is two distinct efforts:
adding real isolation (Layer A) and shaping what gets injected (Layer B). This
design commits to **both, layered** — the option chosen during brainstorming.

### Confirmed defect

`CommandPolicy.MaxOutputBytes` (`pkg/tools/policy.go:12,26`) is **dead config**.
It is declared and wired from consumer overrides (`pkg/agent/agent.go:140-141`)
but the executor (`pkg/tools/executor.go`) reads stdout/stderr into unbounded
`bytes.Buffer`s and never checks it. There is currently **zero enforcement** on
tool-output size. This design makes that cap live and generalizes it.

### Constraint

The binary **does not call the Claude API** (`docs/concepts.md`). No
LLM-based summarization can run inside the binary. All binary-side shaping is
mechanical/deterministic. LLM summarization is available only in Layer A, where
it is performed by the runtime that spawns the emitted sub-agent.

## Goals

- Cap how much tool output a packaged agent injects into context, with a
  guaranteed mechanical floor that needs no API access.
- Offer a lossless option (spill to memory/file, return a reference) for tools
  whose full output must survive.
- Trim eager instruction injection while keeping the prompt available on demand.
- Optionally emit a real Claude Code sub-agent so the agent can run in its own
  context window and return a bounded summary — true isolation.
- Ship safe defaults **on** so existing agents gain protection on rebuild
  without frontmatter changes.

## Non-Goals

- LLM summarization inside the binary (violates the no-API constraint).
- Changing the MCP transport or the core `abby build/publish/install` flow.
- Semantic/AST-aware truncation. Shaping is line/byte-based head-tail elision.
- Unrelated refactoring of packages not touched by this work.

## Architecture

```
                        ┌─────────────────────────────────────┐
                        │      AgentDef.ContextBudget           │  new frontmatter block
                        │      (authored defaults)              │
                        └──────────────┬──────────────────────┘
                                       │ compiled in
                     ┌─────────────────┴──────────────────┐
                     │  config.yaml override (consumer)    │  ~/.abbyfile/<name>/config.yaml
                     └─────────────────┬──────────────────┘
                                       │ effective budget
        ┌──────────────────────────────┼───────────────────────────────┐
        │                              │                                │
  LAYER A: Isolation            LAYER B: Output shaping          Instructions fix
  emit .claude/agents/<name>.md   pkg/tools/shaper.go            eager_instructions flag
  (--plugin / --subagent)         head-tail | spill | passthrough  short stub at handshake,
        │                              │                          full prompt on demand
   runtime Task tool             applied inside Executor.Run
   → own context window          before result → CallToolResult
   → returns bounded summary
```

Two independent, composable layers share one config surface:

- **Layer A (isolation)** — new `pkg/subagent` package + a generator step in
  `runBuild` that emits `.claude/agents/<name>.md`. The runtime's Task tool
  spawns it in its own context window; only a bounded summary returns. The body
  embeds a **return protocol** instructing the LLM to summarize before
  returning.
- **Layer B (output shaping)** — new `pkg/tools/shaper.go` invoked inside
  `Executor.Run`, applying the effective budget with a per-tool strategy. It
  makes `MaxOutputBytes` live and generalizes it to lines + smarter elision.
  Because it lives in the executor, it applies identically in MCP-server mode
  and inside the emitted sub-agent.
- **Instructions fix** — in `pkg/mcp/bridge.go`, gate eager prompt injection on
  the budget; default to a short stub with the full prompt available on demand.

## Data Model

### Runtime type (`pkg/tools`)

```go
type OverflowStrategy string

const (
    OverflowHeadTail    OverflowStrategy = "head-tail"    // default
    OverflowSpill       OverflowStrategy = "spill"
    OverflowPassthrough OverflowStrategy = "passthrough"
)

type ContextBudget struct {
    MaxOutputLines int              // per tool call; 0 = unlimited
    MaxOutputBytes int64            // per tool call; 0 = unlimited
    OnOverflow     OverflowStrategy
    HeadLines      int              // kept from top on head-tail
    TailLines      int              // kept from bottom on head-tail
    SummaryLines   int              // Layer A return-protocol cap
    EagerInstructions bool          // inject full prompt at handshake?
    PerTool        map[string]ContextBudget // sparse per-tool overrides
}
```

### Authoring — frontmatter block (`pkg/definition/agent.go`)

Available in both dual and single (`abbyfile:`) formats:

```yaml
context_budget:
  max_output_lines: 2000
  max_output_bytes: 262144        # 256 KB
  on_overflow: head-tail          # head-tail | spill | passthrough
  head_lines: 100
  tail_lines: 40
  summary_lines: 25
  eager_instructions: false
  per_tool:
    run_command: { on_overflow: spill }
```

`ContextBudget` is added to `AgentDef` and parsed in both `parseDualFormat`
and `parseSingleFormat`. Validation: `on_overflow` must be a known strategy;
negative numbers rejected; `head_lines + tail_lines` must be > 0 when
`on_overflow: head-tail`.

### Consumer override (`pkg/config/config.go`)

Add a nil-able field following the `MemoryLimitsOverride` pattern:

```go
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

`Config.ContextBudget *ContextBudgetOverride` is added; `IsZero` updated. The
`config get/set/reset` subcommand (`internal/cli/config.go`) gains dotted-key
support: `config set context_budget.max_output_lines 500`. Per-tool overrides
are authoring-only for now (not exposed through `config set`).

### Precedence (highest wins)

1. Per-tool override (`PerTool[toolName]`, sparse merge over the base)
2. Consumer `config.yaml` override
3. Compiled frontmatter default
4. Shipped default

### Shipped defaults (on)

`max_output_lines: 2000`, `max_output_bytes: 262144` (256 KB),
`on_overflow: head-tail`, `head_lines: 100`, `tail_lines: 40`,
`summary_lines: 25`, `eager_instructions: false`.

Existing agents gain protection on next rebuild with no changes. Setting a
limit to `0` or `on_overflow: passthrough` restores today's unbounded behavior.

### MaxOutputBytes reconciliation

`CommandPolicy.MaxOutputBytes` becomes the source for
`ContextBudget.MaxOutputBytes` when the budget does not set one explicitly,
so there is a single effective byte cap rather than two competing values. This
resolves the dead-config defect.

## Layer B — Shaper

New file `pkg/tools/shaper.go`. Core is a pure function (no I/O), unit-testable.

```go
type ShapeResult struct {
    Output   string
    Shaped   bool
    Strategy OverflowStrategy
    Original struct{ Lines int; Bytes int64 }
    SpillURI string // set only when strategy == spill
}

type SpillSink interface {
    Put(key, value string) (uri string, err error)
}

func (b ContextBudget) Shape(toolName, raw string, sink SpillSink) ShapeResult
```

### Algorithm

1. Resolve effective budget: `b.PerTool[toolName]` merged over `b`.
2. Measure `raw` (lines, bytes). Under **both** caps → `passthrough`, return
   unchanged (zero overhead — the common case).
3. Over a cap → branch on `OnOverflow`:
   - **head-tail** — keep first `HeadLines` + last `TailLines`; replace the
     middle with one marker line:
     `[… 1,860 lines / 240 KB elided — full output not returned …]`.
     Then apply the byte cap to the *assembled* result as a hard backstop, so a
     few enormous lines cannot defeat the line cap. Guaranteed floor.
   - **spill** — write `raw` via `SpillSink`; return the head-tail preview plus
     the URI: `Full output saved to memory://<agent>/<key>. Fetch it if you
     need the elided detail.` Lossless.
   - **passthrough** — return `raw` unchanged (explicit opt-out).

### SpillSink implementations

- `memorySink` — wraps `memory.Manager`; key `spill/<tool>/<hash>`; respects
  memory's own size limits + TTL; returns a `memory://<agent>/<key>` URI.
- `tempFileSink` — used when memory is disabled; writes under
  `~/.abbyfile/<name>/spill/`; returns a `file://` URI.
- If `spill` is requested but no sink is available, degrade to `head-tail` and
  note the degradation in the marker. Never silently drop the cap.

### Hook point

`Executor.Run` (`pkg/tools/executor.go`), at both return points (CLI + builtin)
and on the error path (a tool that fails with a huge stderr is shaped too,
using head-tail on the error message). The executor gains one option:

```go
func WithContextBudget(b ContextBudget, sink SpillSink) ExecutorOption
```

`serve_mcp.go` and `run_tool.go` already construct the executor with
`execOpts ...tools.ExecutorOption`, so this threads through by appending one
option — no call-site signature churn.

## Layer A — Sub-agent emission

New package `pkg/subagent` with `Generate(def, budget, cfg) error`, called from
`runBuild` (`cmd/abby/build.go`) when `--plugin` is set (and via a dedicated
`--subagent` flag). Emission is **additive and gated**: no existing output
changes unless the flag is set.

Output — `.claude/agents/<name>.md`:

```markdown
---
name: <name>
description: <def.Description>
tools: <def.Tools + the agent's MCP tool names>
model: <model hint, if set>
---

<def.PromptBody>

## Return Protocol
You run in an isolated context window. When you finish, return ONLY:
1. A ≤<summary_lines>-line summary of what you did and the outcome.
2. Concrete artifacts the caller needs (file paths, IDs, final values).
Do NOT paste raw tool output, file dumps, or logs into your final message —
they stay in your context, not the caller's. If the caller needs full detail,
reference where it lives (a path or memory:// URI) instead of inlining it.
```

The runtime's Task tool spawns this in its own context window; only the bounded
summary returns to the main agent — satisfying the original goal literally.
`summary_lines` comes from the budget. Layer B still shapes tool output *inside*
that isolated window, so the material the LLM summarizes from is already lean.

`abby validate` gains a check that the sub-agent's declared tools resolve.

## Instructions fix

In `pkg/mcp/bridge.go`, `ServeTransport` currently always sets
`ServerOptions.Instructions` to the full prompt (`bridge.go:60-68`). When
`EagerInstructions` is false (shipped default), send a short stub instead:

```
<one-paragraph role line derived from Description>
Full instructions are available via the `system` prompt or the
`get_instructions` tool.
```

The full prompt still loads on demand through the existing `system` prompt and
`get_instructions` tool (unchanged). When `EagerInstructions` is true, behavior
is exactly as today. ~15-line change guarded by one budget field.

## Error Handling

- Unknown `on_overflow` value → build-time validation error in
  `ParseAgentMD`; `config set` of an invalid value → error.
- Spill sink write failure → degrade to head-tail, annotate the marker, log at
  WARN. Never fail the tool call solely because spill could not persist.
- Shaper never returns an error; it always returns a usable `ShapeResult`.
- Sub-agent generation failure surfaces as a build error (like plugin gen).

## Testing

- **Unit** — `shaper_test.go` (table-driven: under-cap passthrough, head-tail
  math, byte backstop defeating huge lines, spill URI, sink-absent degradation,
  error-path shaping); `subagent_test.go` (golden-file the emitted markdown);
  config precedence tests (`config_test.go`); frontmatter parse tests for the
  new block in both formats (`agent_test.go`).
- **Integration** (`internal/integration`) — build an agent with a deliberately
  verbose tool; assert the MCP `CallToolResult` is capped; assert `--plugin`
  emits a valid `.claude/agents/<name>.md`; assert `eager_instructions: false`
  yields a stub at handshake and full text via `get_instructions`.
- **Benchmarks** — extend `benchmarks/` with a shaped-vs-unshaped tool-output
  comparison to quantify savings for the README table.
- Run with `-race`; maintain the project's coverage bar.

## Documentation

- New `docs/guides/context-budget.md` (frontmatter reference, strategies,
  examples, defaults).
- Update `docs/concepts.md` — the `Context isolation: No` row gains a note that
  isolation is available when emitted as a sub-agent.
- Update `README.md` comparison table with the context-budget capability and
  benchmark numbers.

## Rollout

Additive and backward compatible. Sub-agent emission is opt-in via flag.
Output shaping defaults on but is fully disableable (`passthrough` / `0`). No
change to the install/publish flow. Version bump per existing CLI convention.
```