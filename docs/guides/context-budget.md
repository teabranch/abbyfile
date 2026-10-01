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
| Where it applies | Always, on the agent's own tools (`serve-mcp` and `run-tool`) | Only when built with `abby build --subagent` (or `--plugin`) |
| Default | On (see [Defaults](#defaults) below) | Off — opt-in |
| Enforced by | `pkg/tools/shaper.go`, hooked into `Executor.Run` | `pkg/subagent`, emitted at build time |

Output shaping is on by default and protects every agent's own tools. Sub-agent emission is an additional, opt-in layer for callers who want true isolation (the runtime keeps the sub-agent's tool chatter out of the main conversation entirely, seeing only the final summary). Inside a sub-agent, the agent's own MCP tools (custom and memory tools) still go through Layer B; the native Claude Code tools it is given do not — see the note under [`--subagent`](#the---subagent-flag).

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
| `per_tool` | map | Sparse per-tool overrides, keyed by MCP tool name (e.g. `run_command`, `read_file`). Any field left unset inherits from the base budget. Only `max_output_lines`, `max_output_bytes`, `on_overflow`, `head_lines`, `tail_lines`, and `inline_large` are honored per-tool; `summary_lines` and `eager_instructions` only apply at the base-budget level. |

Validation happens at parse time (`pkg/definition/agent.go`'s `validateContextBudget`): `on_overflow` must be one of the three known strategies (or empty, which falls back to the default), and all numeric fields must be non-negative. This applies to both the base block and every `per_tool` entry. An invalid value fails `abby build` immediately. At `config set` time (see [Consumer Overrides](#consumer-overrides)), `on_overflow` is re-validated against the known strategies, but numeric fields are only checked for parseability — a negative value written via `config set` is not rejected there, so prefer setting limits in frontmatter where the non-negativity rule is enforced.

## Overflow Strategies

### `head-tail` (default)

Keeps the first `head_lines` and last `tail_lines`, replacing the middle with a marker (the elided line count, then the size of the whole original output):

```
[… 1860 lines / 245760 bytes elided — full output not returned …]
```

The assembled preview is then hard-truncated to `max_output_bytes` as a backstop, so it's a guaranteed floor even against pathological input (e.g. one 10 MB line). Use this for tools whose most useful signal is at the start (a summary or error banner) and the end (the final result), such as build logs and test runners.

### `spill`

Same head-tail preview, but the *full* raw output is persisted via a `SpillSink` first, and the preview is appended with a pointer:

```
Full output saved to memory://my-agent/spill-run_command-3f2a9c1b7e4d. Fetch it if you need the elided detail.
```

Nothing is lost — the agent (or a human) can fetch the full content later. Use this for tools where the elided detail sometimes actually matters (verbose diffs, full log dumps) and losing it would hurt more than the extra round-trip to fetch it back.

Two sink implementations are wired automatically based on whether memory is enabled for the agent:

- **Memory-backed** (`tools.NewMemorySink`) — when `WithMemory(true)` is set, spills land in the agent's own memory store and are addressable as `memory://<agent>/<key>`.
- **Temp-file** (`tools.NewTempFileSink`) — when memory is disabled, spills land under `~/.abbyfile/<name>/spill/` and are addressable as `file://` URIs.

If a spill write fails (or no sink is available), the shaper never drops the cap silently — it degrades to `head-tail` and annotates the marker with `(spill unavailable — output truncated)`.

**Accumulation.** Every distinct overflowing output writes a new key, `spill-<tool>-<hash>` (content-addressed by a hash of the raw output), into the same store as the agent's own `memory_write`/`memory_read` tools — nothing currently evicts or rotates these keys. If the agent sets capacity limits via `agent.WithMemoryLimits(memory.Limits{...})` (see [Memory Guide](./memory.md#limits-configuration)), accumulated spill keys count toward `MaxKeys`/`MaxTotalBytes` alongside the agent's own writes, and enough spill traffic can make a later `memory_write` call fail once a limit is reached. A spill whose value is itself larger than `MaxValueBytes` fails to write and falls back to the `head-tail` degrade path described above, on that call only.

If you use `on_overflow: spill` together with memory limits, consider also setting a `TTL` (`memory.Limits{TTL: 72 * time.Hour}` at build time, or the equivalent `ttl` duration string, e.g. `"72h"`, under `memory_limits:` in `~/.abbyfile/<name>/config.yaml` at runtime — `config set` does not yet expose `memory_limits.*` fields, so a runtime override means hand-editing that file) so old entries expire. Expiry on its own only makes `Read`/`memory_read` fail for that key (`pkg/memory/store.go`'s `checkExpired`); the file stays on disk and in `Keys()`, so it still counts toward `MaxKeys`/`MaxTotalBytes`. Run `./my-agent memory gc` to delete expired keys and free that capacity. Nothing runs `gc` automatically, and there is no dedicated eviction or rotation for spill keys yet.

### `passthrough`

Returns the raw output unchanged, regardless of size. This is the explicit opt-out — use it (or set the caps to `0`) to restore unshaped output for a tool you know is always small, or while debugging shaping itself. (Subprocess output from custom CLI tools and `run_command` is still capped at 10 MB in memory before shaping; see the [Tools guide](./tools.md#sandbox).)

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

`context_budget.head_lines`, `context_budget.tail_lines`, and `context_budget.summary_lines` accept `config set` writes and are applied at runtime the same way as every other field (`applyConfigOverrides`), though `summary_lines` is only read when `abby build` emits the sub-agent file, so a runtime override of it changes nothing visible. All three are not yet included in `config get`'s output — check `~/.abbyfile/<name>/config.yaml` directly if you need to confirm an override for those three fields. `per_tool` overrides are authoring-only (frontmatter) for now; they are not exposed through `config set`.

Overrides are stored per-field, so setting one doesn't clobber the others — you can override just `on_overflow` and leave everything else at its compiled default.

## The `--subagent` Flag

`abby build --subagent` additionally emits `.claude/agents/<name>.md` next to the compiled binary:

```bash
abby build --subagent
→ build/my-agent                        # standalone binary (always)
→ build/.claude/agents/my-agent.md      # Claude Code sub-agent definition
```

The emitted file carries the agent's `name`, `description` and `tools` as frontmatter (a `model` field is supported by the generator, but `abby build` doesn't pass one yet, so it is omitted), followed by the agent's prompt body, followed by a **Return Protocol** section:

```markdown
## Return Protocol
You run in an isolated context window. When you finish, return ONLY:
1. A ≤25-line summary of what you did and the outcome.
2. Concrete artifacts the caller needs (file paths, IDs, final values).
Do NOT paste raw tool output, file dumps, or logs into your final message —
they stay in your context, not the caller's. If the caller needs full detail,
reference where it lives (a file path) instead of inlining it.
```

For an agent with `memory:` set, the last line reads "a file path, or a memory key the caller can fetch with memory_read" instead.

The summary cap (`≤25-line` above) comes from the `summary_lines` in the agent's frontmatter (25 if unset; a `config set context_budget.summary_lines` doesn't change an already-emitted file). When Claude Code's Task tool spawns this file, it runs in its own context window — only the bounded summary text returns to the caller.

The `tools:` line has two parts:

- **Native tools.** The agent's declared tools by their Claude Code names (`Read`, `Bash`, …). Inside the sub-agent these are Claude Code's own built-in tools, not the binary's MCP versions, so Layer B shaping and the abby [sandbox](./tools.md#sandbox) don't apply to them; Claude Code's own permission settings and hooks do.
- **The agent's own MCP tools.** Every tool the built binary serves beyond the built-ins (its `custom_tools` and, with `memory:` set, the `memory_*` tools), named `mcp__<agent>__<tool>`. `abby build` reads them from the binary's `--describe` output, so the list always matches what the binary serves. These calls go through the binary, so Layer B shaping applies to them.

For example, an agent declaring `tools: [Read, Bash]`, one custom tool `lint` and memory gets:

```yaml
tools: Read, Bash, mcp__linty__lint, mcp__linty__memory_delete, mcp__linty__memory_list, mcp__linty__memory_read, mcp__linty__memory_search, mcp__linty__memory_write
```

The `mcp__<agent>__` prefix matches the project `.mcp.json` entry that `abby build` writes, keyed by the agent name. If an agent declares no native tools, `tools:` is omitted entirely and the sub-agent inherits every tool in the session, MCP tools included.

`--plugin` also emits the sub-agent file (in addition to the plugin directory); `--subagent` is for when you want the sub-agent artifact without the full plugin wrapper.

## Instructions Behavior (`eager_instructions`)

By default (`eager_instructions: false`), the MCP handshake (`server/discover`, or `initialize` for older clients) does **not** send the full system prompt as the server's `instructions`. It sends a short stub:

```
<description or agent name>

Call the `get_instructions` tool to load your full instructions before acting.
```

In this mode the agent registers a `get_instructions` tool that returns the full prompt (plus the model hint, if any). The `system` MCP prompt also returns it, but prompts are user-invoked (slash commands in Claude Code), so the model reaches the prompt through the tool.

Set `eager_instructions: true` (in frontmatter, or with `config set context_budget.eager_instructions true`) to send the full prompt in the handshake. `get_instructions` is then **not** registered, since it would only duplicate the handshake and cost context on every turn. This suits agents with short instructions.

Changing this value changes `tools/list`. Servers read config only at start-up, and clients may cache `tools/list` for up to an hour. So after `config set`, restart the runtime session (for example, restart Claude Code) so that it re-lists tools.

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

Because the hint value is derived from the runtime `max_output_bytes`, a `config set context_budget.max_output_bytes` also changes `tools/list` (the advertised `anthropic/maxResultSizeChars` changes or disappears). The same caveat as [`eager_instructions`](#instructions-behavior-eager_instructions) applies: restart the runtime session (for example, restart Claude Code) after the `config set` so it re-lists tools instead of using a cached list.

## Which Strategy for Which Tool

- **Read-only, usually-small tools** (`read_file`, `memory_read`) — leave at the base `head-tail` default; they rarely trigger shaping at all.
- **Verbose, occasionally-huge tools** (`run_command`, build/test runners) — consider `per_tool` overrides to widen `head_lines`/`tail_lines` if the tool's useful signal is spread out, or switch to `spill` if losing the elided middle would be costly.
- **Tools you fully trust to stay small** — set `on_overflow: passthrough` (or `max_output_lines: 0` and `max_output_bytes: 0`) to skip shaping overhead entirely.

## See Also

- [Memory Guide](./memory.md) — the memory-backed spill sink reuses the agent's memory store.
- [Plugins Guide](./plugins.md) — `--plugin` also emits the sub-agent file described above.
- [Concepts](../concepts.md#when-to-use-what) — how context isolation compares across CLAUDE.md, Agent Skills, sub-agents, and Abbyfile.
