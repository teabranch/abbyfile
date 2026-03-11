# MCP Integration Guide

Agentfile agents integrate with Claude Code through the Model Context Protocol (MCP). The `serve-mcp` subcommand starts an MCP-over-stdio server that exposes the agent's tools, prompts, and memory.

## What `serve-mcp` Exposes

When you run `./my-agent serve-mcp`, the MCP server registers:

### Tools

Every tool registered via `WithTools()` plus the automatically registered memory tools (if memory is enabled). Additionally, a `get_instructions` tool is always registered for backward compatibility.

Example tool listing for an agent with `tools: Read, Write` and memory enabled:
- `read_file`, `write_file` -- builtin tools
- `memory_read`, `memory_write`, `memory_list`, `memory_delete` -- memory tools
- `get_instructions` -- returns the system prompt

### Server Instructions

The system prompt is set as the MCP server's `instructions` field during the initialization handshake. MCP clients that support server instructions receive the prompt automatically.

If a model hint is configured (via the agent definition or a config override), a `## Model Preference` section is appended to the instructions, e.g.:

```
## Model Preference

This agent was designed for model: claude-opus-4-6
```

This is informational — the runtime decides which model to use.

### Resources (when memory is enabled)

- `memory://<name>/` -- JSON array of all memory keys
- `memory://<name>/{key}` -- content of a specific memory key

### Prompts

- `system` -- returns the agent's system prompt as a prompt message
- `memory-context` (when memory is enabled) -- returns memory state; accepts an optional `key` argument to return a specific key's content

## `.mcp.json` Configuration

Claude Code discovers MCP servers through `.mcp.json` in the project root.

### Production: Built Binary

```json
{
  "mcpServers": {
    "my-agent": {
      "command": "./my-agent",
      "args": ["serve-mcp"]
    }
  }
}
```

### Development: `go run`

Use `go run` so code changes take effect without rebuilding:

```json
{
  "mcpServers": {
    "my-agent": {
      "type": "stdio",
      "command": "go",
      "args": ["run", "./cmd/my-agent", "serve-mcp"]
    }
  }
}
```

`agentfile build` auto-generates `.mcp.json` with entries like:

```json
{
  "mcpServers": {
    "my-agent": {
      "command": "/path/to/build/my-agent",
      "args": ["serve-mcp"]
    }
  }
}
```

### Multiple Agents

Register multiple agents in the same `.mcp.json`:

```json
{
  "mcpServers": {
    "go-pro": {
      "command": "./build/go-pro",
      "args": ["serve-mcp"]
    },
    "tool-eng": {
      "command": "./build/tool-eng",
      "args": ["serve-mcp"]
    }
  }
}
```

Each agent runs as a separate MCP server process. Claude Code manages the lifecycle.

## The MCP Bridge

The `pkg/mcp` package translates between Agentfile's internal types and the MCP protocol. It uses the official Go MCP SDK (`github.com/modelcontextprotocol/go-sdk`).

The bridge:

1. Creates an MCP `Server` with the agent's name and version
2. Registers each tool from the `tools.Registry` as an MCP tool
3. Maps `tools.Annotations` to MCP `ToolAnnotations`
4. Wires tool calls through the `tools.Executor`
5. Registers memory resources and prompt templates (if applicable)
6. Runs on a `StdioTransport` (production) or an in-memory transport (testing)

From `pkg/mcp/bridge.go`:

```go
func (b *Bridge) Serve(ctx context.Context) error {
    return b.ServeTransport(ctx, &gomcp.StdioTransport{})
}

func (b *Bridge) ServeTransport(ctx context.Context, transport gomcp.Transport) error {
    instructions, _ := b.cfg.Loader.Load()
    server := gomcp.NewServer(&gomcp.Implementation{
        Name:    b.cfg.Name,
        Version: b.cfg.Version,
    }, &gomcp.ServerOptions{
        Instructions: instructions,
    })
    // ... register tools, resources, prompts ...
    return server.Run(ctx, transport)
}
```

## Testing with In-Memory Transports

For unit tests, use `gomcp.NewInMemoryTransports()` to create a client-server pair without stdio:

```go
func TestMyBridge(t *testing.T) {
    registry := tools.NewRegistry()
    registry.Register(tools.BuiltinTool(
        "echo", "Echo input", schema,
        func(input map[string]any) (string, error) {
            msg, _ := input["message"].(string)
            return "echo: " + msg, nil
        },
    ))

    bridge := mcp.NewBridge(mcp.BridgeConfig{
        Name:     "test-agent",
        Version:  "v0.1.0",
        Registry: registry,
        Executor: tools.NewExecutor(30*time.Second, nil),
        Loader:   loader,
    })

    serverTransport, clientTransport := gomcp.NewInMemoryTransports()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    go func() {
        bridge.ServeTransport(ctx, serverTransport)
    }()

    client := gomcp.NewClient(&gomcp.Implementation{
        Name: "test-client", Version: "v0.1.0",
    }, nil)
    session, err := client.Connect(ctx, clientTransport, nil)
    // ... use session.ListTools(), session.CallTool(), etc.
}
```

See `pkg/mcp/bridge_test.go` for comprehensive examples including tool calls, error handling, annotations, memory resources, and prompt templates.

## Integration Testing with `CommandTransport`

For end-to-end tests against a built binary, use `gomcp.CommandTransport`:

```go
func TestServeMCP_Integration(t *testing.T) {
    cmd := exec.CommandContext(ctx, binaryPath, "serve-mcp")
    client := gomcp.NewClient(&gomcp.Implementation{
        Name: "integration-test", Version: "v0.1.0",
    }, nil)

    session, err := client.Connect(ctx, &gomcp.CommandTransport{Command: cmd}, nil)
    // ... exercise the MCP API ...
}
```

This starts the actual binary, connects to it via stdio, and exercises the full MCP protocol. See `internal/integration/agent_test.go` for the pattern.

## Debugging

Agent binaries log to stderr via `slog`. When running under Claude Code, these logs are separate from the MCP protocol (which uses stdout).

To see logs during development:

```bash
./my-agent serve-mcp 2>agent.log
```

Or set a custom logger:

```go
agent.WithLogger(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))),
```

## Plugin Alternative

Instead of wiring `.mcp.json` manually, you can generate a Claude Code plugin directory with `agentfile build --plugin`. The plugin wraps the binary with its own `.mcp.json` and adds skills:

```bash
agentfile build --plugin
claude --plugin-dir ./build/my-agent.claude-plugin/
```

The plugin's `.mcp.json` uses a relative path (`./my-agent`), making the directory self-contained and portable. See the [Plugins Guide](./plugins.md).

## Compatibility

Agentfile uses the standard MCP protocol. While it is designed for Claude Code, any MCP client can connect to an agent's `serve-mcp` server. The binary is a generic MCP server that happens to be built with Agentfile.
