# Tools Guide

Tools are the actions an agent can perform. Agentfile supports two kinds: **CLI tools** that wrap external commands, and **builtin tools** that run Go functions in-process.

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
