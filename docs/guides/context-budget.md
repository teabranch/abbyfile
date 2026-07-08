---
title: Context Budget
parent: Guides
nav_order: 10
---

# Context Budget Guide

A packaged Abbyfile agent runs as an MCP server *inside* the runtime's (Claude Code / Codex / Gemini CLI) main context window by default. Anything a tool prints, and the full system prompt, flows straight into that shared window. Context-budget controls give you two independent, composable ways to keep that footprint under control:

- **Layer B — output shaping.** Cap and reshape tool output before it becomes an MCP `CallToolResult`, so a single verbose command can't blow the budget.
- **Layer A — sub-agent isolation.** Emit the agent as a real Claude Code sub-agent (`.claude/agents/<name>.md`) so it runs in its *own* context window and returns only a bounded summary to the caller.

Both layers share one config surface: the `context_budget:` frontmatter block, overridable at install/runtime via `config set`.

## The Two Layers

| | Layer B: output shaping | Layer A: sub-agent isolation |
|---|---|---|
| What it does | Shrinks individual tool outputs (head-tail elision, spill-to-URI, or passthrough) | Runs the whole agent in a separate context window via the runtime's Task tool |
| Where it applies | Always, in MCP-server mode and inside an emitted sub-agent | Only when built with `abby build --subagent` (or `--plugin`) |
| Default | On (see [Defaults](#defaults) below) | Off — opt-in |
| Enforced by | `pkg/tools/shaper.go`, hooked into `Executor.Run` | `pkg/subagent`, emitted at build time |

Output shaping is on by default and protects every agent, whether or not it's ever run as a sub-agent. Sub-agent emission is an additional, opt-in layer for callers who want true isolation (the runtime keeps the packaged agent's tool chatter out of the main conversation entirely, seeing only the final summary).

## The `context_budget:` Frontmatter Block

Declare it in either agent `.md` frontmatter format (dual-block or single `abbyfile:` block):

```yaml
---
name: my-agent
---

---
description: "A verbose build/test runner"
tools: Read, Bash
context_budget:
  max_output_lines: 2000
  max_output_bytes: 262144        # 256 KB
  on_overflow: head-tail          # head-tail | spill | passthrough
  head_lines: 100
  tail_lines: 40
  summary_lines: 25
  eager_instructions: false
  per_tool:
    run_command:
      on_overflow: spill
---

You are a build/test runner...
```

### Fields

| Field | Type | Meaning |
|---|---|---|
| `max_output_lines` | int | Per-tool-call line cap. `0` = unlimited. |
| `max_output_bytes` | int | Per-tool-call byte cap (hard backstop, applied even after head-tail assembly so a few huge lines can't defeat the line cap). `0` = unlimited. |
| `on_overflow` | string | Strategy when either cap is exceeded: `head-tail`, `spill`, or `passthrough`. |
| `head_lines` | int | Lines kept from the top of the output under `head-tail`/`spill`. |
| `tail_lines` | int | Lines kept from the bottom of the output under `head-tail`/`spill`. |
| `summary_lines` | int | Caps the return-protocol summary emitted for sub-agent output (Layer A only — see [`--subagent`](#the---subagent-flag)). |
| `eager_instructions` | bool | Whether the full system prompt is injected eagerly at the MCP handshake. `false` sends a short stub instead (see [Instructions Behavior](#instructions-behavior-eager_instructions)). |
| `per_tool` | map | Sparse per-tool overrides, keyed by MCP tool name (e.g. `run_command`, `read_file`). Any field left unset inherits from the base budget. Only `max_output_lines`, `max_output_bytes`, `on_overflow`, `head_lines`, and `tail_lines` are honored per-tool; `summary_lines` and `eager_instructions` only apply at the base-budget level. |

Validation happens at parse time (`pkg/definition/agent.go`'s `validateContextBudget`): `on_overflow` must be one of the three known strategies (or empty, which falls back to the default), and all numeric fields must be non-negative. This applies to both the base block and every `per_tool` entry. An invalid value fails `abby build` immediately, with the same rule enforced again at `config set` time (see [Consumer Overrides](#consumer-overrides)).

## Overflow Strategies

### `head-tail` (default)

Keeps the first `head_lines` and last `tail_lines`, replacing the middle with a marker:

```
[… 1,860 lines / 240 KB elided — full output not returned …]
```

The assembled preview is then hard-truncated to `max_output_bytes` as a backstop, so it's a guaranteed floor even against pathological input (e.g. one 10 MB line). Use this for tools whose most useful signal is at the start (a summary or error banner) and the end (the final result), such as build logs and test runners.

### `spill`

Same head-tail preview, but the *full* raw output is persisted via a `SpillSink` first, and the preview is appended with a pointer:

```
Full output saved to memory://my-agent/spill/run_command. Fetch it if you need the elided detail.
```

Nothing is lost — the agent (or a human) can fetch the full content later. Use this for tools where the elided detail sometimes actually matters (verbose diffs, full log dumps) and losing it would hurt more than the extra round-trip to fetch it back.

Two sink implementations are wired automatically based on whether memory is enabled for the agent:

- **Memory-backed** (`tools.NewMemorySink`) — when `WithMemory(true)` is set, spills land in the agent's own memory store and are addressable as `memory://<agent>/<key>`.
- **Temp-file** (`tools.NewTempFileSink`) — when memory is disabled, spills land under `~/.abbyfile/<name>/spill/` and are addressable as `file://` URIs.

If a spill write fails (or no sink is available), the shaper never drops the cap silently — it degrades to `head-tail` and annotates the marker with `(spill unavailable — output truncated)`.

### `passthrough`

Returns the raw output unchanged, regardless of size. This is the explicit opt-out — use it (or set the caps to `0`) to restore today's unbounded behavior for a tool you know is always small, or while debugging shaping itself.

## Defaults

Context-budget protection ships **on** by default (`tools.DefaultContextBudget()` in `pkg/tools/shaper.go`), so existing agents gain protection on their next rebuild with no frontmatter changes required:

| Field | Default |
|---|---|
| `max_output_lines` | `2000` |
| `max_output_bytes` | `262144` (256 KB) |
| `on_overflow` | `head-tail` |
| `head_lines` | `100` |
| `tail_lines` | `40` |
| `summary_lines` | `25` |
| `eager_instructions` | `false` |

A `context_budget:` block in frontmatter only needs to set the fields you want to change — anything omitted falls back to these shipped defaults at build time (`pkg/builder`'s `buildBudgetData`).

## Consumer Overrides

Like `model` and `tool_timeout`, every scalar `context_budget` field can be overridden without rebuilding, via `~/.abbyfile/<name>/config.yaml`:

```bash
./my-agent config set context_budget.max_output_lines 500
./my-agent config set context_budget.max_output_bytes 65536
./my-agent config set context_budget.on_overflow spill
./my-agent config set context_budget.head_lines 50
./my-agent config set context_budget.tail_lines 20
./my-agent config set context_budget.summary_lines 15
./my-agent config set context_budget.eager_instructions true

./my-agent config get                              # show all (compiled + overrides)
./my-agent config reset context_budget             # revert the whole block to compiled defaults
```

`config get` (with no argument) prints the effective value and its source (`compiled` or `override`) for `context_budget.max_output_lines`, `context_budget.max_output_bytes`, `context_budget.on_overflow`, and `context_budget.eager_instructions`:

```
context_budget.max_output_lines: 50 (compiled)
context_budget.max_output_bytes: 262144 (compiled)
context_budget.on_overflow: head-tail (compiled)
context_budget.eager_instructions: false (compiled)
```

`context_budget.head_lines`, `context_budget.tail_lines`, and `context_budget.summary_lines` accept `config set` writes and take effect at runtime (they're applied the same way as every other field in `applyConfigOverrides`), but are not yet included in `config get`'s output — check `~/.abbyfile/<name>/config.yaml` directly if you need to confirm an override for those three fields. `per_tool` overrides are authoring-only (frontmatter) for now; they are not exposed through `config set`.

Overrides are stored per-field, so setting one doesn't clobber the others — you can override just `on_overflow` and leave everything else at its compiled default.

## The `--subagent` Flag

`abby build --subagent` additionally emits `.claude/agents/<name>.md` next to the compiled binary:

```bash
abby build --subagent
→ build/my-agent                        # standalone binary (always)
→ build/.claude/agents/my-agent.md      # Claude Code sub-agent definition
```

The emitted file carries the agent's `name`, `description`, `tools`, and `model` hint as frontmatter, followed by the agent's prompt body, followed by a **Return Protocol** section:

```markdown
## Return Protocol
You run in an isolated context window. When you finish, return ONLY:
1. A ≤25-line summary of what you did and the outcome.
2. Concrete artifacts the caller needs (file paths, IDs, final values).
Do NOT paste raw tool output, file dumps, or logs into your final message —
they stay in your context, not the caller's. If the caller needs full detail,
reference where it lives (a path or memory:// URI) instead of inlining it.
```

The summary cap (`≤25-line` above) comes from the agent's effective `summary_lines`. When Claude Code's Task tool spawns this file, it runs in its own context window — only the bounded summary text returns to the caller. Layer B still shapes tool output *inside* that isolated window, so the material the LLM is summarizing from is already lean.

`--plugin` also emits the sub-agent file (in addition to the plugin directory); `--subagent` is for when you want the sub-agent artifact without the full plugin wrapper.

## Instructions Behavior (`eager_instructions`)

By default (`eager_instructions: false`), the MCP handshake does **not** send the full system prompt as the server's `instructions`. Instead it sends a short stub:

```
<description or agent name>

Full instructions are available via the `system` prompt or the `get_instructions` tool.
```

The full prompt is unchanged and still reachable on demand through the existing channels — the `system` MCP prompt template and the `get_instructions` tool both still return the complete text. This just avoids paying for the full prompt's tokens on every session handshake when the runtime may never need it.

Set `eager_instructions: true` (in frontmatter, or via `config set context_budget.eager_instructions true`) to restore the previous behavior and inject the full prompt eagerly at handshake time — useful for agents whose instructions are short enough that the stub indirection isn't worth it, or where you want the prompt visible without an extra tool round-trip.

## Which Strategy for Which Tool

- **Read-only, usually-small tools** (`read_file`, `memory_read`) — leave at the base `head-tail` default; they rarely trigger shaping at all.
- **Verbose, occasionally-huge tools** (`run_command`, build/test runners) — consider `per_tool` overrides to widen `head_lines`/`tail_lines` if the tool's useful signal is spread out, or switch to `spill` if losing the elided middle would be costly.
- **Tools you fully trust to stay small** — set `on_overflow: passthrough` (or `max_output_lines: 0` and `max_output_bytes: 0`) to skip shaping overhead entirely.

## See Also

- [Memory Guide](./memory.md) — the memory-backed spill sink reuses the agent's memory store.
- [Plugins Guide](./plugins.md) — `--plugin` also emits the sub-agent file described above.
- [Concepts](../concepts.md#when-to-use-what) — how context isolation compares across CLAUDE.md, Agent Skills, sub-agents, and Abbyfile.
