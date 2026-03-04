# Memory Guide

Agentfile provides per-agent persistent memory as a file-based key-value store. Each agent's memory lives at `~/.agentfile/<name>/memory/` and persists across conversations.

## Enabling Memory

```go
agent.WithMemory(true),
```

This does two things:

1. Creates a `FileStore` at `~/.agentfile/<name>/memory/`
2. Registers four builtin tools: `memory_read`, `memory_write`, `memory_list`, `memory_delete`

## CLI Commands

Memory is accessible directly from the command line:

```bash
# Write a value (overwrites if key exists)
./my-agent memory write notes "Project uses Go 1.24"

# Read a value
./my-agent memory read notes

# Append to existing key (or create it)
./my-agent memory append notes " -- confirmed on 2024-01-15"

# List all keys
./my-agent memory list

# Delete a key
./my-agent memory delete notes
```

## MCP Exposure

When `serve-mcp` runs, memory is exposed in three ways:

### 1. Tools

Claude Code can call these tools during conversations:

- `memory_read` -- `{"key": "notes"}` -- read a value
- `memory_write` -- `{"key": "notes", "value": "content"}` -- write a value
- `memory_list` -- `{}` -- list all keys
- `memory_delete` -- `{"key": "notes"}` -- delete a key

These tools have appropriate MCP annotations:

```go
// memory_read
ReadOnlyHint:   true
IdempotentHint: true
OpenWorldHint:  false

// memory_write
DestructiveHint: false  // overwrites but not destructive in MCP sense
IdempotentHint:  true

// memory_delete
OpenWorldHint: false
```

### 2. Resources

Memory keys are exposed as MCP resources:

- `memory://<name>/` -- JSON index of all keys
- `memory://<name>/{key}` -- individual key content

### 3. Prompts

A `memory-context` prompt template is registered:

- No arguments: returns a summary of all keys
- With `key` argument: returns the content of that specific key

## Storage Details

Each key is stored as a file at `~/.agentfile/<name>/memory/<key>.md`. Keys:

- Must not be empty
- Must not contain path separators (`/` or `\`)
- Are case-sensitive
- Map directly to filenames (with `.md` extension)

Values are stored as plain text. There is no structured data format enforced -- store whatever text makes sense for your agent.

## Limits Configuration

By default, memory is unlimited. Use `WithMemoryLimits()` to set capacity bounds:

```go
agent.WithMemoryLimits(memory.Limits{
    MaxKeys:       100,      // maximum number of keys (0 = unlimited)
    MaxValueBytes: 10240,    // maximum size per value in bytes (0 = unlimited)
    MaxTotalBytes: 1048576,  // maximum total storage in bytes (0 = unlimited)
})
```

Limits are enforced on write and append operations:

- `MaxKeys`: checked when creating a new key (overwriting an existing key is always allowed)
- `MaxValueBytes`: checked against the full value size (for append, the existing + new content)
- `MaxTotalBytes`: checked against the sum of all stored values, accounting for the key being overwritten

When a limit is exceeded, the operation returns an error and the write does not happen.

Limits appear in the `--describe` manifest:

```json
{
  "name": "my-agent",
  "memory": true,
  "memoryLimits": {
    "maxKeys": 100,
    "maxValueBytes": 10240,
    "maxTotalBytes": 1048576
  }
}
```

## Use Cases

**Session notes**: Store decisions, context, and observations that persist across conversations.

```bash
./my-agent memory write decisions "Using PostgreSQL for the data layer"
./my-agent memory append decisions "\nChose gRPC over REST for internal services"
```

**Project health tracking**: An agent can store test results and trends over time in persistent memory.

**Context accumulation**: Agents can build up knowledge about a project by writing to memory during conversations, then reading it back in future sessions.

## Testing Patterns

Use `t.TempDir()` for isolated memory stores in tests:

```go
func TestMyAgent_Memory(t *testing.T) {
    dir := filepath.Join(t.TempDir(), "memory")
    store, err := memory.NewFileStoreAt(dir, memory.Limits{})
    if err != nil {
        t.Fatal(err)
    }
    mgr := memory.NewManager(store)

    // Write and read
    if err := mgr.Set("key", "value"); err != nil {
        t.Fatal(err)
    }
    got, err := mgr.Get("key")
    if err != nil {
        t.Fatal(err)
    }
    if got != "value" {
        t.Errorf("Get() = %q, want %q", got, "value")
    }
}
```

`NewFileStoreAt()` creates a store at a specific directory instead of `~/.agentfile/<name>/memory/`. This avoids polluting the user's home directory during tests.

For testing limits:

```go
func TestMemory_Limits(t *testing.T) {
    dir := filepath.Join(t.TempDir(), "memory")
    store, err := memory.NewFileStoreAt(dir, memory.Limits{MaxKeys: 2})
    if err != nil {
        t.Fatal(err)
    }

    store.Write("a", "1")
    store.Write("b", "2")
    err = store.Write("c", "3")
    // err: key count 3 would exceed limit of 2 keys
}
```

For concurrency testing, use the `Manager` which wraps the store with a `sync.RWMutex`:

```go
func TestMemory_Concurrent(t *testing.T) {
    store, _ := memory.NewFileStoreAt(t.TempDir(), memory.Limits{})
    mgr := memory.NewManager(store)

    done := make(chan bool, 10)
    for i := 0; i < 10; i++ {
        go func() {
            mgr.Set("key", "value")
            mgr.Get("key")
            done <- true
        }()
    }
    for i := 0; i < 10; i++ {
        <-done
    }
}
```

## Integration Testing

For end-to-end testing with a built binary, override `HOME` to isolate memory:

```go
tmpHome := t.TempDir()
cmd := exec.Command(binaryPath, "memory", "write", "test-key", "test-value")
cmd.Env = append(os.Environ(), "HOME="+tmpHome)
```

See `internal/integration/agent_test.go` for the full lifecycle test pattern (write, read, list, append, delete, verify deleted).
