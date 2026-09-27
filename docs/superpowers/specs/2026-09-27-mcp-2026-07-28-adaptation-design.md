# MCP 2026-07-28 Adaptation: Protocol, Security, Efficiency, UX

**Date:** 2026-09-27
**Status:** Design — approved in brainstorming, pending written-spec review
**Author:** Danny Teller
**Target release:** v0.10.0 (umbrella spec; one implementation plan per phase)

## Problem

The MCP specification released revision **2026-07-28** (Final) on 2026-07-28.
Abbyfile pins `github.com/modelcontextprotocol/go-sdk` **v1.4.0**, which predates
it. Reading the codebase against the new spec also surfaced correctness,
security, and efficiency defects that are cheapest to fix in the same pass.

Goal: every Abbyfile agent speaks 2026-07-28 while staying reachable by
2025-11-25 clients, ships **secure by default** with explicit escape hatches,
costs less context per turn, and installs into runtimes without risk to the
user's existing configuration.

### What changed in MCP (relevant to a stdio server)

Sources: [2026-07-28 changelog](https://modelcontextprotocol.io/specification/2026-07-28/changelog),
[2025-11-25 changelog](https://modelcontextprotocol.io/specification/2025-11-25/changelog),
[deprecated registry](https://modelcontextprotocol.io/specification/2026-07-28/deprecated),
[roadmap 2026-08-22](https://blog.modelcontextprotocol.io/posts/mcp-roadmap/).

| Change | Revision | Impact on Abbyfile |
|---|---|---|
| `initialize`/`initialized` removed; per-request `_meta` carries protocol version + client info/capabilities (SEP-2575) | 2026-07-28 | Handled by SDK ≥ v1.7.0 (dual-era stdio) |
| `server/discover` mandatory; returns `instructions` | 2026-07-28 | Our handshake instructions move here via SDK; `get_instructions` tool becomes redundant |
| stdio: server MUST NOT send requests on stdout; elicitation/sampling/roots via MRTR `input_required` (SEP-2322) | 2026-07-28 | We send none today; any future confirmation flow must use MRTR |
| `ping`, `logging/setLevel`, resource subscribe/unsubscribe removed | 2026-07-28 | Not used |
| `tools/list` MUST NOT vary per connection; SHOULD be deterministic; `ttlMs`/`cacheScope` required on list results (SEP-2549) | 2026-07-28 | Lazy loading conflicts; cache hints needed |
| Roots, Sampling, Logging deprecated (SEP-2577); allowed dirs should come from server config | 2026-07-28 | Path confinement must be server-configured |
| Input validation failures are tool execution errors (`isError`) (SEP-1303) | 2025-11-25 | Already compliant; lock with test |
| Tool names `[A-Za-z0-9_.-]{1,128}` (SEP-986) | 2025-11-25 | Not validated today |
| JSON Schema 2020-12 default dialect; `structuredContent` any JSON value | both | `outputSchema` declared but `structuredContent` never returned |
| Resource-not-found error code −32002 → −32602 | 2026-07-28 | SDK-handled; update any test asserting codes |

**Not deprecated** and kept: server instructions, prompts, resources/templates,
tool annotations, `outputSchema`/`structuredContent`.

go-sdk releases since v1.4.0: v1.7.0 adds 2026-07-28 with dual-era stdio,
`server/discover`, MRTR, `ttlMs`/`cacheScope`; v1.8.0 (Sep 2026) adds
`ServerOptions.SupportedProtocolVersions`, `ServerOptions.SetCacheable`,
`StdioTransport.MaxLineLength`, async cancel notifications, and removes some
`MCPGODEBUG` flags. **The exact Go API diff was not verified** (module download
blocked by a TLS proxy during design); Phase A step 1 resolves it.

### Confirmed defects in the current code

| # | Defect | Location |
|---|---|---|
| D1 | Lazy mode returns tool names from `search_tools` but never registers those tools — they are uncallable. `LazyToolLoading` is also never wired from `serve-mcp`, so `WithLazyToolLoading` is inert. | `pkg/mcp/bridge.go:75`, `internal/cli/serve_mcp.go:24` |
| D2 | `outputSchema` advertised but results return only text; spec requires `structuredContent`. | `pkg/mcp/bridge.go:112,147` |
| D3 | Builtin handlers take no `context.Context`; `run_command` uses `context.Background()` and an uncapped model-supplied timeout; executor timeout does not apply to builtins. | `pkg/tools/registry.go:34`, `pkg/builtins/bash.go:58` |
| D4 | `run_command` safety is a 6-entry substring denylist (bypassable, e.g. `rm -r -f /`). | `pkg/tools/policy.go:18` |
| D5 | File tools accept any path on disk; `write_file` annotated `DestructiveHint:false` despite overwriting. | `pkg/builtins/read.go`, `write.go:32` |
| D6 | `CommandPolicy.MaxOutputBytes` (10 MB) is never enforced; stdout/stderr buffered unbounded before shaping. | `pkg/tools/policy.go:26`, `pkg/tools/executor.go:135` |
| D7 | Claude Code user-scope path is `~/.claude/mcp.json`; Claude Code reads user/local scope from `~/.claude.json`. | `pkg/runtimecfg/claude.go:27` |
| D8 | Config merge ignores parse errors and overwrites the file; writes are non-atomic 0644. | `pkg/runtimecfg/claude.go:47`, `codex.go:40` |
| D9 | Checksum verification is fail-open: missing SHA256SUMS, download failure, or missing entry all install silently. | `cmd/abby/install.go:340` |
| D10 | `get_instructions` always registered (deprecated in its own description) — per-turn context cost. | `pkg/mcp/bridge.go:150` |
| D11 | Tool names not validated against SEP-986. | `pkg/definition` |

Verified non-issues: `Registry.All()` sorts by name (deterministic list);
memory store already writes atomically with 0600.

## Decisions (from brainstorming)

1. **One umbrella spec, phased** A → D → B → C. A first: B and D build on
   the new SDK types.
2. **Compatibility: secure defaults + escape hatches**, shipped as v0.10.0
   with a migration note. No silent behavior preservation; every opt-out is
   explicit and warns.
3. **Dual-era protocol support** via go-sdk v1.8.0 (not modern-only, not
   hand-rolled). Codex and Gemini CLI 2026-07-28 support is unverified, so
   legacy `initialize` clients must keep working.

## Phase A — MCP 2026-07-28 conformance

**A1. SDK bump.** `go-sdk` v1.4.0 → v1.8.0 in `go.mod` and in the version
pinned into generated agents by `pkg/builder`. Step 1 is an API audit: compile,
fix `pkg/mcp/bridge.go` and tests, then diff wire output (tools/list,
resources/list, prompts/list, discover/initialize responses) before vs after
using the existing in-memory transport tests. If v1.8.0 changes `AddTool`,
`ServerOptions.Instructions`, `CallToolResult`, or `ToolAnnotations` shape
(e.g. hint fields losing `omitempty`), adapt the bridge; do not set
`MCPGODEBUG` escape hatches.

**A2. Structured output (fixes D2).** When a `tools.Definition` has
`OutputSchema`:
- Parse the tool's raw output as JSON → `CallToolResult.StructuredContent`.
- Also emit the JSON as a `TextContent` block for clients that ignore
  structured content.
- Context-budget shaping applies to the text block only; structured content is
  never truncated. If the raw output exceeds the byte cap, return `isError`
  with a message stating the size and cap (a truncated JSON value is invalid).
- Non-JSON output → `isError` with "tool declared outputSchema but produced
  non-JSON output". Schema validation of the output is not added (the client
  validates; YAGNI).

This requires the executor to expose unshaped output to the bridge. Add
`Executor.RunRaw(ctx, def, input) (string, error)` returning pre-shape output
and a public `Executor.Shape(name, s) string`; `Run` stays as
`Shape(RunRaw(...))` so existing callers are unchanged.

**A3. Tool-name validation (fixes D11).** `^[A-Za-z0-9_.-]{1,128}$` checked in
`pkg/definition` when parsing custom tools and in `tools.Registry.Register`.
Violations fail `abby build` naming the tool and the rule.

**A4. Deterministic, connection-invariant listings.** Test that two
connections receive byte-identical `tools/list`, `prompts/list`,
`resources/list` results in sorted order.

**A5. Cache hints.** Tool and prompt lists are static per binary: mark them
cacheable with a long TTL (1h) and server-wide scope via
`ServerOptions.SetCacheable` (or its v1.8.0 equivalent). Memory resources and
`resources/read` of memory keys: `ttlMs: 0`.

**A6. stdout hygiene.** All logging goes to stderr via `slog`; no MCP
`logging` capability is advertised. Integration test asserts every stdout line
is a valid JSON-RPC message.

**A7. Input-validation errors stay `isError`.** Lock current behavior with a
test for both eras.

**Out of scope:** tasks extension, elicitation, sampling, subscriptions,
icons, Streamable HTTP. Any future destructive-action confirmation must use
MRTR `input_required`, never stdout requests.

## Phase D — Efficiency

**D-1. Remove lazy tool loading (fixes D1).** Delete `search_tools` and the
lazy branch in `bridge.go`. `WithLazyToolLoading` becomes a documented no-op
marked `// Deprecated:` (kept one release to avoid breaking library callers).
Rationale: Claude Code's native tool search defers MCP tools by default, and a
tool list that changes after search conflicts with 2026-07-28.

**D-2. Remove `get_instructions` (fixes D10).** Not registered by default.
Opt-in via frontmatter `legacy_instructions_tool: true` (and config key of the
same name). The `system` prompt and the handshake/discover instructions stub
remain.

**D-3. Result-size hint.** For each tool, set
`_meta["anthropic/maxResultSizeChars"]` in `tools/list` to the tool's effective
context-budget byte cap (per-tool override, else base), capped at 500 000. Omit
when the cap is 0 (unlimited). This stops Claude Code from spilling below our
own shaping limit. Other runtimes ignore unknown `_meta`.

**D-4. Measure.** Extend `benchmarks/` to report tokens for
`tools/list` + instructions for a representative agent before and after
D-1..D-3; record the numbers in `docs/guides/benchmarks.md`.

## Phase B — Built-in tool security

New frontmatter block, parallel to `context_budget:`, overridable post-install
with `config set sandbox.<key>`:

```yaml
sandbox:
  allowed_dirs: ["."]                 # default ["."] = agent's working directory; ["/"] opts out
  bash: restricted                    # restricted (default) | unrestricted
  allow_commands: ["go test", "git status", "make *"]
  max_command_timeout: 120s           # hard cap on model-requested timeout
```

**B1. Path confinement (fixes D5).** New `pkg/sandbox` with
`Resolve(path string) (string, error)`:
1. Relative paths resolve against the working directory.
2. `filepath.Clean` then `filepath.EvalSymlinks` (for non-existent write
   targets, evaluate the deepest existing ancestor).
3. Reject unless the result is within an `allowed_dirs` entry (each entry also
   symlink-resolved at startup).
Error text names the allowed dirs so the model can self-correct. Applied in
`read_file`, `write_file`, `edit_file`, `glob` (root and every match), `grep`
(root and every file). `allowed_dirs` is server config, per the spec's Roots
deprecation guidance.

**B2. `run_command` allowlist (fixes D4).** The substring denylist is deleted.
- `bash: restricted` (default): the trimmed command must equal an
  `allow_commands` entry or start with it followed by a space. A trailing
  ` *` on an entry permits arbitrary following arguments **including** shell
  metacharacters; without it, commands containing `;`, `&`, `|`, `` ` ``,
  `$(`, `>`, `<`, or newline are rejected. Empty `allow_commands` → the tool
  refuses every call with a message explaining how to enable it.
- `bash: unrestricted`: today's behavior; `abby build` prints a warning and
  `--describe` reports it.
- Existing agents declaring `tools: Bash` without `sandbox:` get restricted
  mode with an empty allowlist — this is the documented breaking change.

**B3. Context and timeouts (fixes D3).** Add
`HandlerCtx func(ctx context.Context, input map[string]any) (string, error)`
to `tools.Definition` alongside `Handler` (non-breaking). The executor prefers
`HandlerCtx`, wraps it with the executor timeout, and passes the MCP request
context. All builtins migrate to `HandlerCtx`. `run_command` uses
`exec.CommandContext(ctx, …)` so cancellation kills the process group; the
model's `timeout` is clamped to `max_command_timeout`.

**B4. Bounded capture (fixes D6).** CLI and `run_command` stdout/stderr are
captured through a limited writer at `CommandPolicy.MaxOutputBytes`; excess is
discarded and a `[output truncated at N bytes]` marker appended before
shaping. This is a memory bound; the context budget remains the context bound.

**B5. Annotations.** `write_file`, `edit_file`: `DestructiveHint:true`.
`glob`, `grep`, `read_file`: `ReadOnlyHint:true, IdempotentHint:true`.

**B6. Memory URI keys.** Resource-template handler passes the extracted key
through `validateKey` before `Get` (defense in depth; confirm whether `Get`
already does and test either way).

**Out of scope:** OS-level sandboxing (macOS seatbelt, Linux landlock) —
recorded as a follow-up.

## Phase C — Install and runtime-config safety & UX

**C1. Register via runtime CLIs when present (fixes D7).**
- `claude` on PATH → `claude mcp add --scope project|user <name> -- <bin> serve-mcp`
- `codex` on PATH → `codex mcp add <name> -- <bin> serve-mcp`
- `gemini` on PATH → `gemini mcp add <name> <bin> serve-mcp` (scope per its CLI)
- Removal uses the matching `remove` subcommand.
- Fallback when the CLI is absent: file editing. Claude user scope targets
  `~/.claude.json` (`mcpServers` key), project scope `.mcp.json`.

The `ConfigWriter` interface gains a CLI-backed implementation per runtime;
selection happens in `runtimecfg.For`. Exact CLI flag syntax is verified
against each CLI's `--help` in the plan's first task.

**C2. Safe file edits (fixes D8).** For every file-backed writer:
- Parse failure of an existing file → return an error naming the file; never
  write.
- Write via `fsutil.WriteAtomic`, preserving the existing file mode (new
  files 0600).
- On first modification create `<file>.abbyfile.bak` (once; not overwritten).
- Codex TOML: round-trip test proving unknown tables/keys are preserved.
- `~/.claude.json`: only the `mcpServers` subtree is touched; all other keys
  round-trip unchanged (tested with a realistic fixture).

**C3. Runtime detection.** A runtime is detected if its CLI is on PATH or its
config directory exists (`~/.claude/`, `~/.codex/`, `~/.gemini/`). Never
inferred from `$HOME` existing.

**C4. Richer entries.** `ServerEntry` gains `Env map[string]string` and
`Timeout time.Duration` (derived from the agent's tool timeout + margin).
Per runtime:
- Claude: `type: "stdio"`, `env`, `timeout` (ms).
- Codex: `env`, `startup_timeout_sec`, `tool_timeout_sec`.
- Gemini: `env`, `timeout` (ms). `trust` is never set.

**C5. Mandatory checksums (fixes D9).** `abby install` fails when the release
has no checksum asset, the asset download fails, or it lacks an entry for the
binary. `--insecure-skip-checksum` bypasses with a prominent warning.
Checksum is verified **before** executing the downloaded binary for
`--describe` (today describe runs first — reorder). Signing (cosign/sigstore)
is a follow-up.

**C6. UX.**
- `abby install` / `abby build` end with a summary table: runtime, scope,
  method (CLI/file), path.
- `--dry-run` on install/build/uninstall prints planned config changes as a
  diff and writes nothing.
- `abby doctor [agent]` (extends `validate`): binary on PATH; config entry
  present per runtime and pointing at that binary; spawns `serve-mcp` and
  performs `server/discover` (falls back to `initialize`), reporting
  supported protocol versions; prints effective `sandbox` settings with
  warnings for `unrestricted` / `allowed_dirs: ["/"]`.

## Error handling

- Tool-level problems (validation, sandbox denial, non-JSON structured output,
  timeouts) → `CallToolResult{IsError:true}` with a message actionable by the
  model; never protocol errors.
- Install/config problems → non-zero exit with the file/runtime named and the
  opt-out flag (if any) mentioned.
- Full stderr/details logged via `slog` to stderr; shaped text returned.

## Testing

TDD per task; ≥80% coverage on every changed package.

- **Protocol:** integration tests spawn the real built binary over stdio with
  (a) a 2025-11-25 `initialize` client and (b) a 2026-07-28 client; assert
  tools/list equality, instructions delivery, structuredContent, `isError`
  semantics, stdout hygiene.
- **Sandbox:** table tests for `..` traversal, symlink escape (file and
  directory), absolute paths outside allowed dirs, non-existent write targets
  under a symlinked parent, glob/grep matches escaping via symlinks.
- **run_command:** allowlist match/mismatch, metacharacter rejection, `*`
  entries, empty list refusal, timeout clamp, cancellation kills child.
- **Runtime config:** golden-file round trips for `.mcp.json`,
  `~/.claude.json`, Codex TOML, Gemini JSON; parse-failure refusal; backup
  creation; CLI-backed writers tested with a fake `claude`/`codex`/`gemini`
  on a temp PATH.
- **Install:** fake GitHub server for missing / mismatched / absent-entry
  checksums, and verify-before-execute ordering.

## Migration note (v0.10.0 release notes)

1. `tools: Bash` now requires `sandbox.allow_commands` (or
   `sandbox.bash: unrestricted`).
2. File tools are confined to the working directory unless
   `sandbox.allowed_dirs` says otherwise.
3. `get_instructions` is gone unless `legacy_instructions_tool: true`.
4. `search_tools` / lazy loading removed.
5. `abby install` requires SHA256SUMS (`--insecure-skip-checksum` to bypass).
6. Claude Code user-scope entries now land in `~/.claude.json` (or via
   `claude mcp add`); stale `~/.claude/mcp.json` entries written by older
   versions are reported by `abby doctor` with a removal hint.

## Phasing

| Phase | Depends on | Plan doc |
|---|---|---|
| A — Protocol conformance | — | `plans/…-phase-a-protocol.md` |
| D — Efficiency | A | `plans/…-phase-d-efficiency.md` |
| B — Tool security | A (bridge passes request ctx after SDK bump) | `plans/…-phase-b-security.md` |
| C — Install/config | independent of B/D; after A for `doctor` discover | `plans/…-phase-c-install.md` |

## Open questions

- Should a destructive-command confirmation (MRTR `input_required`) be added
  in a later phase, for runtimes that support it?
- Do Codex CLI and Gemini CLI speak 2026-07-28 today? (Determines when
  legacy-era support can be dropped; no action in this spec.)
