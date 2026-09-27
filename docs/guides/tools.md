---
title: Tools
parent: Guides
nav_order: 2
---

# Tools Guide

Tools are the actions an agent can perform. Abbyfile supports two kinds: **CLI tools** that wrap external commands, and **builtin tools** that run Go functions in-process.

## CLI Tools

`tools.CLI()` wraps any command-line binary as a tool:

```go
tools.CLI("date", "date", "Get the current date and time")
//        ^name   ^command  ^description
```

When Claude Code calls this tool through MCP, the agent binary runs the command as a subprocess. Arguments are passed via the `args` field in the tool's input schema.

The generated input schema for CLI tools is:

```json
{
  "type": "object",
  "properties": {
    "args": {
      "type": "string",
      "description": "Command-line arguments to pass to the tool"
    }
  }
}
```

The `args` string is split on whitespace and appended to any default arguments. You can set default arguments on the definition:

```go
def := tools.CLI("lint", "golangci-lint", "Run Go linter")
def.Args = []string{"run", "--fast"}
```

If Claude passes `{"args": "--fix"}`, the final command becomes `golangci-lint run --fast --fix`.

### Minimal example:

```go
agent.WithTools(
    tools.CLI("date", "date", "Get the current date and time"),
),
```

This is the simplest possible tool — it wraps the `date` command with no default arguments.

## Builtin Tools

`tools.BuiltinTool()` creates a tool backed by a Go function:

```go
tools.BuiltinTool(name, description, schema, handler)
```

The handler signature is:

```go
func(input map[string]any) (string, error)
```

- `input` is the parsed JSON input from the MCP call
- Return a string result on success, or an error
- The schema is a `map[string]any` matching JSON Schema format

### Example: `read_file` builtin tool

```go
func readFileTool() *tools.Definition {
    return tools.BuiltinTool(
        "read_file",
        "Read the contents of a file. Returns the file content as text.",
        map[string]any{
            "type": "object",
            "properties": map[string]any{
                "path": map[string]any{
                    "type":        "string",
                    "description": "Path to the file to read (relative to project root)",
                },
            },
            "required": []string{"path"},
        },
        func(input map[string]any) (string, error) {
            path, ok := input["path"].(string)
            if !ok || path == "" {
                return "", fmt.Errorf("missing required parameter: path")
            }
            clean := filepath.Clean(path)
            if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
                return "", fmt.Errorf("path must be relative and within the project")
            }
            data, err := os.ReadFile(clean)
            if err != nil {
                return "", fmt.Errorf("reading %s: %w", clean, err)
            }
            return string(data), nil
        },
    ).WithAnnotations(&tools.Annotations{
        ReadOnlyHint:   true,
        IdempotentHint: true,
        OpenWorldHint:  tools.BoolPtr(false),
        Title:          "Read File",
    })
}
```

