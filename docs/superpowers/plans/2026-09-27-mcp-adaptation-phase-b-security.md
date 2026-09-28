# MCP 2026-07-28 Adaptation — Phase B (Built-in Tool Security) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Built-in tools are secure by default. File tools only touch paths inside `sandbox.allowed_dirs` (default: the working directory). `run_command` runs only allowlisted commands, without a shell, under a hard timeout cap. Every subprocess dies with its whole process group. Captured output is memory-bounded. Each of these has an explicit, warned opt-out.

**Architecture:**
- New leaf package `pkg/sandbox`, which imports nothing from this module:
  - `Config` holds the frontmatter/config values.
  - `Sandbox` is the resolved, immutable runtime form. It provides `Resolve` for path confinement and `CheckCommand` for the argv allowlist.
  - `NewContext` / `FromContext` carry the sandbox on the call context.
- The sandbox reaches builtins **through the call context**, because `builtins.ForNames` builds definitions in generated `main.go` before `agent.New` has read any config:
  - `tools.Definition` gains `HandlerCtx`.
  - `Executor` gains `WithSandbox`. It wraps each `HandlerCtx` call with the executor timeout and injects the sandbox and the output byte cap into the context.
  - With no sandbox on the context, `FromContext` falls back to a secure default: the working directory, restricted mode, and an empty allowlist.
- Frontmatter `sandbox:` flows through the same chain as `context_budget:`: `pkg/definition` → `pkg/builder` template (`agent.WithSandbox`) → `pkg/agent` (`config.yaml` overrides) → `sandbox.New` at `Execute` → `tools.WithSandbox`.
- Subprocesses (CLI tools and `run_command`) get a new process group, a group-kill `cmd.Cancel`, a `WaitDelay`, and a `LimitedBuffer` for stdout/stderr.
- The agent's root command now runs under a `signal.NotifyContext`, so Ctrl-C and SIGTERM cancel in-flight tools.

**Tech Stack:** Go 1.26, go-sdk v1.8.0, cobra, gopkg.in/yaml.v3, standard `testing`. No new dependencies: the argv splitter is hand-written.

**Spec:** `docs/superpowers/specs/2026-09-27-mcp-2026-07-28-adaptation-design.md`: the Phase B section (B1–B6); defects D3, D4, D5, D6; the Testing → Sandbox and run_command bullets; Migration note items 1, 2, 7.

## Global Constraints