This example uses the plain `Handler` for brevity, with its own hand-rolled path check. The shipped `read_file` builtin instead uses `tools.BuiltinToolCtx` and `HandlerCtx`, confining paths through the sandbox described in [Sandbox](#sandbox) rather than a local `..`/absolute-path check.

### Example: `go_test` builtin tool

```go
func goTestTool() *tools.Definition {
    return tools.BuiltinTool(
        "go_test",
        "Run Go tests for a package. Returns test output including pass/fail status.",
        map[string]any{
            "type": "object",
            "properties": map[string]any{
                "package": map[string]any{
                    "type":        "string",
                    "description": "Go package pattern to test (e.g. ./pkg/tools/..., ./...)",
                    "default":     "./...",
                },
            },
        },
        func(input map[string]any) (string, error) {
            pkg := "./..."
            if p, ok := input["package"].(string); ok && p != "" {
                pkg = p
            }
            cmd := exec.Command("go", "test", "-race", "-count=1", pkg)
            out, err := cmd.CombinedOutput()
            if err != nil {
                return fmt.Sprintf("FAIL\n%s", string(out)), nil
            }
            return string(out), nil
        },
    ).WithAnnotations(&tools.Annotations{
        DestructiveHint: tools.BoolPtr(false),
        IdempotentHint:  true,
        OpenWorldHint:   tools.BoolPtr(false),
        Title:           "Run Go Tests",
    })
}
```

Note that `go_test` returns test failures as successful tool results (not errors). This lets Claude Code see the test output and reason about it. Reserve errors for infrastructure failures, not expected negative results.

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
- `glob_files` refuses any pattern containing a `..` path element outright (`pattern must not contain ".."` — use the `path` argument to choose the base directory instead). A plain, non-`**` pattern drops hits that escape the sandbox (via a symlinked directory) silently; only the `**` walk and `grep_search` report how many entries they skipped, appending "(N entries outside the allowed directories were skipped)". `grep_search` opens the symlink-resolved path, not the original one.
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
- Subprocesses — both `run_command` and custom CLI tools — run in their own process group. The group is killed on timeout or cancellation, and it is also reaped after a normal exit, so an ordinary background child (`&`, `nohup`) does not outlive the call. A child that calls `setsid` or otherwise daemonizes into a new session escapes the group and can still outlive it. If a well-behaved child exited successfully but left a background process holding the output pipe open, the call reports success after a short (~2s) wait rather than hanging.
- Output is capped in memory at 10 MB.

**`run_command`, unrestricted mode:** the old `sh -c` behaviour. `abby build` prints a warning and `--describe` reports it.

**What the sandbox does not do:**
- It does not confine `run_command` *arguments*. `cat *` can read any file. `git *`, `find *`, `env *`, `xargs *` and `make *` (with `write_file`) can run arbitrary programs. Allow the narrowest commands you can.
- It does not fully confine the Go toolchain either: an allowlisted `go test *`/`go build *`/`go vet *` still permits `go test -exec <prog>`, `go build -toolexec <prog>` and `go vet -vettool=<prog>`, each of which runs an arbitrary program; `gofmt -w <path>` can write to any path the process can reach, not just `allowed_dirs`; and `go test` itself runs the package's (possibly model-written) test code. Prefer exact argv entries with no trailing `*` (e.g. `go test ./...` rather than `go test *`) so the model cannot add these flags.
- It is not an OS sandbox (no seatbelt or landlock).
- It checks each path when the call is made, so it does not defend against a local process racing to swap symlinks.
- `read_file` on a FIFO (named pipe) blocks until a writer opens the other end; this is a documented limitation, not something the sandbox or the executor timeout currently guards against.

**Startup fallback.** If the effective sandbox — compiled defaults plus any `config.yaml` override — fails to build, the agent prints an error to stderr and falls back rather than exiting; this runs on every subcommand, not only `serve-mcp`. When a `config.yaml` sandbox override caused the failure (for example a hand-edited config with `sandbox.allowed_dirs: [""]`), it retries the compiled-in sandbox with a `config reset sandbox` hint. If the compiled sandbox is itself invalid — with or without an override — file and command tools are disabled outright (a deny-all sandbox: no allowed directories, no allowed commands) rather than widening access to the default sandbox (the working directory).

**`--describe`** includes a `sandbox` object: `allowedDirs`, `bash`, `allowCommands`, `maxCommandTimeout`, and `warnings` (present only when there is at least one).

**Changing it after install** (restart the runtime session afterwards):

```bash
my-agent config set sandbox.allow_commands '["go test *","make *"]'   # JSON array, or: "go test *,make *"
my-agent config set sandbox.allowed_dirs ".,../shared"
my-agent config set sandbox.bash unrestricted                       # prints a warning
my-agent config set sandbox.max_command_timeout 300s
my-agent config reset sandbox
```

- `config set sandbox.*` validates the merged override — all four fields together, not just the one being set — and refuses to write one that would leave the sandbox unusable.
- List values (`allow_commands`, `allowed_dirs`) drop blank entries in both the JSON-array and comma-separated forms.
- Every `sandbox.*` set prints a restart hint on stdout, since a running MCP session already loaded the old sandbox. Setting `bash unrestricted`, or `allowed_dirs` to something containing `/`, also prints a warning to stderr.

### Migrating from v0.10

1. `tools: Bash` now needs `sandbox.allow_commands`, or `sandbox.bash: unrestricted`. Until then `run_command` refuses every call and `abby build` prints a note.
2. File tools are confined to the server's working directory unless `sandbox.allowed_dirs` says otherwise.
3. Restricted `run_command` no longer uses a shell. Pipes, redirects and chaining need `sandbox.bash: unrestricted`.
4. `write_file` and `edit_file` are now annotated destructive.
5. Library users: builtins use `tools.Definition.HandlerCtx`. `Handler` still works, but it runs under the default sandbox with no deadline. `tools.DefaultCommandPolicy()` no longer has a denylist.
6. Custom CLI tools, not just `run_command`, now run in their own process group that is killed after the call returns, so a background child started with `&` or `nohup` no longer outlives the call (one that calls `setsid` or otherwise daemonizes into a new session still escapes and can outlive it).
7. CLI tool output capture is capped at 10 MB in memory (`tools.DefaultMaxOutputBytes`); output beyond that is truncated with a `[output truncated at N bytes]` marker.

## Annotations

Tool annotations provide MCP clients with hints about tool behavior. They are hints only -- clients should not make security decisions based on them.

```go
def.WithAnnotations(&tools.Annotations{
    ReadOnlyHint:    true,              // tool does not modify state
    DestructiveHint: tools.BoolPtr(false), // tool is not destructive (nil = MCP default true)
    IdempotentHint:  true,              // safe to call multiple times
    OpenWorldHint:   tools.BoolPtr(false), // tool operates in a closed system (nil = MCP default true)
    Title:           "Human-Readable Name",
})
```

Annotation fields:

| Field | Type | Default | Meaning |
|---|---|---|---|
| `ReadOnlyHint` | `bool` | `false` | Tool does not modify state |
| `DestructiveHint` | `*bool` | `nil` (MCP default: `true`) | Tool may destructively modify state |
| `IdempotentHint` | `bool` | `false` | Calling multiple times with same input has same effect |
| `OpenWorldHint` | `*bool` | `nil` (MCP default: `true`) | Tool interacts with external systems |
| `Title` | `string` | `""` | Human-readable title for the tool |

For pointer fields, use `tools.BoolPtr(value)` to set an explicit value. `nil` means "use the MCP default."

## Input Validation

Tool definitions validate input against their schema before execution. The `ValidateInput()` method checks:

- **Required fields** are present
- **Property types** match the declared JSON Schema type

```go
def := tools.BuiltinTool("example", "desc", map[string]any{
    "type": "object",
    "properties": map[string]any{
        "key": map[string]any{"type": "string"},
    },
    "required": []string{"key"},
}, handler)

err := def.ValidateInput(map[string]any{"key": 123.0})
// error: field "key": expected string, got number
```

Validation runs automatically in the `run-tool` subcommand before executing the tool. MCP tool calls also go through the executor, which handles errors and returns them to the client.

Supported types: `string`, `number`, `integer`, `boolean`, `array`, `object`.

## Tool Timeout

Set a global tool execution timeout with `WithToolTimeout()`:

```go
agent.WithToolTimeout(60 * time.Second) // default is 30s
```

This applies to both CLI and builtin tools. CLI tools that exceed the timeout are killed. The executor returns a timeout error.

## Registering Multiple Tools

Use variadic `WithTools()`:

```go
agent.WithTools(
    tools.CLI("date", "date", "Get current date"),
    tools.CLI("uptime", "uptime", "System uptime"),
    readFileTool(),
    goTestTool(),
),
```

Or call `WithTools()` multiple times -- definitions accumulate:

```go
agent.WithTools(cliTools()...),
agent.WithTools(builtinTools()...),
```

## Memory Tools (Automatic)

When memory is enabled (`WithMemory(true)`), four builtin tools are automatically registered:

- `memory_read` -- read a value by key
- `memory_write` -- write a value (overwrites existing)
- `memory_list` -- list all keys
- `memory_delete` -- delete a key

These appear in `--describe` and are exposed via MCP. You do not register them manually. See the [Memory Guide](./memory.md) for details.