- **Release: v0.11.0, not v0.10.0.** v0.10.0 already shipped Phases A and D, and its release notes make no Phase B claims (checked with `gh release view v0.10.0`). Phase B breaks existing behavior (pre-1.0), so it is a minor bump. The `cliVersion` bump happens at release time, after merge, and is not part of this plan. The builder pins generated agents to `"v"+cliVersion`, which must be ≥ the release that adds `pkg/sandbox`.
- No dependency changes. go-sdk stays at **v1.8.0**. Do **not** set `MCPGODEBUG`, disable `GOSUMDB`, or use `GONOSUMDB`/`GOINSECURE`/`GOFLAGS=-insecure`.
- Frontmatter block name and keys, verbatim: `sandbox:` with `allowed_dirs`, `bash`, `allow_commands`, `max_command_timeout`.
- Defaults: `allowed_dirs: ["."]`, `bash: restricted`, `allow_commands: []` (refuse every call), `max_command_timeout: 120s`.
- `bash` values: exactly `restricted` | `unrestricted`.
- Runtime override keys: `config set sandbox.allowed_dirs|sandbox.bash|sandbox.allow_commands|sandbox.max_command_timeout`, and `config reset sandbox`.
- Restricted mode metacharacters. These are a **parse error** when unquoted: `;` `&` `|` `` ` `` `$(` `>` `<` newline. Inside quotes they are literal characters, because there is no shell.
- Allowlist semantics: `go test` matches argv exactly `["go","test"]`. `make *` matches `["make", …zero or more]`. `*` is allowed only as the last word, and a bare `*` is rejected.
- Timeout rule:
  - Every tool: effective limit = executor timeout (default 30s).
  - `run_command`: `min(requested or 30s, max_command_timeout)`. The executor's outer wrapper for `run_command` uses `max(executor timeout, max_command_timeout)`.
- Output memory cap: `CommandPolicy.MaxOutputBytes` when > 0, else **10 MB** (`tools.DefaultMaxOutputBytes = 10 << 20`). Marker, verbatim: `[output truncated at N bytes]`.
- Annotations:
  - `write_file`, `edit_file`: `DestructiveHint: true`.
  - `read_file`, `glob_files`, `grep_search`: `ReadOnlyHint: true, IdempotentHint: true`.
  - Real tool names are `glob_files` / `grep_search`. The spec's `glob` / `grep` mean these.
- Tool-level failures, including sandbox denials, stay `CallToolResult{IsError:true}` with a message the model can act on. Logs go to stderr via `slog`. Stdout carries only JSON-RPC.
- TDD per task. ≥80% coverage on `pkg/sandbox`, `pkg/tools`, `pkg/builtins`, `pkg/config`.
- Commit format: `<type>: <description>` plus the trailer `Co-Authored-By: Claude <noreply@anthropic.com>`. Never use `--no-verify`. Never rewrite existing commits.
- **Hook trap:** a local hook blocks any Bash command that combines `git commit` with an `-n` flag, such as `grep -n`. Run `git commit` as its own command.
- LSP may show stale go-sdk v1.4.0 errors. Ignore them. `go build ./...` and `go test ./...` are authoritative.
- Out of scope:
  - OS-level sandboxing (seatbelt, landlock) and TOCTOU hardening such as `openat2`/`RESOLVE_BENEATH`. These are follow-ups, noted in the docs.
  - MRTR confirmation flows.
  - Phase C: `abby doctor`, runtime `cwd`/`timeout` entries.
  - Confinement of `run_command` *arguments*. An allowlisted `cat *` can read any file, and the docs say so.

## Decisions taken in this plan

- **Context carries the sandbox, not constructor arguments.** Generated `main.go` calls `builtins.ForNames` before `agent.New`, so definitions exist before any config is loaded. The executor therefore injects the sandbox into the context of each `HandlerCtx` call. `builtins.ForNames` keeps its signature, so no generated code breaks.
- **Secure fallback.** `sandbox.FromContext` with no sandbox returns `New(Default(), os.Getwd())`. If `Getwd` fails, it returns a sandbox with no directories, which denies everything. Library callers that run builtins through an executor without `WithSandbox` are therefore confined too.
- **`Handler` kept beside `HandlerCtx`.** `tools.BuiltinToolCtx` sets both. The `Handler` wrapper calls the handler with `context.Background()`, so the default sandbox applies and there is no deadline. Existing code that calls `def.Handler(...)` directly keeps working. The executor prefers `HandlerCtx`. Memory tools stay on `Handler`. Legacy `Handler`-only tools cannot be cancelled, so the executor timeout applies to `HandlerCtx` tools only. This is documented.
- **Non-existent `allowed_dirs` entries are warned about, not fatal.** Paths are resolved leniently: the deepest existing ancestor is symlink-resolved and the remainder appended. So `write_file` can create files in an allowed directory that does not exist yet. A typo shows up as a startup warning and in `--describe`. Failing at startup would surface as "server failed to connect" in the runtime, which is harder to diagnose.
- **Spill files stay readable.** With memory off, `on_overflow: spill` writes to `~/.abbyfile/<name>/spill/` and hands the model a `file://` pointer. The agent passes that directory to `sandbox.New` as a **read-only** root. `read_file`, `glob_files` and `grep_search` may read it. `write_file` and `edit_file` may not.
- **Dangling symlinks are refused.** If any path component is a symlink whose target does not exist, `Resolve` fails. This closes the gap where `write_file` on `allowed/link` (with `link` pointing to `/outside/new`) would create a file outside the sandbox.
- **glob/grep keep their output format.** Both resolve the root and walk the **resolved** root. They re-express each hit relative to the caller's original `path` argument, so relative stays relative. Symlinked entries whose target is outside the sandbox are skipped, and a trailing note says how many were skipped. Paths that matched a pattern through `..` or a symlinked directory are dropped the same way.
- **`run_command` description is rewritten at registration.** `agent.Execute` replaces the builtin's description with `builtins.RunCommandDescription(sb.Config())`, which names the mode and the allowlist. The value is static for the life of a process, so `tools/list` stays deterministic. `config set sandbox.*` prints a restart hint, like `eager_instructions` does.
- **The denylist goes away, but the `CommandPolicy` type stays.** `DefaultCommandPolicy()` drops `DeniedSubstrings` and keeps only the 10 MB cap. `run_command` no longer calls `Check`. `CommandPolicy.AllowedPrefixes/DeniedSubstrings` still apply to *custom CLI tool* arguments and to `config.yaml`'s `command_policy`, now documented as "not a security boundary".
- **`MaxOutputBytes: 0` means "use the 10 MB default", not "unlimited".** A `config.yaml` `command_policy:` block that sets only `allowed_prefixes` would otherwise silently remove the memory bound.
- **B6 is test-only.** `memory.Manager.Get` → `FileStore.Read` → `validateKey` already rejects empty keys, `.`, `..`, `/` and `\`. Task 8 locks that in with bridge-level tests. There is no duplicate validation.
- **List values on the CLI.** `config set` accepts a JSON array (`'["go test *","make *"]'`) or a comma-separated list (`"go test *,make *"`). Use the JSON form when an entry contains a comma. An empty string sets an empty list. This is allowed for `allow_commands` (disables it) and rejected for `allowed_dirs`.
- **Graceful shutdown.** `agent.Execute` runs cobra under `signal.NotifyContext(os.Interrupt, syscall.SIGTERM)`. `serve-mcp` treats `context.Canceled` as a clean exit, and `run-tool` passes `cmd.Context()` to the executor.

## Review Focus

1. **Symlinked temp or home paths**, such as macOS `/var` → `/private/var` or a symlinked `~/src`. A user whose allowed dir or file path goes through a symlink expects access, not a denial. → Task 2, Step 1 (`TestResolve_UnresolvedTempDirAllowed`).
2. **An existing agent with `tools: Bash` and no `sandbox:` block, rebuilt on v0.11.0.** They expect three things:
   - `run_command` is refused with a message saying how to enable it.
   - `abby build` prints a note.
   - Everything else keeps working.

   → Task 5, Step 1 (`TestRunCommand_EmptyAllowlistRefused`); Task 6, Step 7 (`TestSandboxNotes`).
3. **Relative paths when the process working directory differs from the sandbox working directory**, which happens in tests and in library use. They expect `read_file("note.txt")` and glob/grep to resolve against the sandbox's working directory, and glob/grep output to stay relative. → Task 4, Step 1 (`TestReadFile_RelativeUsesSandboxCwd`, `TestGlobFiles_RelativeOutputPreserved`).
4. **A spill pointer with memory off.** The model calls `read_file` on the `file://…/.abbyfile/<name>/spill/…` path it was just given, and expects it to work. Writing there must still be refused. → Task 2, Step 1 (`TestResolve_ReadOnlyRoots`); Task 7, Step 9 (`TestAgentSandboxAllowsSpillRead`).
5. **Ctrl-C, or the runtime killing the server, during a long `run_command` or CLI tool.** They expect no orphaned grandchildren. → Task 3, Step 7 (`TestExecutorCLIKillsProcessGroup`); Task 5, Step 5 (`TestRunCommand_CancelKillsGroup`); Task 7, Step 7 (signal context).

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `pkg/sandbox/argv.go`, `argv_test.go` | POSIX-ish argv splitter that rejects shell operators | Create (Task 1) |
| `pkg/sandbox/allow.go`, `allow_test.go` | `AllowRule` parse and match | Create (Task 1) |
| `pkg/sandbox/config.go`, `config_test.go` | `Config`, `Default`, `Normalize`, `Validate` | Create (Task 2) |
| `pkg/sandbox/sandbox.go`, `sandbox_test.go` | `Sandbox`, `New`, `Resolve`, `CheckCommand`, `Warnings` | Create (Task 2) |
| `pkg/sandbox/resolve.go` | `resolveLenient`, `within`, `isRoot` | Create (Task 2) |
| `pkg/sandbox/context.go`, `context_test.go` | `NewContext`, `FromContext` with secure fallback | Create (Task 2) |
| `pkg/tools/registry.go` | `HandlerCtx`, `UsesCommandTimeout`, `BuiltinToolCtx` | Modify (Task 3) |
| `pkg/tools/limit.go`, `limit_test.go` | `LimitedBuffer`, `DefaultMaxOutputBytes`, output-limit context | Create (Task 3) |
| `pkg/tools/procgroup_unix.go`, `procgroup_other.go`, `procgroup_unix_test.go` | `ConfigureProcessGroup` | Create (Task 3) |
| `pkg/tools/executor.go`, `executor_ctx_test.go` | `WithSandbox`, `runBuiltin`, bounded CLI capture, process group | Modify / Create (Task 3) |
| `pkg/tools/policy.go`, `policy_test.go` | Drop the default denylist | Modify (Task 3) |
| `pkg/tools/spill.go` | Export `SpillDir` | Modify (Task 3) |
| `pkg/builtins/read.go`, `write.go`, `edit.go`, `glob.go`, `grep.go`, `paths.go` | Confinement, `HandlerCtx`, annotations | Modify / Create (Task 4) |
| `pkg/builtins/builtins_test.go`, `sandbox_helpers_test.go`, `files_sandbox_test.go` | Tests | Modify / Create (Task 4) |
| `pkg/builtins/bash.go`, `bash_test.go`, `bash_unix_test.go` | `run_command` rewrite | Modify / Create (Task 5) |
| `pkg/definition/sandbox.go`, `sandbox_test.go`, `agent.go` | `sandbox:` frontmatter | Create / Modify (Task 6) |
| `pkg/builder/builder.go`, `templates/main.go.tmpl`, `notes.go`, `builder_test.go`, `notes_test.go` | Emit `agent.WithSandbox`, build notes | Modify / Create (Task 6) |
| `cmd/abby/build.go` | Print build notes | Modify (Task 6) |
| `pkg/config/config.go`, `loader.go`, `config_test.go` | `SandboxOverride`, `config set sandbox.*` | Modify (Task 7) |
| `pkg/agent/options.go`, `agent.go`, `agent_sandbox_test.go` | `WithSandbox`, overrides, `buildSandbox`, signal context | Modify / Create (Task 7) |
| `internal/cli/config.go`, `root.go`, `serve_mcp.go`, `run_tool.go`, `config_sandbox_test.go` | CLI display, describe, contexts | Modify / Create (Task 7) |
| `pkg/mcp/bridge_test.go` | Memory URI key tests (B6) | Modify (Task 8) |
| `internal/integration/sandbox_test.go`, `agent_test.go` | End-to-end via `abby build` | Create / Modify (Task 9) |
| `examples/basic/agents/my-agent.md`, `examples/multi-agent/agents/golang-pro.md` | Add `sandbox:` blocks | Modify (Task 9) |
| `docs/guides/tools.md`, `docs/reference.md` | Sandbox docs and migration | Modify (Task 9) |

---

### Task 1: `pkg/sandbox` — argv splitter and allowlist rules

**Files:**
- Create: `pkg/sandbox/argv.go`, `pkg/sandbox/argv_test.go`
- Create: `pkg/sandbox/allow.go`, `pkg/sandbox/allow_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func SplitArgv(s string) ([]string, error)`
  - `type AllowRule struct { Prefix []string; AnyArgs bool }`
  - `func ParseAllowEntry(entry string) (AllowRule, error)`
  - `func (r AllowRule) Matches(argv []string) bool`

- [ ] **Step 1: Write the failing argv tests**

`pkg/sandbox/argv_test.go`:

```go
package sandbox

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitArgv(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{`go test ./...`, []string{"go", "test", "./..."}},
		{`  go   test  `, []string{"go", "test"}},
		{`echo 'a b' "c d"`, []string{"echo", "a b", "c d"}},
		{`echo "a\"b"`, []string{"echo", `a"b`}},
		{`echo "a\qb"`, []string{"echo", `a\qb`}},
		{`echo a\ b`, []string{"echo", "a b"}},
		{`echo ''`, []string{"echo", ""}},
		{`echo 'x;y|z'`, []string{"echo", "x;y|z"}},
		{`echo "$(id)"`, []string{"echo", "$(id)"}},
		{`echo $HOME`, []string{"echo", "$HOME"}},
		{`echo a"b"'c'`, []string{"echo", "abc"}},
		{"echo\ta", []string{"echo", "a"}},
	}
	for _, tt := range tests {
		got, err := SplitArgv(tt.in)
		if err != nil {
			t.Errorf("SplitArgv(%q) error: %v", tt.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitArgv(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitArgv_Rejects(t *testing.T) {
	tests := map[string]string{
		`a; b`:          "operator",
		`a && b`:        "operator",
		`a & b`:         "operator",
		`a | b`:         "operator",
		"a `id`":        "operator",
		`a $(id)`:       "substitution",
		`a > f`:         "operator",
		`a < f`:         "operator",
		"a\nb":          "newline",
		"a\\\nb":        "newline",
		`echo 'open`:    "unterminated",
		`echo "open`:    "unterminated",
		`echo \`:        "backslash",
		``:              "empty",
		`   `:           "empty",
	}
	for in, wantSub := range tests {
		_, err := SplitArgv(in)
		if err == nil {
			t.Errorf("SplitArgv(%q): expected error containing %q", in, wantSub)
			continue
		}
		if !strings.Contains(err.Error(), wantSub) {
			t.Errorf("SplitArgv(%q) error = %q, want it to contain %q", in, err, wantSub)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/sandbox/ -run SplitArgv -v`
Expected: FAIL to build with `undefined: SplitArgv`.

- [ ] **Step 3: Implement `SplitArgv`**

`pkg/sandbox/argv.go`:

```go
// Package sandbox confines abbyfile built-in tools: file paths must stay
// inside allowed directories, and run_command runs only allowlisted argv
// vectors without a shell. It imports nothing from this module so every
// other package can depend on it.
package sandbox

import (
	"fmt"
	"strings"
)

// shellOperators are rejected when they appear unquoted. There is no shell
// in restricted mode, so they would otherwise be passed to the program as
// literal arguments — rejecting them makes the model's mistake visible.
const shellOperators = ";&|`<>"

// SplitArgv splits a command line into argv using POSIX shell quoting rules:
// single quotes are literal, double quotes honor \ before $ ` " \, and a
// backslash outside quotes escapes the next character. Unquoted shell
// operators, "$(", and any newline are a parse error. Nothing is expanded.
func SplitArgv(s string) ([]string, error) {
	const (
		none = iota
		single
		double
	)
	var (
		argv   []string
		cur    strings.Builder
		inWord bool
		quote  = none
	)
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch quote {
		case single:
			if c == '\'' {
				quote = none
			} else {
				cur.WriteRune(c)
			}
			continue
		case double:
			switch {
			case c == '"':
				quote = none
			case c == '\\' && i+1 < len(rs) && strings.ContainsRune("$`\"\\", rs[i+1]):
				i++
				cur.WriteRune(rs[i])
			default:
				cur.WriteRune(c)
			}
			continue
		}
		switch {
		case c == ' ' || c == '\t':
			if inWord {
				argv = append(argv, cur.String())
				cur.Reset()
				inWord = false
			}
		case c == '\n':
			return nil, fmt.Errorf("newline is not allowed (commands cannot be chained)")
		case c == '\'':
			quote, inWord = single, true
		case c == '"':
			quote, inWord = double, true
		case c == '\\':
			if i+1 >= len(rs) {
				return nil, fmt.Errorf("trailing backslash")
			}
			if rs[i+1] == '\n' {
				return nil, fmt.Errorf("newline is not allowed (commands cannot be chained)")
			}
			i++
			cur.WriteRune(rs[i])
			inWord = true
		case c == '$' && i+1 < len(rs) && rs[i+1] == '(':
			return nil, fmt.Errorf("command substitution $( is not allowed")
		case strings.ContainsRune(shellOperators, c):
			return nil, fmt.Errorf("shell operator %q is not allowed", string(c))
		default:
			cur.WriteRune(c)
			inWord = true
		}
	}
	if quote != none {
		return nil, fmt.Errorf("unterminated quote")
	}
	if inWord {
		argv = append(argv, cur.String())
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("command is empty")
	}
	return argv, nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./pkg/sandbox/ -run SplitArgv -v`
Expected: PASS.

- [ ] **Step 5: Write the failing allow-rule tests**

`pkg/sandbox/allow_test.go`:

```go
package sandbox

import (
	"strings"
	"testing"
)

func TestAllowRuleMatches(t *testing.T) {
	tests := []struct {
		entry string
		argv  []string
		want  bool
	}{
		{"go test", []string{"go", "test"}, true},
		{"go test", []string{"go", "test", "./..."}, false},
		{"go test", []string{"go"}, false},
		{"go test *", []string{"go", "test", "./...", "-run", "X"}, true},
		{"go test *", []string{"go", "test"}, true},
		{"go test *", []string{"go", "vet", "./..."}, false},
		{"make *", []string{"make"}, true},
		{"make *", []string{"makes"}, false},
		{"git status", []string{"/usr/bin/git", "status"}, false},
		{"'my tool' run", []string{"my tool", "run"}, true},
	}
	for _, tt := range tests {
		r, err := ParseAllowEntry(tt.entry)
		if err != nil {
			t.Fatalf("ParseAllowEntry(%q): %v", tt.entry, err)
		}
		if got := r.Matches(tt.argv); got != tt.want {
			t.Errorf("%q.Matches(%q) = %v, want %v", tt.entry, tt.argv, got, tt.want)
		}
	}
}

func TestParseAllowEntry_Rejects(t *testing.T) {
	tests := map[string]string{
		"":             "empty",
		"*":            "bare *",
		"go * test":    "last word",
		"go test; rm":  "operator",
		"go test | x":  "operator",
	}
	for entry, wantSub := range tests {
		_, err := ParseAllowEntry(entry)
		if err == nil || !strings.Contains(err.Error(), wantSub) {
			t.Errorf("ParseAllowEntry(%q) error = %v, want it to contain %q", entry, err, wantSub)
		}
	}
}
```

- [ ] **Step 6: Run to verify failure**

Run: `go test ./pkg/sandbox/ -run AllowRule\|ParseAllowEntry -v`
Expected: FAIL with `undefined: ParseAllowEntry`.

- [ ] **Step 7: Implement `AllowRule`**

`pkg/sandbox/allow.go`:

```go
package sandbox

import "fmt"

// AllowRule is one parsed sandbox.allow_commands entry. Prefix must match
// argv word-for-word; AnyArgs (a trailing "*") permits zero or more further
// arguments. There is no shell, so "*" never means "any shell text".
type AllowRule struct {
	Prefix  []string
	AnyArgs bool
}

// ParseAllowEntry parses an allow_commands entry with the same quoting rules
// as SplitArgv. "*" is only valid as the last word, and a bare "*" is
// rejected: allowing every program is what sandbox.bash: unrestricted is for.
func ParseAllowEntry(entry string) (AllowRule, error) {
	argv, err := SplitArgv(entry)
	if err != nil {
		return AllowRule{}, fmt.Errorf("allow_commands entry %q: %w", entry, err)
	}
	r := AllowRule{Prefix: argv}
	if argv[len(argv)-1] == "*" {
		r.AnyArgs = true
		r.Prefix = argv[:len(argv)-1]
	}
	if len(r.Prefix) == 0 {
		return AllowRule{}, fmt.Errorf("allow_commands entry %q: a bare * would allow every program; name the program, or use sandbox.bash: unrestricted", entry)
	}
	for _, w := range r.Prefix {
		if w == "*" {
			return AllowRule{}, fmt.Errorf("allow_commands entry %q: * is only allowed as the last word", entry)
		}
	}
	return r, nil
}

// Matches reports whether argv is permitted by the rule.
func (r AllowRule) Matches(argv []string) bool {
	if len(argv) < len(r.Prefix) {
		return false
	}
	if !r.AnyArgs && len(argv) != len(r.Prefix) {
		return false
	}
	for i, w := range r.Prefix {
		if argv[i] != w {
			return false
		}
	}
	return true
}
```

- [ ] **Step 8: Run the package tests**

Run: `go test ./pkg/sandbox/ -v`
Expected: PASS. An empty entry fails in `SplitArgv` with "command is empty", which contains "empty".

- [ ] **Step 9: Commit**

```bash
git add pkg/sandbox/argv.go pkg/sandbox/argv_test.go pkg/sandbox/allow.go pkg/sandbox/allow_test.go
```
```bash
git commit -m "feat: add sandbox argv splitter and allowlist rules

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: `pkg/sandbox` — config, path confinement, command check, context

**Files:**
- Create: `pkg/sandbox/config.go`, `pkg/sandbox/config_test.go`
- Create: `pkg/sandbox/resolve.go`
- Create: `pkg/sandbox/sandbox.go`, `pkg/sandbox/sandbox_test.go`
- Create: `pkg/sandbox/context.go`, `pkg/sandbox/context_test.go`

**Interfaces:**
- Consumes: `SplitArgv`, `ParseAllowEntry`, `AllowRule.Matches` (Task 1).
- Produces:
  - `type BashMode string`; `const BashRestricted BashMode = "restricted"`; `const BashUnrestricted BashMode = "unrestricted"`
  - `const DefaultMaxCommandTimeout = 120 * time.Second`
  - `type Config struct { AllowedDirs []string; Bash BashMode; AllowCommands []string; MaxCommandTimeout time.Duration }`
  - `func Default() Config`; `func (c Config) Normalize() Config`; `func (c Config) Validate() error`
  - `type Access int`; `const Read Access = 0`; `const Write Access = 1`
  - `type Sandbox struct{…}` (unexported fields)
  - `func New(cfg Config, cwd string, readOnlyDirs ...string) (*Sandbox, error)`
  - `func (s *Sandbox) Resolve(path string, access Access) (string, error)`
  - `func (s *Sandbox) CheckCommand(command string) ([]string, error)`
  - `func (s *Sandbox) Config() Config`, `AllowedDirs() []string`, `Cwd() string`, `Warnings() []string`
  - `func NewContext(ctx context.Context, s *Sandbox) context.Context`; `func FromContext(ctx context.Context) *Sandbox`

- [ ] **Step 1: Write the failing config and sandbox tests**

`pkg/sandbox/config_test.go`:

```go
package sandbox

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultAndNormalize(t *testing.T) {
	d := Default()
	if len(d.AllowedDirs) != 1 || d.AllowedDirs[0] != "." {
		t.Errorf("Default().AllowedDirs = %q, want [.]", d.AllowedDirs)
	}
	if d.Bash != BashRestricted || d.MaxCommandTimeout != 120*time.Second || len(d.AllowCommands) != 0 {
		t.Errorf("Default() = %+v", d)
	}
	n := Config{}.Normalize()
	if len(n.AllowedDirs) != 1 || n.Bash != BashRestricted || n.MaxCommandTimeout != DefaultMaxCommandTimeout {
		t.Errorf("zero Config normalized = %+v", n)
	}
	kept := Config{AllowedDirs: []string{}}.Normalize()
	if kept.AllowedDirs == nil || len(kept.AllowedDirs) != 0 {
		t.Errorf("explicit empty AllowedDirs must survive Normalize, got %#v", kept.AllowedDirs)
	}
}

func TestValidate(t *testing.T) {
	bad := map[string]Config{
		"empty allowed_dirs": {AllowedDirs: []string{}},
		"blank dir":          {AllowedDirs: []string{"  "}},
		"bad bash":           {Bash: "yolo"},
		"negative timeout":   {MaxCommandTimeout: -time.Second},
		"bad entry":          {AllowCommands: []string{"go test; rm -rf /"}},
	}
	for name, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: Validate() = nil, want error", name)
		} else if !strings.Contains(err.Error(), "sandbox") {
			t.Errorf("%s: error %q should name the sandbox key", name, err)
		}
	}
	if err := Default().Validate(); err != nil {
		t.Errorf("Default().Validate() = %v", err)
	}
}
```

`pkg/sandbox/sandbox_test.go`:

```go
package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realTemp returns a symlink-free temp dir (macOS /var -> /private/var).
func realTemp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustNew(t *testing.T, cfg Config, cwd string, ro ...string) *Sandbox {
	t.Helper()
	s, err := New(cfg, cwd, ro...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve(t *testing.T) {
	root := realTemp(t)
	outside := realTemp(t)
	write(t, filepath.Join(root, "in.txt"), "in")
	write(t, filepath.Join(outside, "secret.txt"), "secret")
	must(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link-out")))
	must(t, os.Symlink(outside, filepath.Join(root, "dir-out")))
	must(t, os.Symlink(filepath.Join(outside, "new.txt"), filepath.Join(root, "dangling")))
	must(t, os.Symlink(filepath.Join(root, "in.txt"), filepath.Join(root, "link-in")))
	evil := root + "-evil"
	must(t, os.Mkdir(evil, 0o755))
	relOut, _ := filepath.Rel(root, filepath.Join(outside, "secret.txt"))

	cfg := Default()
	s := mustNew(t, cfg, root)

	tests := []struct {
		name   string
		path   string
		access Access
		want   string // "" = expect denial
	}{
		{"relative inside", "in.txt", Read, filepath.Join(root, "in.txt")},
		{"absolute inside", filepath.Join(root, "in.txt"), Read, filepath.Join(root, "in.txt")},
		{"dot-dot traversal", relOut, Read, ""},
		{"absolute outside", filepath.Join(outside, "secret.txt"), Read, ""},
		{"file symlink escape", "link-out", Read, ""},
		{"dir symlink escape", "dir-out/secret.txt", Read, ""},
		{"symlink inside", "link-in", Read, filepath.Join(root, "in.txt")},
		{"new file deep", "new/deep/f.txt", Write, filepath.Join(root, "new", "deep", "f.txt")},
		{"new file under symlinked parent", "dir-out/new.txt", Write, ""},
		{"dangling symlink", "dangling", Write, ""},
		{"sibling prefix", filepath.Join(evil, "x"), Read, ""},
		{"root itself", root, Read, root},
		{"cleaned inside", "new/../in.txt", Read, filepath.Join(root, "in.txt")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Resolve(tt.path, tt.access)
			if tt.want == "" {
				if err == nil {
					t.Fatalf("Resolve(%q) = %q, want denial", tt.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tt.path, err)
			}
			if got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestResolve_DenialNamesAllowedDirs(t *testing.T) {
	root := realTemp(t)
	s := mustNew(t, Default(), root)
	_, err := s.Resolve("/etc/passwd", Read)
	if err == nil || !strings.Contains(err.Error(), root) || !strings.Contains(err.Error(), "allowed_dirs") {
		t.Fatalf("error %v should name %s and sandbox.allowed_dirs", err, root)
	}
}

func TestResolve_EmptyPath(t *testing.T) {
	s := mustNew(t, Default(), realTemp(t))
	if _, err := s.Resolve("", Read); err == nil {
		t.Fatal("empty path must be an error")
	}
}

// Review Focus #1.
func TestResolve_UnresolvedTempDirAllowed(t *testing.T) {
	raw := t.TempDir() // on macOS this is /var/..., a symlink to /private/var/...
	write(t, filepath.Join(raw, "a.txt"), "a")
	s := mustNew(t, Config{AllowedDirs: []string{raw}}, raw)
	if _, err := s.Resolve(filepath.Join(raw, "a.txt"), Read); err != nil {
		t.Fatalf("path given through the unresolved temp dir must be allowed: %v", err)
	}
}

// Review Focus #4.
func TestResolve_ReadOnlyRoots(t *testing.T) {
	root := realTemp(t)
	spill := filepath.Join(realTemp(t), "spill") // does not exist yet
	s := mustNew(t, Default(), root, spill)
	write(t, filepath.Join(spill, "out.txt"), "big")
	if _, err := s.Resolve(filepath.Join(spill, "out.txt"), Read); err != nil {
		t.Fatalf("read from read-only root: %v", err)
	}
	if _, err := s.Resolve(filepath.Join(spill, "out.txt"), Write); err == nil {
		t.Fatal("write to read-only root must be denied")
	}
}

func TestResolve_RootAllowsEverything(t *testing.T) {
	root := realTemp(t)
	s := mustNew(t, Config{AllowedDirs: []string{"/"}}, root)
	if _, err := s.Resolve("/etc/hosts", Read); err != nil {
		t.Fatalf("allowed_dirs [/] must allow any path: %v", err)
	}
}

func TestNewWarnings(t *testing.T) {
	root := realTemp(t)
	t.Setenv("HOME", root)
	s := mustNew(t, Config{
		AllowedDirs: []string{"/", root, "missing-dir"},
		Bash:        BashUnrestricted,
	}, root)
	joined := strings.Join(s.Warnings(), "\n")
	for _, want := range []string{"resolves to /", "home directory", "does not exist", "unrestricted"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings missing %q:\n%s", want, joined)
		}
	}
	if w := mustNew(t, Default(), realTemp(t)).Warnings(); len(w) != 0 { // not $HOME
		t.Errorf("default sandbox in a normal dir should not warn: %q", w)
	}
}

func TestNew_RejectsInvalid(t *testing.T) {
	if _, err := New(Config{Bash: "nope"}, realTemp(t)); err == nil {
		t.Fatal("invalid config must fail")
	}
	if _, err := New(Default(), "relative/cwd"); err == nil {
		t.Fatal("relative cwd must fail")
	}
}

func TestCheckCommand(t *testing.T) {
	root := realTemp(t)
	empty := mustNew(t, Default(), root)
	if _, err := empty.CheckCommand("go test ./..."); err == nil || !strings.Contains(err.Error(), "allow_commands is empty") {
		t.Fatalf("empty allowlist error = %v", err)
	}

	s := mustNew(t, Config{AllowCommands: []string{"go test *", "git status"}}, root)
	argv, err := s.CheckCommand(`go test ./... -run 'Test X'`)
	if err != nil {
		t.Fatalf("allowed command refused: %v", err)
	}
	if len(argv) != 5 || argv[4] != "Test X" {
		t.Errorf("argv = %q", argv)
	}
	if _, err := s.CheckCommand("go vet ./..."); err == nil || !strings.Contains(err.Error(), "go test *") {
		t.Fatalf("mismatch error should list the allowlist, got %v", err)
	}
	if _, err := s.CheckCommand("go test ./... | tee out"); err == nil || !strings.Contains(err.Error(), "no shell") {
		t.Fatalf("metacharacter error should explain there is no shell, got %v", err)
	}
}

func TestAccessors(t *testing.T) {
	root := realTemp(t)
	s := mustNew(t, Config{AllowCommands: []string{"ls"}}, root)
	if s.Cwd() != root || len(s.AllowedDirs()) != 1 || s.AllowedDirs()[0] != root {
		t.Errorf("Cwd=%q AllowedDirs=%q", s.Cwd(), s.AllowedDirs())
	}
	if c := s.Config(); c.Bash != BashRestricted || c.AllowCommands[0] != "ls" {
		t.Errorf("Config() = %+v", c)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
```

`pkg/sandbox/context_test.go`:

```go
package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestContextRoundTrip(t *testing.T) {
	s := mustNew(t, Config{AllowCommands: []string{"ls"}}, realTemp(t))
	if got := FromContext(NewContext(context.Background(), s)); got != s {
		t.Fatal("FromContext did not return the stored sandbox")
	}
}

func TestFromContextFallbackIsSecureDefault(t *testing.T) {
	s := FromContext(context.Background())
	if s == nil {
		t.Fatal("fallback must not be nil")
	}
	c := s.Config()
	if c.Bash != BashRestricted || len(c.AllowCommands) != 0 {
		t.Errorf("fallback config = %+v, want restricted with empty allowlist", c)
	}
	wd, _ := os.Getwd()
	wd, _ = filepath.EvalSymlinks(wd)
	if dirs := s.AllowedDirs(); len(dirs) != 1 || dirs[0] != wd {
		t.Errorf("fallback AllowedDirs = %q, want [%s]", dirs, wd)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/sandbox/ -v`
Expected: FAIL to build with `undefined: Default`, `New`, `NewContext`, and so on.

- [ ] **Step 3: Implement `config.go`**

`pkg/sandbox/config.go`:

```go
package sandbox

import (
	"fmt"
	"strings"
	"time"
)

// BashMode selects how run_command executes.
type BashMode string

const (
	// BashRestricted runs allowlisted argv directly, with no shell.
	BashRestricted BashMode = "restricted"
	// BashUnrestricted runs the command string through sh -c.
	BashUnrestricted BashMode = "unrestricted"
)

// DefaultMaxCommandTimeout caps the timeout the model may request for run_command.
const DefaultMaxCommandTimeout = 120 * time.Second

// Config is the sandbox: frontmatter block and its config.yaml overrides.
type Config struct {
	AllowedDirs       []string // relative entries resolve against the working directory; ["/"] opts out
	Bash              BashMode
	AllowCommands     []string
	MaxCommandTimeout time.Duration
}

// Default returns the secure defaults: the working directory only,
// restricted mode, and no allowed commands.
func Default() Config {
	return Config{
		AllowedDirs:       []string{"."},
		Bash:              BashRestricted,
		MaxCommandTimeout: DefaultMaxCommandTimeout,
	}
}

// Normalize fills zero-valued fields with defaults and returns a copy.
// A nil AllowedDirs means ["."]; an explicit empty slice is kept so that
// Validate can reject it.
func (c Config) Normalize() Config {
	out := Config{Bash: c.Bash, MaxCommandTimeout: c.MaxCommandTimeout}
	if c.AllowedDirs == nil {
		out.AllowedDirs = []string{"."}
	} else {
		out.AllowedDirs = append(make([]string, 0, len(c.AllowedDirs)), c.AllowedDirs...)
	}
	out.AllowCommands = append(make([]string, 0, len(c.AllowCommands)), c.AllowCommands...)
	if out.Bash == "" {
		out.Bash = BashRestricted
	}
	if out.MaxCommandTimeout == 0 {
		out.MaxCommandTimeout = DefaultMaxCommandTimeout
	}
	return out
}

// Validate reports the first illegal value, naming the sandbox key.
func (c Config) Validate() error {
	n := c.Normalize()
	if len(n.AllowedDirs) == 0 {
		return fmt.Errorf(`sandbox.allowed_dirs must list at least one directory (use ["."] for the working directory)`)
	}
	for _, d := range n.AllowedDirs {
		if strings.TrimSpace(d) == "" {
			return fmt.Errorf("sandbox.allowed_dirs: entries must not be blank")
		}
	}
	switch n.Bash {
	case BashRestricted, BashUnrestricted:
	default:
		return fmt.Errorf("sandbox.bash: invalid value %q (want restricted or unrestricted)", c.Bash)
	}
	if n.MaxCommandTimeout < 0 {
		return fmt.Errorf("sandbox.max_command_timeout must be positive, got %s", n.MaxCommandTimeout)
	}
	for _, e := range n.AllowCommands {
		if _, err := ParseAllowEntry(e); err != nil {
			return fmt.Errorf("sandbox.%w", err)
		}
	}
	return nil
}
```

`ParseAllowEntry` errors start with `allow_commands entry …`, so `sandbox.%w` reads `sandbox.allow_commands entry …`.

- [ ] **Step 4: Implement `resolve.go`**

`pkg/sandbox/resolve.go`:

```go
package sandbox

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// resolveLenient returns p with every symlink evaluated. For paths that do
// not exist yet (write targets, lazily-created dirs) it evaluates the
// deepest existing ancestor and appends the rest. A dangling symlink
// anywhere on the path is an error: following it later could create a file
// outside the sandbox.
func resolveLenient(p string) (string, error) {
	cur := filepath.Clean(p)
	var rest []string // components below cur, outermost first
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if fi, lerr := os.Lstat(cur); lerr == nil && fi.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("%s is a symlink whose target does not exist", cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
}

// isRoot reports whether p is a filesystem root ("/").
func isRoot(p string) bool { return filepath.Dir(p) == p }

// within reports whether p is root or below it. Both must be clean,
// absolute and symlink-resolved. Comparison is per path element, so
// "/a-evil" is not within "/a".
func within(root, p string) bool {
	if isRoot(root) {
		return true
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
```

- [ ] **Step 5: Implement `sandbox.go`**

`pkg/sandbox/sandbox.go`:

```go
package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Access distinguishes reads from writes; read-only roots admit only Read.
type Access int

const (
	Read Access = iota
	Write
)

// Sandbox is the resolved, immutable runtime form of a Config.
type Sandbox struct {
	cfg      Config
	cwd      string
	dirs     []string // resolved read-write roots
	readOnly []string // resolved read-only roots
	rules    []AllowRule
	warnings []string
}

// New resolves cfg against cwd (which must be absolute). readOnlyDirs are
// extra roots readable but not writable (the agent's spill directory);
// they may not exist yet. Symlinks in every root are evaluated once, here.
func New(cfg Config, cwd string, readOnlyDirs ...string) (*Sandbox, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(cwd) {
		return nil, fmt.Errorf("sandbox: working directory %q is not absolute", cwd)
	}
	n := cfg.Normalize()
	s := &Sandbox{cfg: n}

	resolvedCwd, err := resolveLenient(cwd)
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolving working directory: %w", err)
	}
	s.cwd = resolvedCwd

	home := ""
	if h, err := os.UserHomeDir(); err == nil {
		home, _ = resolveLenient(h)
	}
	for _, entry := range n.AllowedDirs {
		abs := entry
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(s.cwd, abs)
		}
		resolved, err := resolveLenient(abs)
		if err != nil {
			return nil, fmt.Errorf("sandbox.allowed_dirs entry %q: %w", entry, err)
		}
		switch {
		case isRoot(resolved):
			s.warnings = append(s.warnings, fmt.Sprintf("sandbox.allowed_dirs entry %q resolves to / — file tools can reach the whole filesystem", entry))
		case home != "" && resolved == home:
			s.warnings = append(s.warnings, fmt.Sprintf("sandbox.allowed_dirs entry %q resolves to your home directory — file tools can reach every file you own", entry))
		}
		if _, err := os.Stat(resolved); err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("sandbox.allowed_dirs entry %q (%s) does not exist", entry, resolved))
		}
		s.dirs = append(s.dirs, resolved)
	}
	for _, d := range readOnlyDirs {
		if d == "" {
			continue
		}
		if resolved, err := resolveLenient(d); err == nil {
			s.readOnly = append(s.readOnly, resolved)
		}
	}
	for _, e := range n.AllowCommands {
		r, _ := ParseAllowEntry(e) // validated above
		s.rules = append(s.rules, r)
	}
	if n.Bash == BashUnrestricted {
		s.warnings = append(s.warnings, "sandbox.bash is unrestricted — run_command runs any shell command with your user's permissions")
	}
	return s, nil
}

// Resolve returns the absolute, symlink-free form of path if it lies inside
// an allowed directory (or, for Read, a read-only root). Relative paths
// resolve against the sandbox working directory. Tools must operate on the
// returned path, never the original.
func (s *Sandbox) Resolve(path string, access Access) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.cwd, abs)
	}
	resolved, err := resolveLenient(abs)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", path, err)
	}
	for _, d := range s.dirs {
		if within(d, resolved) {
			return resolved, nil
		}
	}
	if access == Read {
		for _, d := range s.readOnly {
			if within(d, resolved) {
				return resolved, nil
			}
		}
	}
	return "", fmt.Errorf("path %q is outside the allowed directories (%s); use a path inside them, or ask the user to extend sandbox.allowed_dirs",
		path, strings.Join(s.dirs, ", "))
}

// CheckCommand parses command and returns its argv if an allow_commands
// rule permits it. Only meaningful in restricted mode.
func (s *Sandbox) CheckCommand(command string) ([]string, error) {
	if len(s.rules) == 0 {
		return nil, fmt.Errorf("run_command is disabled for this agent: sandbox.allow_commands is empty. " +
			`Ask the user to allow specific commands (for example: <agent> config set sandbox.allow_commands "go test *"), or to set sandbox.bash: unrestricted`)
	}
	argv, err := SplitArgv(command)
	if err != nil {
		return nil, fmt.Errorf("command rejected: %w (run_command has no shell in restricted mode: pipes, redirects, chaining and substitution are unavailable)", err)
	}
	for _, r := range s.rules {
		if r.Matches(argv) {
			return argv, nil
		}
	}
	return nil, fmt.Errorf("command %q is not allowed; sandbox.allow_commands permits: %s", command, strings.Join(s.cfg.AllowCommands, "; "))
}

// Config returns the normalized configuration (a copy).
func (s *Sandbox) Config() Config { return s.cfg.Normalize() }

// Cwd returns the resolved working directory relative paths are joined to.
func (s *Sandbox) Cwd() string { return s.cwd }

// AllowedDirs returns the resolved read-write roots.
func (s *Sandbox) AllowedDirs() []string { return append([]string(nil), s.dirs...) }

// Warnings returns startup warnings (/, $HOME, missing dirs, unrestricted).
func (s *Sandbox) Warnings() []string { return append([]string(nil), s.warnings...) }
```

- [ ] **Step 6: Implement `context.go`**

`pkg/sandbox/context.go`:

```go
package sandbox

import (
	"context"
	"os"
)

type ctxKey struct{}

// NewContext returns a copy of ctx carrying s.
func NewContext(ctx context.Context, s *Sandbox) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// FromContext returns the sandbox carried by ctx. Without one it returns
// the secure default for the process working directory; if that cannot be
// determined, a sandbox with no roots, which denies every path.
func FromContext(ctx context.Context) *Sandbox {
	if s, ok := ctx.Value(ctxKey{}).(*Sandbox); ok && s != nil {
		return s
	}
	if cwd, err := os.Getwd(); err == nil {
		if s, err := New(Default(), cwd); err == nil {
			return s
		}
	}
	return &Sandbox{cfg: Default().Normalize()}
}
```

- [ ] **Step 7: Run the package tests with coverage**

Run: `go test ./pkg/sandbox/ -cover -v`
Expected: PASS, coverage ≥ 80%. If `TestNewWarnings` fails on the home check because the resolved `HOME` differs, make sure the test sets `HOME` to the already-resolved `root`, as the code above does.

- [ ] **Step 8: Commit**

```bash
git add pkg/sandbox/
```
```bash
git commit -m "feat: add sandbox path confinement, command check and context carrier

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: `pkg/tools` — `HandlerCtx`, executor timeout and sandbox injection, bounded capture, process groups

**Files:**
- Modify: `pkg/tools/registry.go` (`Definition`, new `BuiltinToolCtx`)
- Create: `pkg/tools/limit.go`, `pkg/tools/limit_test.go`
- Create: `pkg/tools/procgroup_unix.go`, `pkg/tools/procgroup_other.go`, `pkg/tools/procgroup_unix_test.go`
- Modify: `pkg/tools/executor.go` (`Executor.sandbox`, `WithSandbox`, `RunRaw`, new `runBuiltin`, `outputLimit`)
- Create: `pkg/tools/executor_ctx_test.go`
- Modify: `pkg/tools/policy.go`, `pkg/tools/policy_test.go`
- Modify: `pkg/tools/spill.go` (export `SpillDir`)

**Interfaces:**
- Consumes: `sandbox.Sandbox`, `sandbox.NewContext`, `sandbox.FromContext`, `(*Sandbox).Config().MaxCommandTimeout` (Task 2).
- Produces:
  - `Definition.HandlerCtx func(ctx context.Context, input map[string]any) (string, error)`
  - `Definition.UsesCommandTimeout bool`
  - `func BuiltinToolCtx(name, description string, schema any, handler func(ctx context.Context, input map[string]any) (string, error)) *Definition`
  - `func WithSandbox(s *sandbox.Sandbox) ExecutorOption`
  - `const DefaultMaxOutputBytes int64 = 10 << 20`
  - `func WithOutputLimit(ctx context.Context, n int64) context.Context`; `func OutputLimit(ctx context.Context) int64`
  - `type LimitedBuffer`; `func NewLimitedBuffer(limit int64) *LimitedBuffer`; methods `Write`, `String`, `Truncated`
  - `func ConfigureProcessGroup(cmd *exec.Cmd)`
  - `func SpillDir(agentName string) string`

- [ ] **Step 1: Write the failing limit tests**

`pkg/tools/limit_test.go`:

```go
package tools

import (
	"context"
	"strings"
	"testing"
)

func TestLimitedBuffer(t *testing.T) {
	b := NewLimitedBuffer(4)
	n, err := b.Write([]byte("abcdef"))
	if n != 6 || err != nil {
		t.Fatalf("Write = %d, %v; must report full length so the writer never blocks", n, err)
	}
	b.Write([]byte("gh"))
	if !b.Truncated() {
		t.Fatal("Truncated() = false")
	}
	if got, want := b.String(), "abcd\n[output truncated at 4 bytes]"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	small := NewLimitedBuffer(100)
	small.Write([]byte("ok"))
	if small.Truncated() || small.String() != "ok" {
		t.Errorf("under-limit buffer = %q truncated=%v", small.String(), small.Truncated())
	}

	def := NewLimitedBuffer(0)
	def.Write([]byte(strings.Repeat("x", 1000)))
	if def.Truncated() {
		t.Error("limit 0 must mean DefaultMaxOutputBytes, not zero")
	}
}

func TestOutputLimitContext(t *testing.T) {
	if got := OutputLimit(context.Background()); got != DefaultMaxOutputBytes {
		t.Errorf("default OutputLimit = %d, want %d", got, DefaultMaxOutputBytes)
	}
	if got := OutputLimit(WithOutputLimit(context.Background(), 42)); got != 42 {
		t.Errorf("OutputLimit = %d, want 42", got)
	}
	if got := OutputLimit(WithOutputLimit(context.Background(), 0)); got != DefaultMaxOutputBytes {
		t.Errorf("non-positive limit must fall back to default, got %d", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/tools/ -run 'LimitedBuffer|OutputLimit' -v`
Expected: FAIL with `undefined: NewLimitedBuffer`.

- [ ] **Step 3: Implement `limit.go`**

`pkg/tools/limit.go`:

```go
package tools

import (
	"bytes"
	"context"
	"fmt"
	"sync"
)

// DefaultMaxOutputBytes bounds captured subprocess output in memory when no
// CommandPolicy sets MaxOutputBytes. The context budget bounds what reaches
// the model; this bounds what the agent process holds.
const DefaultMaxOutputBytes int64 = 10 << 20

// LimitedBuffer keeps the first limit bytes written and discards the rest.
// Write always reports success so a subprocess is never blocked on a full
// pipe. Safe for concurrent writers (stdout and stderr sharing one buffer).
type LimitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

// NewLimitedBuffer returns a buffer capped at limit bytes; limit <= 0 means
// DefaultMaxOutputBytes.
func NewLimitedBuffer(limit int64) *LimitedBuffer {
	if limit <= 0 {
		limit = DefaultMaxOutputBytes
	}
	return &LimitedBuffer{limit: limit}
}

func (b *LimitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	room := b.limit - int64(b.buf.Len())
	if room <= 0 {
		b.truncated = b.truncated || len(p) > 0
		return len(p), nil
	}
	if int64(len(p)) > room {
		b.buf.Write(p[:room])
		b.truncated = true
		return len(p), nil
	}
	b.buf.Write(p)
	return len(p), nil
}

// Truncated reports whether any bytes were discarded.
func (b *LimitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// String returns the kept bytes, plus a truncation marker when bytes were dropped.
func (b *LimitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.truncated {
		return b.buf.String()
	}
	return b.buf.String() + fmt.Sprintf("\n[output truncated at %d bytes]", b.limit)
}

type outputLimitKey struct{}

// WithOutputLimit returns ctx carrying the output byte cap for subprocess capture.
func WithOutputLimit(ctx context.Context, n int64) context.Context {
	return context.WithValue(ctx, outputLimitKey{}, n)
}

// OutputLimit returns the cap carried by ctx, or DefaultMaxOutputBytes.
func OutputLimit(ctx context.Context) int64 {
	if n, ok := ctx.Value(outputLimitKey{}).(int64); ok && n > 0 {
		return n
	}
	return DefaultMaxOutputBytes
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./pkg/tools/ -run 'LimitedBuffer|OutputLimit' -v`
Expected: PASS.

- [ ] **Step 5: Write the failing executor, process-group and policy tests**

`pkg/tools/executor_ctx_test.go`:

```go
package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

func testSandbox(t *testing.T, cfg sandbox.Config) *sandbox.Sandbox {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := sandbox.New(cfg, root)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestExecutorPrefersHandlerCtx(t *testing.T) {
	def := &Definition{
		Name: "t", Builtin: true,
		Handler:    func(map[string]any) (string, error) { return "legacy", nil },
		HandlerCtx: func(context.Context, map[string]any) (string, error) { return "ctx", nil },
	}
	got, err := NewExecutor(time.Second, nil).RunRaw(context.Background(), def, nil)
	if err != nil || got != "ctx" {
		t.Fatalf("RunRaw = %q, %v; want ctx", got, err)
	}
}

func TestExecutorLegacyHandlerStillRuns(t *testing.T) {
	def := BuiltinTool("t", "d", nil, func(map[string]any) (string, error) { return "legacy", nil })
	got, err := NewExecutor(time.Second, nil).RunRaw(context.Background(), def, nil)
	if err != nil || got != "legacy" {
		t.Fatalf("RunRaw = %q, %v", got, err)
	}
}

func TestExecutorInjectsSandbox(t *testing.T) {
	sb := testSandbox(t, sandbox.Config{MaxCommandTimeout: 7 * time.Second})
	def := BuiltinToolCtx("t", "d", nil, func(ctx context.Context, _ map[string]any) (string, error) {
		return sandbox.FromContext(ctx).Config().MaxCommandTimeout.String(), nil
	})
	got, err := NewExecutor(time.Second, nil, WithSandbox(sb)).RunRaw(context.Background(), def, nil)
	if err != nil || got != "7s" {
		t.Fatalf("handler saw %q, %v; want 7s", got, err)
	}
}

func TestExecutorTimeoutAppliesToHandlerCtx(t *testing.T) {
	def := BuiltinToolCtx("slow", "d", nil, func(ctx context.Context, _ map[string]any) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	_, err := NewExecutor(50*time.Millisecond, nil).RunRaw(context.Background(), def, nil)
	if err == nil || !strings.Contains(err.Error(), `tool "slow" timed out after 50ms`) {
		t.Fatalf("err = %v", err)
	}
}

func TestExecutorCommandTimeoutExtendsOuterLimit(t *testing.T) {
	sb := testSandbox(t, sandbox.Config{MaxCommandTimeout: 2 * time.Second})
	def := BuiltinToolCtx("run_command", "d", nil, func(ctx context.Context, _ map[string]any) (string, error) {
		dl, ok := ctx.Deadline()
		if !ok {
			return "", fmt.Errorf("no deadline")
		}
		return fmt.Sprint(time.Until(dl) > time.Second), nil
	})
	def.UsesCommandTimeout = true
	got, err := NewExecutor(50*time.Millisecond, nil, WithSandbox(sb)).RunRaw(context.Background(), def, nil)
	if err != nil || got != "true" {
		t.Fatalf("outer limit should be max(50ms, 2s); got %q, %v", got, err)
	}
}

func TestExecutorInjectsOutputLimit(t *testing.T) {
	probe := func(ctx context.Context, _ map[string]any) (string, error) { return fmt.Sprint(OutputLimit(ctx)), nil }
	def := BuiltinToolCtx("t", "d", nil, probe)
	def.Policy = &CommandPolicy{MaxOutputBytes: 5}
	if got, _ := NewExecutor(time.Second, nil).RunRaw(context.Background(), def, nil); got != "5" {
		t.Errorf("per-tool policy limit = %s, want 5", got)
	}
	plain := BuiltinToolCtx("t", "d", nil, probe)
	if got, _ := NewExecutor(time.Second, nil).RunRaw(context.Background(), plain, nil); got != fmt.Sprint(DefaultMaxOutputBytes) {
		t.Errorf("no policy limit = %s, want default", got)
	}
	zero := BuiltinToolCtx("t", "d", nil, probe)
	ex := NewExecutor(time.Second, nil, WithDefaultPolicy(&CommandPolicy{AllowedPrefixes: []string{"go "}}))
	if got, _ := ex.RunRaw(context.Background(), zero, nil); got != fmt.Sprint(DefaultMaxOutputBytes) {
		t.Errorf("policy with MaxOutputBytes 0 must use default, got %s", got)
	}
}

func TestExecutorCLIBoundedCapture(t *testing.T) {
	def := &Definition{
		Name: "big", Command: "sh",
		Args:   []string{"-c", "head -c 100000 /dev/zero | tr '\\0' x"},
		Policy: &CommandPolicy{MaxOutputBytes: 1000},
	}
	got, err := NewExecutor(5*time.Second, nil).RunRaw(context.Background(), def, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "[output truncated at 1000 bytes]") || len(got) > 1100 {
		t.Errorf("len=%d tail=%q", len(got), got[max(0, len(got)-40):])
	}
}

func TestBuiltinToolCtxSetsLegacyHandler(t *testing.T) {
	def := BuiltinToolCtx("t", "d", nil, func(ctx context.Context, _ map[string]any) (string, error) {
		if sandbox.FromContext(ctx) == nil {
			return "", fmt.Errorf("no sandbox")
		}
		return "ok", nil
	})
	if !def.Builtin || def.Handler == nil {
		t.Fatal("BuiltinToolCtx must set Builtin and a Handler wrapper")
	}
	if got, err := def.Handler(nil); err != nil || got != "ok" {
		t.Fatalf("Handler wrapper = %q, %v", got, err)
	}
}
```

`pkg/tools/procgroup_unix_test.go`:

```go
//go:build unix

package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// waitGone polls until pid no longer exists (ESRCH) or the deadline passes.
func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("grandchild %d survived cancellation", pid)
}

func readPid(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("pid file never written")
	return 0
}

// Review Focus #5.
func TestExecutorCLIKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	def := &Definition{
		Name: "spawner", Command: "sh",
		Args: []string{"-c", "sleep 60 & echo $! > " + pidFile + "; wait"},
	}
	_, err := NewExecutor(300*time.Millisecond, nil).RunRaw(context.Background(), def, nil)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want timeout", err)
	}
	waitGone(t, readPid(t, pidFile))
}
```

In `pkg/tools/policy_test.go`, delete the three table entries named `deny list blocks dangerous commands`, `deny list blocks fork bomb` and `deny list allows safe commands`, and append this test:

```go
func TestDefaultCommandPolicy_NoDenylist(t *testing.T) {
	p := DefaultCommandPolicy()
	if len(p.DeniedSubstrings) != 0 {
		t.Errorf("default denylist must be gone (it was never a security boundary), got %q", p.DeniedSubstrings)
	}
	if p.MaxOutputBytes != DefaultMaxOutputBytes {
		t.Errorf("MaxOutputBytes = %d, want %d", p.MaxOutputBytes, DefaultMaxOutputBytes)
	}
}
```

- [ ] **Step 6: Run to verify failure**

Run: `go test ./pkg/tools/ -v`
Expected: FAIL to build with `undefined: BuiltinToolCtx`, `WithSandbox`, and `Definition.HandlerCtx`.

- [ ] **Step 7: Implement `registry.go`, process groups, the executor, policy and spill**

In `pkg/tools/registry.go`, add `"context"` to the imports. Replace the `// For built-in tools` field block with:

```go
	// For built-in tools. The executor prefers HandlerCtx: it receives the
	// executor timeout, the sandbox and the output cap through ctx. Handler
	// is kept for existing tools and callers; it cannot be cancelled.
	Handler    func(input map[string]any) (string, error)
	HandlerCtx func(ctx context.Context, input map[string]any) (string, error)
	// UsesCommandTimeout marks tools (run_command) whose handler enforces
	// sandbox.max_command_timeout itself. The executor's outer limit becomes
	// max(executor timeout, max_command_timeout) so that cap is reachable.
	UsesCommandTimeout bool
```

Append after `BuiltinTool`:

```go
// BuiltinToolCtx creates a built-in tool with a context-aware handler. It
// also sets Handler to a wrapper using context.Background(), so callers that
// invoke Handler directly keep working under the default sandbox.
func BuiltinToolCtx(name, description string, schema any, handler func(ctx context.Context, input map[string]any) (string, error)) *Definition {
	return &Definition{
		Name:        name,
		Description: description,
		InputSchema: schema,
		Builtin:     true,
		HandlerCtx:  handler,
		Handler: func(input map[string]any) (string, error) {
			return handler(context.Background(), input)
		},
	}
}
```

`pkg/tools/procgroup_unix.go`:

```go
//go:build unix

package tools

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// waitDelay bounds how long Wait blocks on inherited pipes after a kill.
const waitDelay = 2 * time.Second

// ConfigureProcessGroup starts cmd in its own process group and makes
// context cancellation kill the whole group; exec.CommandContext alone
// kills only the direct child and orphans grandchildren.
func ConfigureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone // exited at the deadline; not a failure
			}
			return err
		}
		return nil
	}
	cmd.WaitDelay = waitDelay
}
```

`pkg/tools/procgroup_other.go`:

```go
//go:build !unix

package tools

import (
	"os/exec"
	"time"
)

// ConfigureProcessGroup on non-unix platforms only bounds Wait; process
// groups are not portable there. Agents are built for darwin and linux.
func ConfigureProcessGroup(cmd *exec.Cmd) {
	cmd.WaitDelay = 2 * time.Second
}
```

In `pkg/tools/executor.go`:
- Add `"github.com/teabranch/abbyfile/pkg/sandbox"` to the imports.
- Add the field `sandbox *sandbox.Sandbox // nil = sandbox.FromContext fallback` to `Executor`.
- Add the option:

```go
// WithSandbox sets the sandbox injected into every HandlerCtx call.
func WithSandbox(s *sandbox.Sandbox) ExecutorOption {
	return func(e *Executor) { e.sandbox = s }
}
```

Replace the builtin branch at the top of `RunRaw`. The old code ran from `if def.Builtin {` through its closing `}` at executor.go:72-89. The new branch:

```go
	if def.Builtin {
		if def.HandlerCtx == nil && def.Handler == nil {
			return "", fmt.Errorf("built-in tool %q has no handler", def.Name)
		}
		e.logger.Info("running builtin tool", "tool", def.Name)
		start := time.Now()
		result, err := e.runBuiltin(ctx, def, input)
		duration := time.Since(start)
		if e.hook != nil {
			e.hook(def.Name, duration, err)
		}
		if err != nil {
			e.logger.Error("builtin tool failed", "tool", def.Name, "duration", duration, "error", err)
			return "", err
		}
		e.logger.Info("builtin tool completed", "tool", def.Name, "duration", duration)
		return result, nil
	}
```

In the CLI path:
- Right after `cmd := exec.CommandContext(ctx, def.Command, args...)`, add `ConfigureProcessGroup(cmd)`.
- Replace `var stdout, stderr bytes.Buffer` with:

```go
	limit := e.outputLimit(def)
	stdout, stderr := NewLimitedBuffer(limit), NewLimitedBuffer(limit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
```

- Delete the two old `cmd.Stdout = &stdout` / `cmd.Stderr = &stderr` lines. The existing `stdout.String()` and `stderr.String()` calls stay valid.
- Keep the `bytes` import, which `bytes.NewReader` still uses.

Append to `executor.go`:

```go
// runBuiltin calls the tool's handler. HandlerCtx runs under the executor
// timeout (extended to max_command_timeout for UsesCommandTimeout tools)
// with the sandbox and output cap in ctx. Legacy Handler takes no context,
// so no timeout can apply to it.
func (e *Executor) runBuiltin(ctx context.Context, def *Definition, input map[string]any) (string, error) {
	if def.HandlerCtx == nil {
		return def.Handler(input)
	}
	sb := e.sandbox
	if sb == nil {
		sb = sandbox.FromContext(ctx)
	}
	limit := e.timeout
	if def.UsesCommandTimeout {
		limit = max(limit, sb.Config().MaxCommandTimeout)
	}
	hctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	hctx = sandbox.NewContext(hctx, sb)
	hctx = WithOutputLimit(hctx, e.outputLimit(def))
	result, err := def.HandlerCtx(hctx, input)
	if err != nil && errors.Is(hctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("tool %q timed out after %s", def.Name, limit)
	}
	return result, err
}

// outputLimit is the memory cap for captured output: the tool's (or the
// executor's default) CommandPolicy.MaxOutputBytes when positive, else
// DefaultMaxOutputBytes. Zero never means unlimited here.
func (e *Executor) outputLimit(def *Definition) int64 {
	p := def.Policy
	if p == nil {
		p = e.defaultPolicy
	}
	if p != nil && p.MaxOutputBytes > 0 {
		return p.MaxOutputBytes
	}
	return DefaultMaxOutputBytes
}
```

In `pkg/tools/policy.go`, replace the `CommandPolicy` doc comment, the `MaxOutputBytes` field comment, and `DefaultCommandPolicy` with:

```go
// CommandPolicy defines execution constraints for custom CLI tools.
// AllowedPrefixes and DeniedSubstrings are plain string checks on the
// argument string — conveniences, not a security boundary. run_command is
// governed by pkg/sandbox instead.
type CommandPolicy struct {
	AllowedPrefixes  []string // if non-empty, command must start with one of these
	DeniedSubstrings []string // command must not contain any of these
	MaxOutputBytes   int64    // cap on captured stdout/stderr each (<=0 = DefaultMaxOutputBytes)
}

// DefaultCommandPolicy returns the default policy: no string checks and a
// DefaultMaxOutputBytes capture cap.
func DefaultCommandPolicy() *CommandPolicy {
	return &CommandPolicy{MaxOutputBytes: DefaultMaxOutputBytes}
}
```

In `pkg/tools/spill.go`, add the function below. In `tempFileSink.Put`, replace the `home, err := os.UserHomeDir()` block and the `dir := filepath.Join(home, ".abbyfile", filepath.Base(s.agentName), "spill")` line with `dir := SpillDir(s.agentName)` plus `if dir == "" { return "", fmt.Errorf("resolving home directory for spill") }`.

```go
// SpillDir is where the temp-file sink writes overflow for agentName, or ""
// if the home directory cannot be resolved. The agent passes it to the
// sandbox as a read-only root so the model can read spilled output.
func SpillDir(agentName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".abbyfile", filepath.Base(agentName), "spill")
}
```

- [ ] **Step 8: Run the package tests**

Run: `go test ./pkg/tools/ -cover -v`
Expected: PASS, coverage ≥ 80%. `pkg/builtins` will not compile yet; Tasks 4 and 5 fix it. Run `go build ./pkg/tools/ ./pkg/sandbox/` to confirm these two packages build.

- [ ] **Step 9: Commit**

```bash
git add pkg/tools/
```
```bash
git commit -m "feat: context-aware builtin handlers, executor sandbox and timeout, bounded capture, process-group kill

Co-Authored-By: Claude <noreply@anthropic.com>"
```

`go build ./... && go test ./...` must pass at this commit. `pkg/builtins` still uses `tools.BuiltinTool` with one-argument handlers, and `defaultRunCommandPolicy.Check` still compiles against a policy that no longer has a denylist. Nothing breaks until Task 4 changes the handler signatures.

---

### Task 4: Confine the file tools (`read_file`, `write_file`, `edit_file`, `glob_files`, `grep_search`)

**Files:**
- Create: `pkg/builtins/paths.go`
- Modify: `pkg/builtins/read.go`, `write.go`, `edit.go`, `glob.go`, `grep.go`
- Create: `pkg/builtins/sandbox_helpers_test.go`, `pkg/builtins/files_sandbox_test.go`
- Modify: `pkg/builtins/builtins_test.go` (existing tests move to the context handlers)

**Interfaces:**
- Consumes: `tools.BuiltinToolCtx` (Task 3); `sandbox.FromContext`, `(*Sandbox).Resolve`, `sandbox.Read`, `sandbox.Write`, `sandbox.New`, `sandbox.NewContext` (Task 2).
- Produces: handler signatures `func(ctx context.Context, input map[string]any) (string, error)` for `handleReadFile`, `handleWriteFile`, `handleEditFile`, `handleGlobFiles`, `handleGrepSearch`. The test helper `sandboxCtx(t, root string, cfg sandbox.Config) context.Context` and `realTempDir(t) string` are reused by Task 5's tests.

- [ ] **Step 1: Write the failing tests**

`pkg/builtins/sandbox_helpers_test.go`:

```go
package builtins

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

func realTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// sandboxCtx returns a context whose sandbox has cwd = root. A zero cfg
// means sandbox.Default() (allowed_dirs ["."] = root).
func sandboxCtx(t *testing.T, root string, cfg sandbox.Config) context.Context {
	t.Helper()
	sb, err := sandbox.New(cfg, root)
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	return sandbox.NewContext(context.Background(), sb)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

`pkg/builtins/files_sandbox_test.go`:

```go
package builtins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

type fixture struct{ root, outside string }

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{root: realTempDir(t), outside: realTempDir(t)}
	writeFile(t, filepath.Join(f.root, "in.go"), "package in\n// needle inside\n")
	writeFile(t, filepath.Join(f.root, "sub", "deep.go"), "package sub\n// needle deep\n")
	writeFile(t, filepath.Join(f.outside, "secret.go"), "package secret\n// needle secret\n")
	if err := os.Symlink(filepath.Join(f.outside, "secret.go"), filepath.Join(f.root, "link.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.outside, filepath.Join(f.root, "outdir")); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestReadFile_OutsideDenied(t *testing.T) {
	f := newFixture(t)
	ctx := sandboxCtx(t, f.root, sandbox.Config{})
	for _, p := range []string{filepath.Join(f.outside, "secret.go"), "link.go", "outdir/secret.go"} {
		if _, err := handleReadFile(ctx, map[string]any{"path": p}); err == nil || !strings.Contains(err.Error(), "outside the allowed directories") {
			t.Errorf("read %q: err = %v, want denial", p, err)
		}
	}
}

// Review Focus #3.
func TestReadFile_RelativeUsesSandboxCwd(t *testing.T) {
	f := newFixture(t) // process cwd is the package dir, not f.root
	got, err := handleReadFile(sandboxCtx(t, f.root, sandbox.Config{}), map[string]any{"path": "in.go"})
	if err != nil || !strings.Contains(got, "package in") {
		t.Fatalf("relative read = %q, %v", got, err)
	}
}

func TestWriteFile_OutsideDeniedCreatesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := sandboxCtx(t, f.root, sandbox.Config{})
	target := filepath.Join(f.outside, "newdir", "x.txt")
	if _, err := handleWriteFile(ctx, map[string]any{"path": target, "content": "x"}); err == nil {
		t.Fatal("write outside must be denied")
	}
	if _, err := os.Stat(filepath.Join(f.outside, "newdir")); !os.IsNotExist(err) {
		t.Fatal("denied write must not create parent directories")
	}
	if _, err := handleWriteFile(ctx, map[string]any{"path": "outdir/new.txt", "content": "x"}); err == nil {
		t.Fatal("write through a symlinked parent must be denied")
	}
}

func TestWriteFile_RelativeCreatesUnderSandboxCwd(t *testing.T) {
	f := newFixture(t)
	if _, err := handleWriteFile(sandboxCtx(t, f.root, sandbox.Config{}), map[string]any{"path": "made/new.txt", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(f.root, "made", "new.txt")); err != nil || string(b) != "hi" {
		t.Fatalf("file not created under sandbox cwd: %q, %v", b, err)
	}
}

func TestEditFile_SymlinkEscapeDenied(t *testing.T) {
	f := newFixture(t)
	_, err := handleEditFile(sandboxCtx(t, f.root, sandbox.Config{}), map[string]any{
		"path": "link.go", "old_string": "secret", "new_string": "pwned",
	})
	if err == nil {
		t.Fatal("edit through escaping symlink must be denied")
	}
	if b, _ := os.ReadFile(filepath.Join(f.outside, "secret.go")); strings.Contains(string(b), "pwned") {
		t.Fatal("outside file was modified")
	}
}

func TestGlobFiles_SkipsEscapes(t *testing.T) {
	f := newFixture(t)
	ctx := sandboxCtx(t, f.root, sandbox.Config{})
	got, err := handleGlobFiles(ctx, map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "in.go") || strings.Contains(got, "link.go") {
		t.Errorf("glob *.go = %q; want in.go, not link.go", got)
	}
	if !strings.Contains(got, "1 entries outside the allowed directories were skipped") {
		t.Errorf("missing skip note: %q", got)
	}
	got, err = handleGlobFiles(ctx, map[string]any{"pattern": "outdir/*.go"})
	if err != nil || strings.Contains(got, "secret.go") {
		t.Errorf("glob through symlinked dir leaked: %q, %v", got, err)
	}
}

// Review Focus #3.
func TestGlobFiles_RelativeOutputPreserved(t *testing.T) {
	f := newFixture(t)
	got, err := handleGlobFiles(sandboxCtx(t, f.root, sandbox.Config{}), map[string]any{"pattern": "**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.SplitN(got, "\n(", 2)[0], "\n")
	want := map[string]bool{"in.go": true, filepath.Join("sub", "deep.go"): true}
	for _, l := range lines {
		if !want[l] {
			t.Errorf("unexpected line %q in %q (must stay relative, no escapes)", l, got)
		}
	}
}

func TestGlobFiles_RootOutsideDenied(t *testing.T) {
	f := newFixture(t)
	ctx := sandboxCtx(t, f.root, sandbox.Config{})
	if _, err := handleGlobFiles(ctx, map[string]any{"pattern": "*.go", "path": f.outside}); err == nil {
		t.Error("glob base outside must be denied")
	}
	if _, err := handleGlobFiles(ctx, map[string]any{"pattern": "../**/*.go"}); err == nil {
		t.Error("glob ** prefix escaping the sandbox must be denied before walking")
	}
}

func TestGrepSearch_SkipsEscapes(t *testing.T) {
	f := newFixture(t)
	got, err := handleGrepSearch(sandboxCtx(t, f.root, sandbox.Config{}), map[string]any{"pattern": "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "secret") {
		t.Errorf("grep leaked outside content: %q", got)
	}
	if !strings.Contains(got, "needle inside") || !strings.Contains(got, "needle deep") {
		t.Errorf("grep missed inside matches: %q", got)
	}
	if !strings.HasPrefix(got, "in.go:") && !strings.Contains(got, "\nin.go:") {
		t.Errorf("grep output must stay relative to the given path: %q", got)
	}
}

func TestGrepSearch_RootOutsideDenied(t *testing.T) {
	f := newFixture(t)
	ctx := sandboxCtx(t, f.root, sandbox.Config{})
	for _, p := range []string{f.outside, "link.go"} {
		if _, err := handleGrepSearch(ctx, map[string]any{"pattern": "needle", "path": p}); err == nil {
			t.Errorf("grep path %q must be denied", p)
		}
	}
}

func TestFileToolAnnotations(t *testing.T) {
	for _, def := range []struct {
		name        string
		destructive bool
		readOnly    bool
	}{
		{"write_file", true, false}, {"edit_file", true, false},
		{"read_file", false, true}, {"glob_files", false, true}, {"grep_search", false, true},
	} {
		tool := toolByName(t, def.name)
		a := tool.Annotations
		if def.destructive && (a.DestructiveHint == nil || !*a.DestructiveHint) {
			t.Errorf("%s: DestructiveHint must be true", def.name)
		}
		if def.readOnly && (!a.ReadOnlyHint || !a.IdempotentHint) {
			t.Errorf("%s: want ReadOnlyHint and IdempotentHint", def.name)
		}
	}
}
```

Add this helper to `sandbox_helpers_test.go`:

```go
func toolByName(t *testing.T, name string) *tools.Definition {
	t.Helper()
	for _, d := range All() {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no builtin %q", name)
	return nil
}
```

Also add the `"github.com/teabranch/abbyfile/pkg/tools"` import to `sandbox_helpers_test.go`.

In `pkg/builtins/builtins_test.go`, update every existing file-tool test to the new signatures:
- `TestReadFile`, `TestReadFile_NotFound`, `TestWriteFile`, `TestEditFile*`, `TestGlobFiles*`, `TestGrepSearch*`: use `dir := realTempDir(t)` instead of `t.TempDir()`, and call `handleX(sandboxCtx(t, dir, sandbox.Config{}), map[string]any{...})`.
- `TestReadFile_NotFound`: read `filepath.Join(dir, "nonexistent")` instead of `/nonexistent/file`, so the test still exercises "not found" rather than a denial.
- Add the imports `"github.com/teabranch/abbyfile/pkg/sandbox"`.
- Remove `TestRunCommand`. Task 5 replaces it in `bash_test.go`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/builtins/ -v`
Expected: FAIL to build, because the handlers take one argument where the tests pass two.

- [ ] **Step 3: Implement `paths.go`**

`pkg/builtins/paths.go`:

```go
package builtins

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

// displayPath re-expresses hit (an absolute path under resolvedBase) relative
// to the caller's original base argument, so output keeps the shape the
// model asked for: "sub/a.go" for base ".", absolute for an absolute base.
func displayPath(base, resolvedBase, hit string) string {
	rel, err := filepath.Rel(resolvedBase, hit)
	if err != nil {
		return hit
	}
	return filepath.Join(base, rel)
}

// allowedEntry reports whether a walked entry may be used. Regular entries
// under a resolved root are inside by construction; symlinks are resolved
// and checked because WalkDir reports them without following.
func allowedEntry(sb *sandbox.Sandbox, path string, d fs.DirEntry) bool {
	if d.Type()&fs.ModeSymlink == 0 {
		return true
	}
	_, err := sb.Resolve(path, sandbox.Read)
	return err == nil
}

// withSkipNote appends a note when entries outside the sandbox were skipped.
func withSkipNote(out string, skipped int) string {
	if skipped == 0 {
		return out
	}
	return fmt.Sprintf("%s\n(%d entries outside the allowed directories were skipped)", out, skipped)
}
```

- [ ] **Step 4: Implement the file tools**

`pkg/builtins/read.go`:
- The constructor calls `tools.BuiltinToolCtx(` instead of `tools.BuiltinTool(`.
- Tool description: `"Read the contents of a file. Paths may be relative to the working directory; paths outside the allowed directories are refused."`
- `path` property description: `"Path to the file to read"`.
- Imports: `"context"`, `"fmt"`, `"os"`, sandbox, tools.

The handler:

```go
func handleReadFile(ctx context.Context, input map[string]any) (string, error) {
	path, ok := input["path"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: path")
	}
	resolved, err := sandbox.FromContext(ctx).Resolve(path, sandbox.Read)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	return string(data), nil
}
```

`pkg/builtins/write.go`:
- `tools.BuiltinToolCtx`.
- Description: `"Write content to a file, creating parent directories if needed. Overwrites existing files. Paths outside the allowed directories are refused."`
- `path` property description: `"Path to the file to write"`.
- Annotations: `DestructiveHint: tools.BoolPtr(true)`.

The handler:

```go
func handleWriteFile(ctx context.Context, input map[string]any) (string, error) {
	path, ok := input["path"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: path")
	}
	content, ok := input["content"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: content")
	}
	// Resolve before MkdirAll: a denied write must not create directories.
	resolved, err := sandbox.FromContext(ctx).Resolve(path, sandbox.Write)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return "", fmt.Errorf("creating directories: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(content), path), nil
}
```

`pkg/builtins/edit.go`:
- `tools.BuiltinToolCtx`.
- Description: `"Edit a file by replacing one exact, unique string match. Paths outside the allowed directories are refused."`
- `path` property description: `"Path to the file to edit"`.
- Annotations: `DestructiveHint: tools.BoolPtr(true)`.

In `handleEditFile(ctx context.Context, input map[string]any)`, after the three parameter checks insert:

```go
	resolved, err := sandbox.FromContext(ctx).Resolve(path, sandbox.Write)
	if err != nil {
		return "", err
	}
```

Then use `resolved` in `os.Stat`, `os.ReadFile` and `os.WriteFile`. Keep `fmt.Sprintf("Edited %s", path)`.

`pkg/builtins/glob.go`: switch to `tools.BuiltinToolCtx` and replace the handler with:

```go
func handleGlobFiles(ctx context.Context, input map[string]any) (string, error) {
	pattern, ok := input["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: pattern")
	}
	sb := sandbox.FromContext(ctx)
	baseDir := "."
	if p, ok := input["path"].(string); ok && p != "" {
		baseDir = p
	}
	resolvedBase, err := sb.Resolve(baseDir, sandbox.Read)
	if err != nil {
		return "", err
	}

	var matches []string
	skipped := 0

	if !strings.Contains(pattern, "**") {
		found, err := filepath.Glob(filepath.Join(resolvedBase, pattern))
		if err != nil {
			return "", fmt.Errorf("globbing: %w", err)
		}
		// Glob follows symlinked directories and ".." in the pattern, so
		// every hit is checked.
		for _, m := range found {
			if _, err := sb.Resolve(m, sandbox.Read); err != nil {
				skipped++
				continue
			}
			matches = append(matches, displayPath(baseDir, resolvedBase, m))
		}
	} else {
		parts := strings.SplitN(pattern, "**", 2)
		prefix := parts[0]
		suffix := strings.TrimPrefix(strings.TrimPrefix(parts[1], "/"), string(filepath.Separator))
		// Resolve the walk root before walking: a prefix like "../../"
		// must be refused, not walked.
		searchDir, err := sb.Resolve(filepath.Join(resolvedBase, prefix), sandbox.Read)
		if err != nil {
			return "", err
		}
		err = filepath.WalkDir(searchDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if !allowedEntry(sb, path, d) {
				skipped++
				return nil
			}
			if suffix != "" {
				rel, relErr := filepath.Rel(searchDir, path)
				if relErr != nil {
					return nil
				}
				matched, _ := filepath.Match(suffix, rel)
				if !matched {
					matched, _ = filepath.Match(suffix, filepath.Base(path))
				}
				if !matched {
					return nil
				}
			}
			matches = append(matches, displayPath(baseDir, resolvedBase, path))
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walking directory: %w", err)
		}
	}

	sort.Strings(matches)
	if len(matches) == 0 {
		return withSkipNote("No files matched.", skipped), nil
	}
	return withSkipNote(strings.Join(matches, "\n"), skipped), nil
}
```

`TestGlobFiles_SkipsEscapes` expects the skip counter to count `link.go` for pattern `*.go`. It does, because `filepath.Glob` returns `link.go` and `Resolve` then denies it. The pattern `outdir/*.go` returns `outdir/secret.go`, which is denied and skipped.

`pkg/builtins/grep.go`: switch to `tools.BuiltinToolCtx`. Change `searchFile` to take a display label, and replace the handler with:

```go
func handleGrepSearch(ctx context.Context, input map[string]any) (string, error) {
	pattern, ok := input["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: pattern")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}
	sb := sandbox.FromContext(ctx)
	searchPath := "."
	if p, ok := input["path"].(string); ok && p != "" {
		searchPath = p
	}
	globFilter, _ := input["glob"].(string)

	root, err := sb.Resolve(searchPath, sandbox.Read)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", searchPath, err)
	}
	const maxResults = 100
	if !info.IsDir() {
		return searchFile(root, searchPath, re, maxResults)
	}

	var results []string
	skipped := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if globFilter != "" {
			if matched, _ := filepath.Match(globFilter, filepath.Base(path)); !matched {
				return nil
			}
		}
		if !allowedEntry(sb, path, d) {
			skipped++
			return nil
		}
		label := displayPath(searchPath, root, path)
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			if re.MatchString(scanner.Text()) {
				results = append(results, fmt.Sprintf("%s:%d:%s", label, lineNum, scanner.Text()))
				if len(results) >= maxResults {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("searching: %w", err)
	}
	if len(results) == 0 {
		return withSkipNote("No matches found.", skipped), nil
	}
	return withSkipNote(strings.Join(results, "\n"), skipped), nil
}

// searchFile greps one file at path, labelling hits with label.
func searchFile(path, label string, re *regexp.Regexp, maxResults int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	var results []string
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if re.MatchString(scanner.Text()) {
			results = append(results, fmt.Sprintf("%s:%d:%s", label, lineNum, scanner.Text()))
			if len(results) >= maxResults {
				break
			}
		}
	}
	if len(results) == 0 {
		return "No matches found.", nil
	}
	return strings.Join(results, "\n"), nil
}
```

The hidden-directory check compares `path != root` instead of `d.Name() != "."`, because the walk root is now absolute.

For `read.go`, `write.go`, `edit.go`, `glob.go` and `grep.go`, add the imports `"context"` and `"github.com/teabranch/abbyfile/pkg/sandbox"`.

- [ ] **Step 5: Run the builtins tests**

Run: `go test ./pkg/builtins/ -run 'Read|Write|Edit|Glob|Grep|Annotations|ForNames|All' -v`
Expected: PASS. `bash.go` still compiles, because it keeps the old `handleRunCommand(input)` passed to `tools.BuiltinTool`.

- [ ] **Step 6: Commit**

```bash
git add pkg/builtins/
```
```bash
git commit -m "feat: confine file builtins to sandbox.allowed_dirs; fix destructive annotations

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 5: `run_command` — allowlist without a shell, timeout clamp, process group, bounded output

**Files:**
- Modify: `pkg/builtins/bash.go`
- Create: `pkg/builtins/bash_test.go`, `pkg/builtins/bash_unix_test.go`

**Interfaces:**
- Consumes:
  - `tools.BuiltinToolCtx`, `tools.ConfigureProcessGroup`, `tools.NewLimitedBuffer`, `tools.OutputLimit`, `Definition.UsesCommandTimeout` (Task 3).
  - `sandbox.FromContext`, `(*Sandbox).CheckCommand`, `(*Sandbox).Config`, `sandbox.BashUnrestricted` (Task 2).
  - `sandboxCtx`, `realTempDir` (Task 4 test helpers).
- Produces:
  - `const RunCommandToolName = "run_command"`
  - `func RunCommandDescription(cfg sandbox.Config) string`
  - `func commandTimeout(input map[string]any, maxTimeout time.Duration) time.Duration` (unexported; tested)

- [ ] **Step 1: Write the failing tests**

`pkg/builtins/bash_test.go`:

```go
package builtins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/sandbox"
	"github.com/teabranch/abbyfile/pkg/tools"
)

func runCmd(t *testing.T, cfg sandbox.Config, input map[string]any) (string, error) {
	t.Helper()
	return handleRunCommand(sandboxCtx(t, realTempDir(t), cfg), input)
}

func TestRunCommand_Allowed(t *testing.T) {
	out, err := runCmd(t, sandbox.Config{AllowCommands: []string{"echo *"}}, map[string]any{"command": "echo hello"})
	if err != nil || strings.TrimSpace(out) != "hello" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

// Review Focus #2.
func TestRunCommand_EmptyAllowlistRefused(t *testing.T) {
	_, err := runCmd(t, sandbox.Config{}, map[string]any{"command": "echo hello"})
	if err == nil || !strings.Contains(err.Error(), "allow_commands is empty") || !strings.Contains(err.Error(), "config set sandbox.allow_commands") {
		t.Fatalf("err = %v, want an actionable refusal", err)
	}
}

func TestRunCommand_NotAllowed(t *testing.T) {
	_, err := runCmd(t, sandbox.Config{AllowCommands: []string{"echo *"}}, map[string]any{"command": "ls"})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunCommand_NoShell(t *testing.T) {
	root := realTempDir(t)
	ctx := sandboxCtx(t, root, sandbox.Config{AllowCommands: []string{"echo *"}})
	if _, err := handleRunCommand(ctx, map[string]any{"command": "echo hi > " + filepath.Join(root, "f")}); err == nil || !strings.Contains(err.Error(), "no shell") {
		t.Fatalf("redirect err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "f")); !os.IsNotExist(err) {
		t.Fatal("redirect target must not be created")
	}
	out, err := handleRunCommand(ctx, map[string]any{"command": "echo $HOME"})
	if err != nil || strings.TrimSpace(out) != "$HOME" {
		t.Fatalf("no expansion expected, got %q, %v", out, err)
	}
}

func TestRunCommand_Unrestricted(t *testing.T) {
	out, err := runCmd(t, sandbox.Config{Bash: sandbox.BashUnrestricted}, map[string]any{"command": "echo a | tr a b"})
	if err != nil || strings.TrimSpace(out) != "b" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestRunCommand_TimeoutClamped(t *testing.T) {
	start := time.Now()
	_, err := runCmd(t, sandbox.Config{AllowCommands: []string{"sleep *"}, MaxCommandTimeout: 200 * time.Millisecond},
		map[string]any{"command": "sleep 30", "timeout": float64(30)})
	if err == nil || !strings.Contains(err.Error(), "timed out after 200ms") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("clamp not applied")
	}
}

func TestCommandTimeout(t *testing.T) {
	max := 120 * time.Second
	tests := []struct {
		in   map[string]any
		max  time.Duration
		want time.Duration
	}{
		{map[string]any{}, max, 30 * time.Second},
		{map[string]any{"timeout": float64(10)}, max, 10 * time.Second},
		{map[string]any{"timeout": float64(500)}, max, max},
		{map[string]any{"timeout": float64(0)}, max, 30 * time.Second},
		{map[string]any{"timeout": float64(-1)}, max, 30 * time.Second},
		{map[string]any{}, 5 * time.Second, 5 * time.Second},
	}
	for _, tt := range tests {
		if got := commandTimeout(tt.in, tt.max); got != tt.want {
			t.Errorf("commandTimeout(%v, %s) = %s, want %s", tt.in, tt.max, got, tt.want)
		}
	}
}

func TestRunCommand_BoundedOutput(t *testing.T) {
	ctx := tools.WithOutputLimit(sandboxCtx(t, realTempDir(t), sandbox.Config{AllowCommands: []string{"head *"}}), 100)
	out, err := handleRunCommand(ctx, map[string]any{"command": "head -c 5000 /dev/zero"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "[output truncated at 100 bytes]") || len(out) > 140 {
		t.Fatalf("len=%d", len(out))
	}
}

func TestRunCommandDescription(t *testing.T) {
	if d := RunCommandDescription(sandbox.Config{Bash: sandbox.BashUnrestricted}); !strings.Contains(d, "sh -c") {
		t.Errorf("unrestricted: %q", d)
	}
	if d := RunCommandDescription(sandbox.Default()); !strings.Contains(d, "every call is refused") {
		t.Errorf("empty: %q", d)
	}
	d := RunCommandDescription(sandbox.Config{AllowCommands: []string{"go test *", "git status"}})
	if !strings.Contains(d, "`go test *`") || !strings.Contains(d, "`git status`") || !strings.Contains(d, "without a shell") {
		t.Errorf("restricted: %q", d)
	}
}

func TestRunCommandTool_Definition(t *testing.T) {
	def := RunCommandTool()
	if def.Name != RunCommandToolName || !def.UsesCommandTimeout || def.HandlerCtx == nil {
		t.Fatalf("def = %+v", def)
	}
	if strings.Contains(def.Description, "sh -c") {
		t.Errorf("default description must describe restricted mode: %q", def.Description)
	}
}
```

`bash_test.go` does not import `"context"`. Only `bash_unix_test.go` needs it.

`pkg/builtins/bash_unix_test.go`:

```go
//go:build unix

package builtins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

// Review Focus #5.
func TestRunCommand_CancelKillsGroup(t *testing.T) {
	root := realTempDir(t)
	pidFile := filepath.Join(root, "pid")
	base := sandboxCtx(t, root, sandbox.Config{Bash: sandbox.BashUnrestricted})
	ctx, cancel := context.WithCancel(base)
	done := make(chan error, 1)
	go func() {
		_, err := handleRunCommand(ctx, map[string]any{"command": "sleep 60 & echo $! > " + pidFile + "; wait"})
		done <- err
	}()
	var pid int
	for i := 0; i < 100 && pid == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
	}
	if pid == 0 {
		t.Fatal("pid never written")
	}
	cancel()
	<-done
	for i := 0; i < 60; i++ {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("grandchild %d survived cancel", pid)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/builtins/ -run 'RunCommand|CommandTimeout' -v`
Expected: FAIL to build with `undefined: RunCommandDescription`, `commandTimeout`, and `RunCommandToolName`.

- [ ] **Step 3: Rewrite `bash.go`**

Replace the whole of `pkg/builtins/bash.go`:

```go
package builtins

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/teabranch/abbyfile/pkg/sandbox"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// RunCommandToolName is the MCP name of the Bash builtin.
const RunCommandToolName = "run_command"

// defaultCommandTimeout applies when the model does not request a timeout.
const defaultCommandTimeout = 30 * time.Second

// RunCommandTool returns the run_command definition. Its description
// assumes the default sandbox; the agent rewrites it at registration with
// RunCommandDescription for the effective sandbox.
func RunCommandTool() *tools.Definition {
	def := tools.BuiltinToolCtx(
		RunCommandToolName,
		RunCommandDescription(sandbox.Default()),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The command to run",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Timeout in seconds (default: 30; capped by the agent's sandbox.max_command_timeout)",
				},
			},
			"required": []string{"command"},
		},
		handleRunCommand,
	).WithAnnotations(&tools.Annotations{
		DestructiveHint: tools.BoolPtr(true),
		OpenWorldHint:   tools.BoolPtr(true),
		Title:           "Run Command",
	})
	def.UsesCommandTimeout = true
	return def
}

// RunCommandDescription describes run_command for the given sandbox mode so
// the model knows what it may run before trying.
func RunCommandDescription(cfg sandbox.Config) string {
	n := cfg.Normalize()
	if n.Bash == sandbox.BashUnrestricted {
		return "Execute a shell command via sh -c and return its combined output. Runs with the permissions of the current user."
	}
	if len(n.AllowCommands) == 0 {
		return "Run an allowlisted command. No commands are allowlisted for this agent, so every call is refused."
	}
	quoted := make([]string, len(n.AllowCommands))
	for i, c := range n.AllowCommands {
		quoted[i] = "`" + c + "`"
	}
	return fmt.Sprintf("Run an allowlisted command without a shell and return its combined output. Allowed: %s (* = any arguments). Pipes, redirects, chaining and substitution are not supported; quote arguments with ' or \".",
		strings.Join(quoted, ", "))
}

// commandTimeout is min(requested seconds or 30s, maxTimeout).
func commandTimeout(input map[string]any, maxTimeout time.Duration) time.Duration {
	timeout := defaultCommandTimeout
	if t, ok := input["timeout"].(float64); ok && t > 0 {
		timeout = time.Duration(t * float64(time.Second))
	}
	return min(timeout, maxTimeout)
}

func handleRunCommand(ctx context.Context, input map[string]any) (string, error) {
	command, ok := input["command"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: command")
	}
	sb := sandbox.FromContext(ctx)
	cfg := sb.Config()

	var argv []string
	if cfg.Bash == sandbox.BashUnrestricted {
		argv = []string{"sh", "-c", command}
	} else {
		var err error
		if argv, err = sb.CheckCommand(command); err != nil {
			return "", err
		}
	}

	timeout := commandTimeout(input, cfg.MaxCommandTimeout)
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = sb.Cwd()
	tools.ConfigureProcessGroup(cmd)
	out := tools.NewLimitedBuffer(tools.OutputLimit(ctx))
	cmd.Stdout = out
	cmd.Stderr = out

	err := cmd.Run()
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("command timed out after %s (sandbox.max_command_timeout is %s)\noutput: %s", timeout, cfg.MaxCommandTimeout, out.String())
	}
	if err != nil {
		return "", fmt.Errorf("command failed: %w\noutput: %s", err, out.String())
	}
	return out.String(), nil
}
```

`cmd.Dir = sb.Cwd()` makes the command run in the same directory that relative file-tool paths resolve against. In production this is the process working directory. In tests it is the fixture root.

- [ ] **Step 4: Run the builtins tests with coverage**

Run: `go test ./pkg/builtins/ -cover -v`
Expected: PASS, coverage ≥ 80%.

- [ ] **Step 5: Run the whole module**

Run: `go build ./... && go test ./...`
Expected: PASS. If any test outside `pkg/builtins` depended on the old `run_command` description or on `DefaultCommandPolicy().DeniedSubstrings`, update that assertion to the new behavior and note it in the commit body. `pkg/mcp/testdata/tools_list.golden.json` contains no builtins, so it does not change.

- [ ] **Step 6: Commit**

```bash
git add pkg/builtins/
```
```bash
git commit -m "feat: run_command allowlist without a shell, timeout clamp, process-group kill, bounded output

Replaces the bypassable substring denylist (D4). Restricted mode is the
default; sandbox.bash: unrestricted restores sh -c.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 6: `sandbox:` frontmatter → generated code, and `abby build` notes

**Files:**
- Create: `pkg/definition/sandbox.go`, `pkg/definition/sandbox_test.go`
- Modify: `pkg/definition/agent.go` (`AgentDef.Sandbox`, `frontmatter2.Sandbox`, `abbyfileBlock.Sandbox`, both parse functions)
- Modify: `pkg/builder/builder.go` (`templateData.Sandbox`, `sandboxData`, `buildSandboxData`, `GenerateSource`)
- Modify: `pkg/builder/templates/main.go.tmpl`
- Create: `pkg/builder/notes.go`, `pkg/builder/notes_test.go`
- Modify: `pkg/builder/builder_test.go`
- Modify: `cmd/abby/build.go`

**Interfaces:**
- Consumes: `sandbox.Config`, `Validate`, `Normalize`, `BashMode` (Task 2); `builtins.NameBash` (existing).
- Produces:
  - `type SandboxDef struct { AllowedDirs []string; Bash string; AllowCommands []string; MaxCommandTimeout string }` with YAML keys `allowed_dirs`, `bash`, `allow_commands`, `max_command_timeout`
  - `func (s *SandboxDef) ToConfig() (sandbox.Config, error)`
  - `AgentDef.Sandbox *SandboxDef`
  - `func SandboxNotes(def *definition.AgentDef) []string`
  - The generated code calls `agent.WithSandbox(sandbox.Config{...})`, defined in Task 7. Generated code is only compiled by the integration test in Task 9. Task 6's tests parse it with `go/parser` rather than compiling it.

- [ ] **Step 1: Write the failing definition tests**

`pkg/definition/sandbox_test.go`:

```go
package definition

import (
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

const dualWithSandbox = `---
name: sb
---

---
description: "d"
tools: Read, Bash
sandbox:
  allowed_dirs: [".", "/tmp/work"]
  bash: restricted
  allow_commands: ["go test *", "git status"]
  max_command_timeout: 45s
---

body
`

func TestParseAgentMD_Sandbox_Dual(t *testing.T) {
	def, err := ParseAgentMD(writeTempAgent(t, dualWithSandbox))
	if err != nil {
		t.Fatal(err)
	}
	s := def.Sandbox
	if s == nil || len(s.AllowedDirs) != 2 || s.Bash != "restricted" || len(s.AllowCommands) != 2 || s.MaxCommandTimeout != "45s" {
		t.Fatalf("Sandbox = %+v", s)
	}
	cfg, err := s.ToConfig()
	if err != nil || cfg.MaxCommandTimeout != 45*time.Second || cfg.Bash != sandbox.BashRestricted {
		t.Fatalf("ToConfig = %+v, %v", cfg, err)
	}
}

func TestParseAgentMD_Sandbox_Single(t *testing.T) {
	md := "---\nname: sb\ndescription: d\nabbyfile:\n  tools: [Bash]\n  sandbox:\n    bash: unrestricted\n---\n\nbody\n"
	def, err := ParseAgentMD(writeTempAgent(t, md))
	if err != nil || def.Sandbox == nil || def.Sandbox.Bash != "unrestricted" {
		t.Fatalf("def.Sandbox = %+v, err %v", def.Sandbox, err)
	}
}

func TestParseAgentMD_Sandbox_Omitted(t *testing.T) {
	md := strings.Replace(dualWithSandbox, "sandbox:\n  allowed_dirs: [\".\", \"/tmp/work\"]\n  bash: restricted\n  allow_commands: [\"go test *\", \"git status\"]\n  max_command_timeout: 45s\n", "", 1)
	def, err := ParseAgentMD(writeTempAgent(t, md))
	if err != nil || def.Sandbox != nil {
		t.Fatalf("omitted sandbox must be nil, got %+v, %v", def.Sandbox, err)
	}
}

func TestParseAgentMD_Sandbox_Invalid(t *testing.T) {
	cases := map[string]string{
		"bash":     "sandbox:\n  bash: yolo\n",
		"entry":    "sandbox:\n  allow_commands: [\"go test | tee x\"]\n",
		"duration": "sandbox:\n  max_command_timeout: soon\n",
		"zero dur": "sandbox:\n  max_command_timeout: 0s\n",
		"no dirs":  "sandbox:\n  allowed_dirs: []\n",
	}
	for name, block := range cases {
		md := "---\nname: sb\n---\n\n---\ndescription: d\ntools: Bash\n" + block + "---\n\nbody\n"
		if _, err := ParseAgentMD(writeTempAgent(t, md)); err == nil || !strings.Contains(err.Error(), "sandbox") {
			t.Errorf("%s: err = %v, want a sandbox error", name, err)
		}
	}
}
```

`writeTempAgent` already exists in `pkg/definition/agent_test.go`. Check this with `grep -n "func writeTempAgent" pkg/definition/*_test.go`. If it does not exist, add it to `sandbox_test.go`. It writes the content to `t.TempDir()/agent.md` and returns the path.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/definition/ -run Sandbox -v`
Expected: FAIL with `def.Sandbox undefined`.

- [ ] **Step 3: Implement the definition changes**

`pkg/definition/sandbox.go`:

```go
package definition

import (
	"fmt"
	"time"

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

// SandboxDef is the parsed sandbox: frontmatter block.
type SandboxDef struct {
	AllowedDirs       []string `yaml:"allowed_dirs"`
	Bash              string   `yaml:"bash"`
	AllowCommands     []string `yaml:"allow_commands"`
	MaxCommandTimeout string   `yaml:"max_command_timeout"` // Go duration, e.g. "120s"
}

// ToConfig validates the block and returns the normalized sandbox.Config.
func (s *SandboxDef) ToConfig() (sandbox.Config, error) {
	cfg := sandbox.Config{
		AllowedDirs:   s.AllowedDirs,
		Bash:          sandbox.BashMode(s.Bash),
		AllowCommands: s.AllowCommands,
	}
	if s.MaxCommandTimeout != "" {
		d, err := time.ParseDuration(s.MaxCommandTimeout)
		if err != nil {
			return sandbox.Config{}, fmt.Errorf("sandbox.max_command_timeout: %w", err)
		}
		if d <= 0 {
			return sandbox.Config{}, fmt.Errorf("sandbox.max_command_timeout must be positive, got %s", s.MaxCommandTimeout)
		}
		cfg.MaxCommandTimeout = d
	}
	if err := cfg.Validate(); err != nil {
		return sandbox.Config{}, err
	}
	return cfg.Normalize(), nil
}

func validateSandbox(s *SandboxDef) error {
	if s == nil {
		return nil
	}
	_, err := s.ToConfig()
	return err
}
```

In `pkg/definition/agent.go`:
- Add `Sandbox *SandboxDef` to `AgentDef`, after `ContextBudget`.
- Add ``Sandbox *SandboxDef `yaml:"sandbox"` `` to `frontmatter2` and to `abbyfileBlock`, after their `ContextBudget` fields.
- In `parseDualFormat`, after the `validateContextBudget` block, add:

```go
	if err := validateSandbox(fm2.Sandbox); err != nil {
		return nil, err
	}
	def.Sandbox = fm2.Sandbox
```

- In `parseSingleFormat`, inside `if sfm.Abbyfile != nil {`, after the context-budget block, add:

```go
		if err := validateSandbox(sfm.Abbyfile.Sandbox); err != nil {
			return nil, err
		}
		def.Sandbox = sfm.Abbyfile.Sandbox
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./pkg/definition/ -v`
Expected: PASS. The `no dirs` case depends on yaml.v3 decoding `[]` into a non-nil empty slice, which it does. If that assumption fails, add `if s.AllowedDirs != nil && len(s.AllowedDirs) == 0` handling in `ToConfig` so it returns the `Validate` error, and keep the test.

- [ ] **Step 5: Write the failing builder and notes tests**

Append to `pkg/builder/builder_test.go`:

```go
func TestGenerateSource_EmitsSandbox(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name: "s", Version: "0.0.1", Description: "d", Tools: []string{"Read", "Bash"}, PromptBody: "b",
		Sandbox: &definition.SandboxDef{AllowCommands: []string{`go test *`, `echo "a b"`}, MaxCommandTimeout: "45s"},
	}
	if err := GenerateSource(dir, def, "v0.11.0", ""); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "main.go")
	data, _ := os.ReadFile(src)
	s := string(data)
	for _, want := range []string{
		`"github.com/teabranch/abbyfile/pkg/sandbox"`,
		"agent.WithSandbox(sandbox.Config{",
		`AllowedDirs: []string{"."}`,
		`Bash: sandbox.BashMode("restricted")`,
		`"go test *"`, `"echo \"a b\""`,
		"MaxCommandTimeout: 45000000000, // 45s",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("main.go missing %q:\n%s", want, s)
		}
	}
	if _, err := parser.ParseFile(token.NewFileSet(), src, nil, parser.AllErrors); err != nil {
		t.Fatalf("generated main.go is not valid Go: %v\n---\n%s", err, data)
	}
}

func TestGenerateSource_NoSandbox_OmitsWithSandbox(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{Name: "s", Version: "0.0.1", Tools: []string{"Read"}, PromptBody: "b"}
	if err := GenerateSource(dir, def, "v0.11.0", ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if strings.Contains(string(data), "WithSandbox") || strings.Contains(string(data), "pkg/sandbox") {
		t.Fatalf("no sandbox block must emit nothing:\n%s", data)
	}
}

func TestGenerateSource_EmptyAllowCommandsIsValidGo(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name: "s", Version: "0.0.1", Tools: []string{"Bash"}, PromptBody: "b",
		Sandbox: &definition.SandboxDef{Bash: "restricted"},
	}
	if err := GenerateSource(dir, def, "v0.11.0", ""); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "main.go")
	if _, err := parser.ParseFile(token.NewFileSet(), src, nil, parser.AllErrors); err != nil {
		data, _ := os.ReadFile(src)
		t.Fatalf("invalid Go: %v\n%s", err, data)
	}
}
```

`pkg/builder/notes_test.go`:

```go
package builder

import (
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/definition"
)

// Review Focus #2.
func TestSandboxNotes(t *testing.T) {
	tests := []struct {
		name string
		def  *definition.AgentDef
		want []string // substrings, one per expected note; nil = no notes
	}{
		{"bash no sandbox", &definition.AgentDef{Name: "a", Tools: []string{"Read", "Bash"}}, []string{"run_command will refuse every call"}},
		{"bash empty allowlist", &definition.AgentDef{Name: "a", Tools: []string{"Bash"}, Sandbox: &definition.SandboxDef{}}, []string{"refuse every call"}},
		{"bash allowlisted", &definition.AgentDef{Name: "a", Tools: []string{"Bash"}, Sandbox: &definition.SandboxDef{AllowCommands: []string{"ls"}}}, nil},
		{"unrestricted", &definition.AgentDef{Name: "a", Tools: []string{"Bash"}, Sandbox: &definition.SandboxDef{Bash: "unrestricted"}}, []string{"unrestricted"}},
		{"root dir", &definition.AgentDef{Name: "a", Tools: []string{"Read"}, Sandbox: &definition.SandboxDef{AllowedDirs: []string{"/"}}}, []string{"allowed_dirs includes /"}},
		{"no bash", &definition.AgentDef{Name: "a", Tools: []string{"Read"}}, nil},
	}
	for _, tt := range tests {
		got := SandboxNotes(tt.def)
		if len(got) != len(tt.want) {
			t.Errorf("%s: notes = %q, want %d", tt.name, got, len(tt.want))
			continue
		}
		for i, sub := range tt.want {
			if !strings.Contains(got[i], sub) || !strings.Contains(got[i], `"a"`) {
				t.Errorf("%s: note %q should mention %q and the agent name", tt.name, got[i], sub)
			}
		}
	}
}
```

- [ ] **Step 6: Run to verify failure**

Run: `go test ./pkg/builder/ -run 'Sandbox' -v`
Expected: FAIL with `undefined: SandboxNotes`, and the main.go assertions fail.

- [ ] **Step 7: Implement builder, template, notes and build output**

In `pkg/builder/builder.go`:
- Add `Sandbox *sandboxData` to `templateData`, after `Budget`.
- Add:

```go
// sandboxData holds the normalized sandbox config for code generation.
// MaxCommandTimeout is emitted as an untyped nanosecond constant so the
// generated file needs no "time" import.
type sandboxData struct {
	AllowedDirs           []string
	Bash                  string
	AllowCommands         []string
	MaxCommandTimeout     int64
	MaxCommandTimeoutText string
}

// buildSandboxData validates and normalizes a frontmatter sandbox block.
// A nil block yields nil: the agent then uses sandbox.Default() at runtime.
func buildSandboxData(s *definition.SandboxDef) (*sandboxData, error) {
	if s == nil {
		return nil, nil
	}
	cfg, err := s.ToConfig()
	if err != nil {
		return nil, err
	}
	return &sandboxData{
		AllowedDirs:           cfg.AllowedDirs,
		Bash:                  string(cfg.Bash),
		AllowCommands:         cfg.AllowCommands,
		MaxCommandTimeout:     int64(cfg.MaxCommandTimeout),
		MaxCommandTimeoutText: cfg.MaxCommandTimeout.String(),
	}, nil
}
```

- In `GenerateSource`, before `data := templateData{`, add:

```go
	sbData, err := buildSandboxData(def.Sandbox)
	if err != nil {
		return fmt.Errorf("agent %q: %w", def.Name, err)
	}
```

  Then add `Sandbox: sbData,` to the `templateData` literal. If `err` is already declared in that scope, use `=` or rename it so the code compiles.

In `pkg/builder/templates/main.go.tmpl`:
- After the `{{- if or .CustomTools .Budget}} … {{- end}}` tools-import block, add:

```
{{- if .Sandbox}}
	"github.com/teabranch/abbyfile/pkg/sandbox"
{{- end}}
```

- Insert this immediately before the `{{- if .Budget}}` line in the `agent.New(` argument list:

```
		{{- if .Sandbox}}
		agent.WithSandbox(sandbox.Config{
			AllowedDirs: []string{ {{- range $i, $d := .Sandbox.AllowedDirs}}{{if $i}}, {{end}}{{printf "%q" $d}}{{end -}} },
			Bash: sandbox.BashMode({{printf "%q" .Sandbox.Bash}}),
			AllowCommands: []string{ {{- range $i, $c := .Sandbox.AllowCommands}}{{if $i}}, {{end}}{{printf "%q" $c}}{{end -}} },
			MaxCommandTimeout: {{.Sandbox.MaxCommandTimeout}}, // {{.Sandbox.MaxCommandTimeoutText}}
		}),
		{{- end}}
```

With one dir, `{{- range …}}` trims the space after `{`, so the output is `AllowedDirs: []string{"."}`, which is what the test expects. With no commands it renders `[]string{}`. If the whitespace differs, adjust the trim markers until the test string matches exactly. Do not loosen the test.

`pkg/builder/notes.go`:

```go
package builder

import (
	"fmt"
	"slices"

	"github.com/teabranch/abbyfile/pkg/builtins"
	"github.com/teabranch/abbyfile/pkg/definition"
)

// SandboxNotes returns warnings and notes about def's sandbox for
// `abby build` to print on stderr. Empty when nothing is noteworthy.
func SandboxNotes(def *definition.AgentDef) []string {
	var notes []string
	sb := def.Sandbox
	hasBash := slices.Contains(def.Tools, builtins.NameBash)
	switch {
	case sb != nil && sb.Bash == "unrestricted":
		notes = append(notes, fmt.Sprintf("warning: agent %q sets sandbox.bash: unrestricted; run_command runs any shell command with the user's permissions", def.Name))
	case hasBash && (sb == nil || len(sb.AllowCommands) == 0):
		notes = append(notes, fmt.Sprintf("note: agent %q declares Bash but sandbox.allow_commands is empty; run_command will refuse every call until commands are allowed (see docs/guides/tools.md#sandbox)", def.Name))
	}
	if sb != nil && slices.Contains(sb.AllowedDirs, "/") {
		notes = append(notes, fmt.Sprintf("warning: agent %q sandbox.allowed_dirs includes /; file tools can reach the whole filesystem", def.Name))
	}
	return notes
}
```

In `cmd/abby/build.go`, immediately before `if err := builder.BuildAll(defs, cfg); err != nil {`, add the loop below. Add `"maps"` and `"slices"` to the imports if they are missing. `os` is already imported.

```go
	for _, name := range slices.Sorted(maps.Keys(defs)) {
		for _, note := range builder.SandboxNotes(defs[name]) {
			fmt.Fprintln(os.Stderr, note)
		}
	}
```

- [ ] **Step 8: Run the tests**

Run: `go test ./pkg/definition/ ./pkg/builder/ ./cmd/... -v`
Expected: PASS. Existing builder tests are unaffected, because they declare no sandbox.

- [ ] **Step 9: Commit**

```bash
git add pkg/definition/ pkg/builder/ cmd/abby/build.go
```
```bash
git commit -m "feat: sandbox frontmatter block, generated WithSandbox, and abby build notes

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 7: Runtime config, agent wiring, describe, signals

**Files:**
- Modify: `pkg/config/config.go` (`Config.Sandbox`, `SandboxOverride`, `IsZero`), `pkg/config/loader.go` (`WriteFieldTo`, `ResetFieldTo`, `ParseList`, `ensureSandbox`), `pkg/config/config_test.go`
- Modify: `pkg/agent/options.go` (`WithSandbox`), `pkg/agent/agent.go`
- Create: `pkg/agent/agent_sandbox_test.go`
- Modify: `internal/cli/config.go` (`CompiledDefaults.Sandbox`, get/set output), `internal/cli/root.go` (`Options.Sandbox`, `SandboxManifest`), `internal/cli/serve_mcp.go`, `internal/cli/run_tool.go`
- Create: `internal/cli/config_sandbox_test.go`

**Interfaces:**
- Consumes:
  - `sandbox.Config`, `New`, `Normalize`, `ParseAllowEntry`, `(*Sandbox).Warnings/AllowedDirs/Config` (Task 2).
  - `tools.WithSandbox`, `tools.SpillDir` (Task 3).
  - `builtins.RunCommandDescription`, `builtins.RunCommandToolName` (Task 5).
- Produces:
  - `config.SandboxOverride{AllowedDirs *[]string; Bash *string; AllowCommands *[]string; MaxCommandTimeout *string}`
  - `func config.ParseList(value string) ([]string, error)`
  - `func agent.WithSandbox(cfg sandbox.Config) Option`
  - `cli.CompiledDefaults.Sandbox sandbox.Config`, `cli.Options.Sandbox *sandbox.Sandbox`
  - `cli.AgentManifest.Sandbox *cli.SandboxManifest`

- [ ] **Step 1: Write the failing config tests**

Append to `pkg/config/config_test.go` (add the `"reflect"` and `"strings"` imports if missing):

```go
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
		"":                  {},
		"a, b ,,c":          {"a", "b", "c"},
		`["x, y","z"]`:      {"x, y", "z"},
		`[]`:                {},
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/config/ -v`
Expected: FAIL with `cfg.Sandbox undefined` and `undefined: ParseList`.

- [ ] **Step 3: Implement the config changes**

In `pkg/config/config.go`:
- Add ``Sandbox *SandboxOverride `yaml:"sandbox,omitempty"` `` to `Config`.
- Add:

```go
// SandboxOverride holds optional overrides for the sandbox: block.
type SandboxOverride struct {
	AllowedDirs       *[]string `yaml:"allowed_dirs,omitempty"`
	Bash              *string   `yaml:"bash,omitempty"`
	AllowCommands     *[]string `yaml:"allow_commands,omitempty"`
	MaxCommandTimeout *string   `yaml:"max_command_timeout,omitempty"` // duration string, e.g. "120s"
}
```

- Append `&& c.Sandbox == nil` to `IsZero`.

In `pkg/config/loader.go`:
- Add imports `"encoding/json"`, `"strings"`, `"time"` and `"github.com/teabranch/abbyfile/pkg/sandbox"`.
- Add these cases before `default:` in `WriteFieldTo`:

```go
	case "sandbox.allowed_dirs":
		dirs, err := ParseList(value)
		if err != nil {
			return fmt.Errorf("sandbox.allowed_dirs: %w", err)
		}
		if len(dirs) == 0 {
			return fmt.Errorf(`sandbox.allowed_dirs needs at least one directory (use "." for the working directory)`)
		}
		ensureSandbox(cfg)
		cfg.Sandbox.AllowedDirs = &dirs
	case "sandbox.bash":
		if value != string(sandbox.BashRestricted) && value != string(sandbox.BashUnrestricted) {
			return fmt.Errorf("sandbox.bash must be restricted or unrestricted")
		}
		ensureSandbox(cfg)
		v := value
		cfg.Sandbox.Bash = &v
	case "sandbox.allow_commands":
		cmds, err := ParseList(value)
		if err != nil {
			return fmt.Errorf("sandbox.allow_commands: %w", err)
		}
		for _, c := range cmds {
			if _, err := sandbox.ParseAllowEntry(c); err != nil {
				return fmt.Errorf("sandbox.%w", err)
			}
		}
		ensureSandbox(cfg)
		cfg.Sandbox.AllowCommands = &cmds
	case "sandbox.max_command_timeout":
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return fmt.Errorf("sandbox.max_command_timeout must be a positive duration such as 120s")
		}
		ensureSandbox(cfg)
		v := value
		cfg.Sandbox.MaxCommandTimeout = &v
```

`sandbox.ParseAllowEntry` errors read `allow_commands entry …`, so the wrapped message contains `sandbox.allow_commands`, which is what the test asserts.

- Add `case "sandbox": cfg.Sandbox = nil` to `ResetFieldTo`.
- Append:

```go
// ParseList parses a config-set list value: a JSON array (use it when an
// entry contains a comma) or a comma-separated list. Blank items are
// dropped; an empty string yields an empty, non-nil list.
func ParseList(value string) ([]string, error) {
	v := strings.TrimSpace(value)
	if strings.HasPrefix(v, "[") {
		var out []string
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return nil, fmt.Errorf("invalid JSON list: %w", err)
		}
		if out == nil {
			out = []string{}
		}
		return out, nil
	}
	out := []string{}
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

// ensureSandbox lazily creates cfg.Sandbox, preserving existing fields.
func ensureSandbox(cfg *Config) {
	if cfg.Sandbox == nil {
		cfg.Sandbox = &SandboxOverride{}
	}
}
```

- Update the unsupported-field message in `WriteFieldTo` if it lists supported fields. As of this plan it does not.

Run: `go test ./pkg/config/ -cover -v`
Expected: PASS, coverage ≥ 80%.

- [ ] **Step 4: Write the failing agent and CLI tests**

`pkg/agent/agent_sandbox_test.go`:

```go
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/builtins"
	"github.com/teabranch/abbyfile/pkg/sandbox"
	"github.com/teabranch/abbyfile/pkg/tools"
)

func TestApplyConfigOverrides_Sandbox(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(cfgPath, []byte("sandbox:\n  bash: unrestricted\n  allow_commands: [\"ls\"]\n  max_command_timeout: 9s\n  allowed_dirs: [\"/tmp\"]\n"), 0o600)
	a := newTestAgent(t, WithConfigPath(cfgPath), WithSandbox(sandbox.Config{AllowCommands: []string{"go test *"}}))
	if a.sandbox.Bash != sandbox.BashUnrestricted || a.sandbox.AllowCommands[0] != "ls" ||
		a.sandbox.MaxCommandTimeout != 9*time.Second || a.sandbox.AllowedDirs[0] != "/tmp" {
		t.Fatalf("sandbox after overrides = %+v", a.sandbox)
	}
}

func TestDefaultSandboxWhenNotSet(t *testing.T) {
	a := newTestAgent(t, WithConfigPath(filepath.Join(t.TempDir(), "none.yaml")))
	if a.sandbox.Normalize().Bash != sandbox.BashRestricted || len(a.sandbox.AllowCommands) != 0 {
		t.Fatalf("default sandbox = %+v", a.sandbox)
	}
}

func TestSandboxedToolDefs_RewritesRunCommandDescription(t *testing.T) {
	a := newTestAgent(t, WithConfigPath(filepath.Join(t.TempDir(), "none.yaml")),
		WithTools(builtins.RunCommandTool()),
		WithSandbox(sandbox.Config{AllowCommands: []string{"go test *"}}))
	sb, err := a.buildSandbox()
	if err != nil {
		t.Fatal(err)
	}
	defs := a.sandboxedToolDefs(sb)
	if !strings.Contains(defs[0].Description, "`go test *`") {
		t.Errorf("description = %q", defs[0].Description)
	}
	if strings.Contains(a.toolDefs[0].Description, "go test") {
		t.Error("original definition must not be mutated")
	}
}

// Review Focus #4.
func TestAgentSandboxAllowsSpillRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a := newTestAgent(t, WithName("spiller"), WithConfigPath(filepath.Join(t.TempDir(), "none.yaml")))
	sb, err := a.buildSandbox()
	if err != nil {
		t.Fatal(err)
	}
	spill := filepath.Join(tools.SpillDir("spiller"), "run_command-abc.txt")
	if _, err := sb.Resolve(spill, sandbox.Read); err != nil {
		t.Fatalf("spill file must be readable: %v", err)
	}
	if _, err := sb.Resolve(spill, sandbox.Write); err == nil {
		t.Fatal("spill dir must be read-only")
	}
}
```

`newTestAgent` builds an agent with a name, a version and an embedded prompt FS. Check first with `grep -n "func newTestAgent\|embed.FS" pkg/agent/*_test.go`. If `pkg/agent/agent_test.go` already has a helper that constructs an agent, reuse it under that name. Otherwise add this to `agent_sandbox_test.go`. It relies on a `testdata/prompts/system.md` that the existing tests use, so check the path used there and match it.

```go
//go:embed testdata
var testPromptFS embed.FS

func newTestAgent(t *testing.T, opts ...Option) *Agent {
	t.Helper()
	base := []Option{WithName("sb-agent"), WithVersion("0.0.1"), WithPromptFS(testPromptFS, "testdata/prompts/system.md")}
	a, err := New(append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
```

`WithName` in `opts` overrides the base name, because options apply in order.

`internal/cli/config_sandbox_test.go`:

```go
package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/config"
	"github.com/teabranch/abbyfile/pkg/sandbox"
)

func TestSandboxFields(t *testing.T) {
	defaults := CompiledDefaults{Sandbox: sandbox.Config{AllowCommands: []string{"go test *"}}.Normalize()}
	bash := "unrestricted"
	cfg := &config.Config{Sandbox: &config.SandboxOverride{Bash: &bash}}
	got := map[string]string{}
	for _, f := range sandboxFields(cfg, defaults) {
		got[f.key] = f.value + " (" + f.source + ")"
	}
	want := map[string]string{
		"sandbox.allowed_dirs":        `["."] (compiled)`,
		"sandbox.bash":                "unrestricted (override)",
		"sandbox.allow_commands":      `["go test *"] (compiled)`,
		"sandbox.max_command_timeout": (2 * time.Minute).String() + " (compiled)",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestSandboxSetWarning(t *testing.T) {
	if w := sandboxSetWarning("sandbox.bash", "unrestricted"); !strings.Contains(w, "any shell command") {
		t.Errorf("bash warning = %q", w)
	}
	if w := sandboxSetWarning("sandbox.allowed_dirs", "/,."); !strings.Contains(w, "whole filesystem") {
		t.Errorf("dirs warning = %q", w)
	}
	if w := sandboxSetWarning("sandbox.bash", "restricted"); w != "" {
		t.Errorf("no warning expected, got %q", w)
	}
}

func TestPrintManifestIncludesSandbox(t *testing.T) {
	root := t.TempDir()
	sb, err := sandbox.New(sandbox.Config{Bash: sandbox.BashUnrestricted}, root)
	if err != nil {
		t.Fatal(err)
	}
	m := buildManifest(Options{Name: "a", Version: "1", Sandbox: sb})
	if m.Sandbox == nil || m.Sandbox.Bash != "unrestricted" || len(m.Sandbox.Warnings) == 0 || m.Sandbox.AllowCommands == nil {
		t.Fatalf("manifest sandbox = %+v", m.Sandbox)
	}
}
```

`TestPrintManifestIncludesSandbox` tests a new pure helper, `buildManifest(opts Options) AgentManifest`. Extract it from `printManifest` so the test doesn't need a loader or a registry. `buildManifest` must tolerate a nil `Loader` and a nil `Registry`. The test file's imports are `"strings"`, `"testing"`, `"time"`, `config` and `sandbox`.

- [ ] **Step 5: Run to verify failure**

Run: `go test ./pkg/agent/ ./internal/cli/ -v`
Expected: FAIL with `undefined: WithSandbox`, `buildSandbox`, `sandboxFields`, and `buildManifest`.

- [ ] **Step 6: Implement the agent changes**

In `pkg/agent/options.go`, add the import `"github.com/teabranch/abbyfile/pkg/sandbox"` and:

```go
// WithSandbox sets the compiled-in sandbox for built-in tools. Without it
// the agent uses sandbox.Default(): file tools confined to the working
// directory and run_command refusing every call.
func WithSandbox(cfg sandbox.Config) Option {
	return func(a *Agent) { a.sandbox = cfg }
}
```

In `pkg/agent/agent.go`:
- Add the imports `"context"`, `"os/signal"`, `"syscall"`, `"github.com/teabranch/abbyfile/pkg/builtins"` and `"github.com/teabranch/abbyfile/pkg/sandbox"`.
- Add `sandbox sandbox.Config` to `Agent`, after `budget`.
- In `New`, add `sandbox: sandbox.Default(),` to the initial literal.
- Add `Sandbox: a.sandbox.Normalize(),` to `compiledDefaults()`.
- Append this to `applyConfigOverrides`:

```go
	if cfg.Sandbox != nil {
		so := cfg.Sandbox
		if so.AllowedDirs != nil {
			a.sandbox.AllowedDirs = *so.AllowedDirs
		}
		if so.Bash != nil {
			a.sandbox.Bash = sandbox.BashMode(*so.Bash)
		}
		if so.AllowCommands != nil {
			a.sandbox.AllowCommands = *so.AllowCommands
		}
		if so.MaxCommandTimeout != nil {
			if d, err := time.ParseDuration(*so.MaxCommandTimeout); err == nil && d > 0 {
				a.sandbox.MaxCommandTimeout = d
			} else {
				a.logger.Warn("invalid sandbox.max_command_timeout in config, keeping compiled value", "value", *so.MaxCommandTimeout)
			}
		}
	}
```

- Add these methods:

```go
// buildSandbox resolves the effective sandbox against the process working
// directory, with the spill directory as a read-only root, and logs its
// warnings to stderr.
func (a *Agent) buildSandbox() (*sandbox.Sandbox, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolving working directory: %w", err)
	}
	sb, err := sandbox.New(a.sandbox, cwd, tools.SpillDir(a.name))
	if err != nil {
		return nil, err
	}
	for _, w := range sb.Warnings() {
		a.logger.Warn(w)
	}
	return sb, nil
}

// sandboxedToolDefs returns the tool definitions with run_command's
// description rewritten for the effective sandbox. Originals are not mutated.
func (a *Agent) sandboxedToolDefs(sb *sandbox.Sandbox) []*tools.Definition {
	out := make([]*tools.Definition, len(a.toolDefs))
	for i, def := range a.toolDefs {
		if def.Builtin && def.Name == builtins.RunCommandToolName {
			c := *def
			c.Description = builtins.RunCommandDescription(sb.Config())
			def = &c
		}
		out[i] = def
	}
	return out
}
```

- In `Execute`:
  1. At the very top, add:

```go
	sb, err := a.buildSandbox()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid sandbox config: %v\n", err)
		return 1
	}
```

  2. In the registration loop, change `for _, def := range a.toolDefs {` to `for _, def := range a.sandboxedToolDefs(sb) {`.
  3. Add `Sandbox: sb,` to `cliOpts`.
  4. After the `executionHook` block, add `execOpts = append(execOpts, tools.WithSandbox(sb))`.
  5. Replace `if err := cmd.Execute(); err != nil {` with:

```go
	// Ctrl-C / SIGTERM cancel in-flight tools (and their process groups)
	// instead of killing the agent and orphaning children.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cmd.ExecuteContext(ctx); err != nil {
```

- [ ] **Step 7: Implement the CLI changes**

In `internal/cli/root.go`:
- Add the import `"github.com/teabranch/abbyfile/pkg/sandbox"`.
- Add `Sandbox *sandbox.Sandbox` to `Options`.
- Add ``Sandbox *SandboxManifest `json:"sandbox,omitempty"` `` to `AgentManifest`.
- Add:

```go
// SandboxManifest reports the effective sandbox in --describe.
type SandboxManifest struct {
	AllowedDirs       []string `json:"allowedDirs"`
	Bash              string   `json:"bash"`
	AllowCommands     []string `json:"allowCommands"`
	MaxCommandTimeout string   `json:"maxCommandTimeout"`
	Warnings          []string `json:"warnings,omitempty"`
}
```

- Split `printManifest` into `buildManifest(opts Options) AgentManifest`, which holds everything up to the tools loop. Guard `if opts.Loader != nil` around the checksum and `if opts.Registry != nil` around the tools loop. Add:

```go
	if opts.Sandbox != nil {
		c := opts.Sandbox.Config()
		manifest.Sandbox = &SandboxManifest{
			AllowedDirs:       opts.Sandbox.AllowedDirs(),
			Bash:              string(c.Bash),
			AllowCommands:     append([]string{}, c.AllowCommands...),
			MaxCommandTimeout: c.MaxCommandTimeout.String(),
			Warnings:          opts.Sandbox.Warnings(),
		}
	}
```

  `printManifest` then calls `buildManifest` and marshals the result.

In `internal/cli/config.go`:
- Add the imports `"encoding/json"`, `"slices"`, `"strings"`, `"github.com/teabranch/abbyfile/pkg/sandbox"`.
- Add `Sandbox sandbox.Config` to `CompiledDefaults`.
- Add:

```go
type sandboxField struct{ key, value, source string }

// sandboxFields returns the effective sandbox.* values and their source.
func sandboxFields(cfg *config.Config, defaults CompiledDefaults) []sandboxField {
	d := defaults.Sandbox.Normalize()
	var so config.SandboxOverride
	if cfg.Sandbox != nil {
		so = *cfg.Sandbox
	}
	src := func(set bool) string {
		if set {
			return "override"
		}
		return "compiled"
	}
	list := func(v []string) string {
		b, _ := json.Marshal(append([]string{}, v...))
		return string(b)
	}
	dirs, bash, cmds, timeout := d.AllowedDirs, string(d.Bash), d.AllowCommands, d.MaxCommandTimeout.String()
	if so.AllowedDirs != nil {
		dirs = *so.AllowedDirs
	}
	if so.Bash != nil {
		bash = *so.Bash
	}
	if so.AllowCommands != nil {
		cmds = *so.AllowCommands
	}
	if so.MaxCommandTimeout != nil {
		timeout = *so.MaxCommandTimeout
	}
	return []sandboxField{
		{"sandbox.allowed_dirs", list(dirs), src(so.AllowedDirs != nil)},
		{"sandbox.bash", bash, src(so.Bash != nil)},
		{"sandbox.allow_commands", list(cmds), src(so.AllowCommands != nil)},
		{"sandbox.max_command_timeout", timeout, src(so.MaxCommandTimeout != nil)},
	}
}

// sandboxSetWarning returns a warning for risky sandbox values, or "".
func sandboxSetWarning(field, value string) string {
	switch field {
	case "sandbox.bash":
		if value == string(sandbox.BashUnrestricted) {
			return "run_command will run any shell command with your user's permissions"
		}
	case "sandbox.allowed_dirs":
		if dirs, err := config.ParseList(value); err == nil && slices.Contains(dirs, "/") {
			return "allowed_dirs includes / — file tools can reach the whole filesystem"
		}
	}
	return ""
}
```

- In `printField`, before `default:`, add:

```go
	case "sandbox.allowed_dirs", "sandbox.bash", "sandbox.allow_commands", "sandbox.max_command_timeout":
		for _, f := range sandboxFields(cfg, defaults) {
			if f.key == field {
				fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n", f.value, f.source)
			}
		}
```

  Also append `, sandbox.allowed_dirs, sandbox.bash, sandbox.allow_commands, sandbox.max_command_timeout` to the supported list in the `default:` error.
- In `printAllFields`, before `// memory_limits`, add:

```go
	for _, f := range sandboxFields(cfg, defaults) {
		fmt.Fprintf(w, "%s: %s (%s)\n", f.key, f.value, f.source)
	}
```

- In `newConfigSetCommand`, after the existing success `Fprintf`, add:

```go
			if strings.HasPrefix(field, "sandbox.") {
				if w := sandboxSetWarning(field, value); w != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), "warning: "+w)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Restart the runtime session (e.g. Claude Code) so running agents pick this up.")
			}
```

In `internal/cli/run_tool.go`, change `executor.Run(context.Background(), def, input)` to `executor.Run(cmd.Context(), def, input)`. Drop the `context` import if it is now unused.

In `internal/cli/serve_mcp.go`, replace `return bridge.Serve(cmd.Context())` with:

```go
			err := bridge.Serve(cmd.Context())
			if errors.Is(err, context.Canceled) {
				return nil // SIGINT/SIGTERM: clean shutdown
			}
			return err
```

Add the imports `"context"` and `"errors"`.

- [ ] **Step 8: Run the tests**

Run: `go build ./... && go test ./pkg/agent/ ./internal/cli/ ./pkg/config/ -cover -v`
Expected: PASS.

- [ ] **Step 9: Run the whole module**

Run: `go test ./...`
Expected: PASS. `TestAgentSandboxAllowsSpillRead` covers Review Focus #4. If an existing `internal/cli` config test asserts the exact full output of `printAllFields`, add the four `sandbox.*` lines to its expectation.

- [ ] **Step 10: Commit**

```bash
git add pkg/config/ pkg/agent/ internal/cli/
```
```bash
git commit -m "feat: runtime sandbox overrides, agent sandbox wiring, --describe sandbox, signal-aware execution

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 8: Memory resource-template keys (B6)

**Files:**
- Modify: `pkg/mcp/bridge_test.go`

**Interfaces:**
- Consumes: the existing `startBridgeWithConfig`, `newTestLoader`, `memory.NewFileStoreAt`, `memory.NewManager`.
- Produces: nothing. This task only adds tests.

`memory.Manager.Get` → `FileStore.Read` → `validateKey` already rejects empty keys, `.`, `..`, `/` and `\` (`pkg/memory/store.go:70`). The spec says to "confirm whether `Get` already does and test either way". It does, so this task adds bridge-level tests and no code.

- [ ] **Step 1: Write the tests**

Append to `pkg/mcp/bridge_test.go`:

```go
func TestBridgeMemoryTemplateRejectsTraversalKeys(t *testing.T) {
	parent := t.TempDir()
	storeDir := filepath.Join(parent, "store")
	if err := os.WriteFile(filepath.Join(parent, "secret"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := memory.NewFileStoreAt(storeDir, memory.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	session, _ := startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name: "test-agent", Version: "v0.1.0", Registry: tools.NewRegistry(),
		Executor: tools.NewExecutor(30*time.Second, nil), Loader: newTestLoader(t),
		Memory: memory.NewManager(store),
	})
	for _, uri := range []string{
		"memory://test-agent/../secret",
		"memory://test-agent/a/b",
		"memory://test-agent/%2e%2e%2fsecret",
		"memory://test-agent/..",
	} {
		res, err := session.ReadResource(context.Background(), &gomcp.ReadResourceParams{URI: uri})
		if err == nil {
			t.Errorf("ReadResource(%q) succeeded: %+v", uri, res)
			continue
		}
		if res != nil {
			for _, c := range res.Contents {
				if strings.Contains(c.Text, "SECRET") {
					t.Fatalf("ReadResource(%q) leaked a file outside the store", uri)
				}
			}
		}
	}
}
```

Add the `"os"` and `"path/filepath"` imports if they are missing.

- [ ] **Step 2: Run it**

Run: `go test ./pkg/mcp/ -run TemplateRejectsTraversal -v`
Expected: PASS without code changes.

If a URI does not match the template, the SDK may reject it before the handler runs. That still counts as a pass, because the test only requires an error and no leaked content. If a case unexpectedly **succeeds**, stop. That is a real bug. Fix it in `pkg/mcp/bridge.go` by rejecting keys containing `/`, `\`, or equal to `.`/`..` before `mgr.Get`, and report it.

- [ ] **Step 3: Commit**

```bash
git add pkg/mcp/bridge_test.go
```
```bash
git commit -m "test: lock memory resource-template key validation against traversal (B6)

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 9: End-to-end through `abby build`, examples, docs

**Files:**
- Create: `internal/integration/sandbox_test.go`
- Modify: `internal/integration/agent_test.go` (`TestRunTool` working directory)
- Modify: `examples/basic/agents/my-agent.md`, `examples/multi-agent/agents/golang-pro.md`
- Modify: `docs/guides/tools.md`, `docs/reference.md`

**Interfaces:**
- Consumes: everything above; `abbyBin` and `findProjectRoot` from `internal/integration/agent_test.go` (build tag `integration`).
- Produces: nothing new.

- [ ] **Step 1: Fix `TestRunTool`**

`TestRunTool` in `internal/integration/agent_test.go` reads a file in `t.TempDir()` while the agent runs in the test package directory. Under confinement that is outside `allowed_dirs`. Add this helper next to `runAgentStdout`:

```go
// runAgentStdoutIn runs the agent with working directory dir.
func runAgentStdoutIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("agent %v failed: %v\nstderr: %s", args, err, string(ee.Stderr))
		}
		t.Fatalf("agent %v failed: %v", args, err)
	}
	return string(out)
}
```

In `TestRunTool`, replace the `runAgentStdout(t, "run-tool", …)` call with `runAgentStdoutIn(t, filepath.Dir(tmpFile), "run-tool", …)`.

- [ ] **Step 2: Write the end-to-end test**

`internal/integration/sandbox_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func buildSandboxAgent(t *testing.T) string {
	t.Helper()
	projectRoot := findProjectRoot()
	tmp := t.TempDir()
	agentMD := `---
name: sandbox-agent
---

---
description: "Sandbox integration agent"
tools: Read, Bash
sandbox:
  allow_commands: ["echo *"]
  max_command_timeout: 5s
---

You test the sandbox.
`
	os.MkdirAll(filepath.Join(tmp, "agents"), 0o755)
	os.WriteFile(filepath.Join(tmp, "agents", "sandbox-agent.md"), []byte(agentMD), 0o644)
	os.WriteFile(filepath.Join(tmp, "Abbyfile"), []byte("version: \"1\"\nagents:\n  sandbox-agent:\n    path: agents/sandbox-agent.md\n    version: 0.1.0\n"), 0o644)
	buildDir := filepath.Join(tmp, "build")
	cmd := exec.Command(abbyBin, "build", "-f", filepath.Join(tmp, "Abbyfile"), "-o", buildDir, "--module-dir", projectRoot)
	cmd.Dir = projectRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("abby build: %v\n%s", err, out)
	}
	return filepath.Join(buildDir, "sandbox-agent")
}

func runIn(t *testing.T, bin, dir string, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func TestSandboxEndToEnd(t *testing.T) {
	bin := buildSandboxAgent(t)
	work := t.TempDir()
	os.WriteFile(filepath.Join(work, "note.txt"), []byte("inside note"), 0o644)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("SECRET"), 0o644)

	t.Run("allowed command runs", func(t *testing.T) {
		out, stderr, err := runIn(t, bin, work, "run-tool", "run_command", "--input", `{"command":"echo sandboxed"}`)
		if err != nil || !strings.Contains(out, "sandboxed") {
			t.Fatalf("out=%q err=%v stderr=%s", out, err, stderr)
		}
	})
	t.Run("unlisted command refused", func(t *testing.T) {
		_, stderr, err := runIn(t, bin, work, "run-tool", "run_command", "--input", `{"command":"ls"}`)
		if err == nil || !strings.Contains(stderr, "not allowed") {
			t.Fatalf("err=%v stderr=%s", err, stderr)
		}
	})
	t.Run("chaining refused", func(t *testing.T) {
		_, stderr, err := runIn(t, bin, work, "run-tool", "run_command", "--input", `{"command":"echo a; echo b"}`)
		if err == nil || !strings.Contains(stderr, "no shell") {
			t.Fatalf("err=%v stderr=%s", err, stderr)
		}
	})
	t.Run("read inside works", func(t *testing.T) {
		out, stderr, err := runIn(t, bin, work, "run-tool", "read_file", "--input", `{"path":"note.txt"}`)
		if err != nil || !strings.Contains(out, "inside note") {
			t.Fatalf("out=%q err=%v stderr=%s", out, err, stderr)
		}
	})
	t.Run("read outside refused", func(t *testing.T) {
		out, stderr, err := runIn(t, bin, work, "run-tool", "read_file", "--input", `{"path":"`+outside+`"}`)
		if err == nil || strings.Contains(out, "SECRET") || !strings.Contains(stderr, "outside the allowed directories") {
			t.Fatalf("out=%q err=%v stderr=%s", out, err, stderr)
		}
	})
	t.Run("describe reports sandbox", func(t *testing.T) {
		out, stderr, err := runIn(t, bin, work, "--describe")
		if err != nil {
			t.Fatalf("describe: %v\n%s", err, stderr)
		}
		var m struct {
			Sandbox struct {
				Bash              string   `json:"bash"`
				AllowCommands     []string `json:"allowCommands"`
				MaxCommandTimeout string   `json:"maxCommandTimeout"`
				AllowedDirs       []string `json:"allowedDirs"`
			} `json:"sandbox"`
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		}
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatalf("parse: %v\n%s", err, out)
		}
		if m.Sandbox.Bash != "restricted" || len(m.Sandbox.AllowCommands) != 1 || m.Sandbox.AllowCommands[0] != "echo *" || m.Sandbox.MaxCommandTimeout != "5s" {
			t.Fatalf("sandbox = %+v", m.Sandbox)
		}
		for _, tool := range m.Tools {
			if tool.Name == "run_command" && !strings.Contains(tool.Description, "`echo *`") {
				t.Errorf("run_command description not rewritten: %q", tool.Description)
			}
		}
	})
}
```

- [ ] **Step 3: Run the integration tests**

Run: `go test -tags integration ./internal/integration/ -v -timeout 600s`
Expected: PASS, including `TestRunTool` and `TestSandboxEndToEnd`. If `abby build` fails because the generated `go.mod` pins a published version that lacks `pkg/sandbox`, check that the test passes `--module-dir`, as above. The published pin only matters at release.

- [ ] **Step 4: Update the examples**

In `examples/basic/agents/my-agent.md` and `examples/multi-agent/agents/golang-pro.md`, add a `sandbox:` block to the **second** frontmatter block, directly after the `tools:` line:

```yaml
sandbox:
  allow_commands: ["go build *", "go test *", "go vet *", "gofmt *"]
```

Then run `go run ./cmd/abby build -f examples/multi-agent/Abbyfile -o /tmp/abby-examples-check --module-dir .`. Expected: the build succeeds and prints no `note:` line for the edited agents. Delete `/tmp/abby-examples-check` afterwards.

- [ ] **Step 5: Write the docs**

In `docs/guides/tools.md`, add this section after `## Builtin Tools` and before `## Annotations`. It must carry the anchor `sandbox`, which `SandboxNotes` links to.

````markdown
## Sandbox

Built-in tools are confined by a `sandbox:` block in agent frontmatter. Without the block you get the defaults shown here:

```yaml
sandbox:
  allowed_dirs: ["."]          # file tools stay inside these; "." = the working directory the runtime starts the server in
  bash: restricted             # restricted (default) | unrestricted
  allow_commands: []           # run_command refuses every call until you list commands
  max_command_timeout: 120s    # hard cap on the timeout the model may request
```

**File tools** (`read_file`, `write_file`, `edit_file`, `glob_files`, `grep_search`):
- Every path is resolved (relative paths against the working directory, symlinks evaluated) and must land inside an `allowed_dirs` entry.
- `glob_files` and `grep_search` skip entries that point outside, and report how many they skipped.
- The agent's spill directory (`~/.abbyfile/<name>/spill/`) is readable but not writable, so `on_overflow: spill` pointers still work.
- `allowed_dirs: ["/"]` opts out. It is warned about at startup and in `--describe`.

**`run_command`, restricted mode:**
- There is **no shell**. The command is split into words, with `'` and `"` quoting honoured, and run directly.
- Unquoted `;` `&` `|` `` ` `` `$(` `>` `<` and newlines are rejected, not filtered. Pipes, redirects and chaining need `bash: unrestricted`.
- `allow_commands` entries are words:
  - `go test` allows exactly `go test`.
  - `go test *` allows `go test` with any arguments.
  - `*` is only valid as the last word.
- The effective timeout is `min(requested or 30s, max_command_timeout)`.
- Subprocesses run in their own process group and are killed as a group on timeout or cancellation.
- Output is capped in memory at 10 MB.

**`run_command`, unrestricted mode:** the old `sh -c` behaviour. `abby build` prints a warning and `--describe` reports it.

**What the sandbox does not do:**
- It does not confine `run_command` *arguments*. `cat *` can read any file. `git *`, `find *`, `env *`, `xargs *` and `make *` (with `write_file`) can run arbitrary programs. Allow the narrowest commands you can.
- It is not an OS sandbox (no seatbelt or landlock).
- It checks each path when the call is made, so it does not defend against a local process racing to swap symlinks.

**Changing it after install** (restart the runtime session afterwards):

```bash
my-agent config set sandbox.allow_commands '["go test *","make *"]'   # JSON array, or: "go test *,make *"
my-agent config set sandbox.allowed_dirs ".,../shared"
my-agent config set sandbox.bash unrestricted                       # prints a warning
my-agent config set sandbox.max_command_timeout 300s
my-agent config reset sandbox
```

### Migrating from v0.10

1. `tools: Bash` now needs `sandbox.allow_commands`, or `sandbox.bash: unrestricted`. Until then `run_command` refuses every call and `abby build` prints a note.
2. File tools are confined to the server's working directory unless `sandbox.allowed_dirs` says otherwise.
3. Restricted `run_command` no longer uses a shell. Pipes, redirects and chaining need `sandbox.bash: unrestricted`.
4. `write_file` and `edit_file` are now annotated destructive.
5. Library users: builtins use `tools.Definition.HandlerCtx`. `Handler` still works, but it runs under the default sandbox with no deadline. `tools.DefaultCommandPolicy()` no longer has a denylist.
````

In `docs/reference.md`:
- Add a `### WithSandbox(cfg sandbox.Config) Option` entry after `### WithLogger`. Its one-paragraph text is the `WithSandbox` doc comment from Task 7.
- Under `### config`, add the four `sandbox.*` keys and `config reset sandbox` to the list of supported fields, in the same format as the existing `context_budget.*` rows.
- Under `## --describe JSON Schema`, add the `sandbox` object with the fields `allowedDirs`, `bash`, `allowCommands`, `maxCommandTimeout`, `warnings`.
- In `## tools.Definition`, add the `HandlerCtx` and `UsesCommandTimeout` rows.
- Add a `## tools.BuiltinToolCtx` entry.

Then check that no builtin or run_command claim in the docs is stale:

Run: `grep -rn "sh -c\|DeniedSubstrings\|absolute path\|rm -rf" docs README.md index.md | grep -v docs/superpowers`
Expected: only the intentional mentions of the unrestricted mode, and the `command_policy` "not a security boundary" note if you add one. Fix anything that still describes the old behaviour.

- [ ] **Step 6: Refresh the benchmark numbers**

`benchmarks/bench_test.go` measures the real builtins with `builtins.All()` and `builtins.ForNames`. The descriptions of `read_file`, `write_file`, `edit_file`, `glob_files`, `grep_search` and `run_command` changed, so the published token figures drift.

Run: `make bench-report`, then compare its per-tool and handshake numbers with `docs/guides/benchmarks.md`. Update every figure that changed, including the "~1958 tokens" eager-agent figure if it moved, and add one line noting that v0.11.0 sandbox descriptions changed the builtin schema cost by N tokens. If a benchmark test asserts a threshold that the new descriptions break, shorten the description rather than raising the threshold, unless the gain is only a few tokens. Record that decision in the commit body.

- [ ] **Step 7: Full verification**

Run: `go build ./... && go vet ./... && go test ./... -cover && go test -tags integration ./internal/integration/ -timeout 600s`
Expected: all PASS. Coverage ≥ 80% on `pkg/sandbox`, `pkg/tools`, `pkg/builtins`, `pkg/config`.

- [ ] **Step 8: Commit**

```bash
git add internal/integration/ examples/ docs/guides/tools.md docs/reference.md docs/guides/benchmarks.md
```
```bash
git commit -m "docs: sandbox guide, migration notes and reference; end-to-end sandbox test; sandboxed examples

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Self-review record

- **Spec coverage:**
  - B1: Tasks 2 and 4.
  - B2: Tasks 1, 2 and 5, plus the build warning in Task 6 and the describe output in Task 7.
  - B3: `HandlerCtx` and the timeout rule in Tasks 3 and 5. Process groups for CLI tools and `run_command` in Tasks 3 and 5. The C4 runtime-timeout part is Phase C.
  - B4: Task 3 (CLI) and Task 5 (`run_command`), with the default 10 MB cap even without a policy.
  - B5: Task 4.
  - B6: Task 8.
  - Frontmatter plus `config set sandbox.<key>`: Tasks 6 and 7.
  - Resolving `.` and the `/`/`$HOME` warnings: Task 2 (the `doctor` part is Phase C).
  - The breaking change for `tools: Bash` without `sandbox:`: Tasks 5, 6 and 9.
  - Migration items 1, 2 and 7: Task 9.
- **Type consistency:**
  - `sandbox.Config` fields are `AllowedDirs`/`Bash`/`AllowCommands`/`MaxCommandTimeout` everywhere.
  - `RunCommandToolName`, `RunCommandDescription`, `SpillDir`, `WithSandbox` (both `tools.` and `agent.`), `ParseList`, `SandboxNotes` and `buildManifest` are each defined once and used by those names.
- **Known judgment calls for the reviewer:**
  - The warn-and-continue behaviour for missing `allowed_dirs`.
  - The `MaxOutputBytes: 0` ⇒ default reinterpretation.
  - Logging sandbox warnings on every subcommand, not only `serve-mcp`, because it is simplest and they go to stderr.
