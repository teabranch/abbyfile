# Testing Guide

Agentfile has three levels of testing: unit tests for individual packages, integration tests against built binaries, and MCP bridge tests for the protocol layer.

## Unit Testing Tools

### CLI Tools

CLI tool tests use real system commands. The `Executor` runs them as subprocesses:

```go
func TestExecutor_Run_CLI(t *testing.T) {
    exec := tools.NewExecutor(5*time.Second, nil)
    def := tools.CLI("echo", "echo", "Echo text")
    def.Args = []string{"hello"}

    result, err := exec.Run(context.Background(), def, nil)
    if err != nil {
        t.Fatalf("Run() error: %v", err)
    }
    if result != "hello" {
        t.Errorf("Run() = %q, want %q", result, "hello")
    }
}
```

Test argument passing via the `args` input field:

```go
func TestExecutor_Run_WithArgs(t *testing.T) {
    exec := tools.NewExecutor(5*time.Second, nil)
    def := tools.CLI("echo", "echo", "Echo text")

    result, err := exec.Run(context.Background(), def, map[string]any{
        "args": "hello world",
    })
    // result == "hello world"
}
```

### Builtin Tools

Builtin tool handlers are plain functions. Test them directly:

```go
func TestBuiltinTool(t *testing.T) {
    exec := tools.NewExecutor(5*time.Second, nil)
    def := tools.BuiltinTool("test", "A test tool", nil,
        func(input map[string]any) (string, error) {
            return "builtin result", nil
        },
    )

    result, err := exec.Run(context.Background(), def, nil)
    // result == "builtin result"
}
```

### Timeout Testing

```go
func TestExecutor_Run_Timeout(t *testing.T) {
    exec := tools.NewExecutor(100*time.Millisecond, nil)
    def := tools.CLI("sleep", "sleep", "Sleep")
    def.Args = []string{"10"}

    _, err := exec.Run(context.Background(), def, nil)
    // err contains "timed out"
}
```

### Input Validation

Test schema validation without running the tool:

```go
func TestValidateInput(t *testing.T) {
    def := &tools.Definition{
        Name: "test_tool",
        InputSchema: map[string]any{
            "type":     "object",
            "required": []any{"key"},
            "properties": map[string]any{
                "key": map[string]any{"type": "string"},
            },
        },
    }

    // Missing required field
    err := def.ValidateInput(map[string]any{})
    // err: missing required field "key"

    // Wrong type
    err = def.ValidateInput(map[string]any{"key": 123.0})
    // err: field "key": expected string, got number
}
```

See `pkg/tools/validate_test.go` for the full table-driven test suite.

## Unit Testing Memory

Use `t.TempDir()` for isolated file stores:

```go
func TestMemory(t *testing.T) {
    dir := filepath.Join(t.TempDir(), "memory")
    store, err := memory.NewFileStoreAt(dir, memory.Limits{})
    if err != nil {
        t.Fatal(err)
    }

    store.Write("greeting", "Hello, world!")
    got, _ := store.Read("greeting")
    // got == "Hello, world!"
}
```

Test limits enforcement:

```go
func TestMemory_MaxKeys(t *testing.T) {
    dir := filepath.Join(t.TempDir(), "memory")
    store, _ := memory.NewFileStoreAt(dir, memory.Limits{MaxKeys: 2})

    store.Write("a", "1")
    store.Write("b", "2")
    err := store.Write("c", "3")
    // err: key count 3 would exceed limit of 2 keys
}
```

Test the Manager's built-in tool handlers:

```go
func TestManager_HandleWrite_Read(t *testing.T) {
    store, _ := memory.NewFileStoreAt(t.TempDir(), memory.Limits{})
    mgr := memory.NewManager(store)

    memTools := mgr.Tools()
    toolMap := make(map[string]*tools.Definition)
    for _, tool := range memTools {
        toolMap[tool.Name] = tool
    }

    // Call the write handler directly
    result, err := toolMap["memory_write"].Handler(map[string]any{
        "key":   "test",
        "value": "hello world",
    })
    // result: "Stored 11 bytes under key \"test\""

    // Call the read handler directly
    result, err = toolMap["memory_read"].Handler(map[string]any{"key": "test"})
    // result: "hello world"
}
```

## Unit Testing Prompts

Use `t.Setenv("HOME", ...)` to isolate override behavior:

```go
func TestLoader_Override(t *testing.T) {
    tmpHome := t.TempDir()
    t.Setenv("HOME", tmpHome)

    overrideDir := filepath.Join(tmpHome, ".agentfile", "test-agent")
    os.MkdirAll(overrideDir, 0o755)
    os.WriteFile(
        filepath.Join(overrideDir, "override.md"),
        []byte("Override prompt content"),
        0o644,
    )

    loader := prompt.NewLoader("test-agent", testFS, "testdata/system.md")
    got, _ := loader.Load()
    // got == "Override prompt content"
}
```

## MCP Bridge Testing

Use `gomcp.NewInMemoryTransports()` for in-process client/server testing:

```go
func TestBridge(t *testing.T) {
    registry := tools.NewRegistry()
    registry.Register(tools.BuiltinTool("echo", "Echo input",
        map[string]any{
            "type": "object",
            "properties": map[string]any{
                "message": map[string]any{"type": "string"},
            },
            "required": []string{"message"},
        },
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

    go func() { bridge.ServeTransport(ctx, serverTransport) }()

    client := gomcp.NewClient(&gomcp.Implementation{
        Name: "test-client", Version: "v0.1.0",
    }, nil)
    session, _ := client.Connect(ctx, clientTransport, nil)
    defer session.Close()

    // List tools
    listResult, _ := session.ListTools(ctx, nil)
    // Verify tool count, names, annotations

    // Call a tool
    callResult, _ := session.CallTool(ctx, &gomcp.CallToolParams{
        Name:      "echo",
        Arguments: map[string]any{"message": "hello"},
    })
    // Verify result content
}
```

Test memory resources, prompts, and error handling in the same pattern. See `pkg/mcp/bridge_test.go` for the full test suite.

## Integration Testing

Integration tests build the binary and exercise all subcommands. They live in `internal/integration/` and use the `//go:build integration` tag.

```bash
# Run integration tests
make integration
# Equivalent to:
go test -tags integration -race -count=1 -timeout 60s ./internal/integration/
```

Integration tests are not included in the normal `go test ./...` run.

### Test Setup

The `TestMain` function builds the binary once for all tests:

```go
//go:build integration

func TestMain(m *testing.M) {
    tmp, _ := os.MkdirTemp("", "agentfile-integration-*")
    defer os.RemoveAll(tmp)

    binaryPath = filepath.Join(tmp, "my-agent")
    cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/my-agent")
    cmd.Dir = findProjectRoot()
    cmd.Run()

    os.Exit(m.Run())
}
```

### Important: Use `cmd.Output()`, not `CombinedOutput()`

Agent binaries log to stderr via `slog`. When parsing stdout output (like JSON from `--describe`), use `cmd.Output()` to get only stdout. `CombinedOutput()` mixes in log lines and corrupts the output.

```go
func runAgentStdout(t *testing.T, args ...string) string {
    cmd := exec.CommandContext(ctx, binaryPath, args...)
    out, err := cmd.Output() // stdout only
    // ...
}
```

### MCP Integration with CommandTransport

```go
func TestServeMCP(t *testing.T) {
    cmd := exec.CommandContext(ctx, binaryPath, "serve-mcp")
    client := gomcp.NewClient(&gomcp.Implementation{
        Name: "integration-test", Version: "v0.1.0",
    }, nil)

    session, _ := client.Connect(ctx, &gomcp.CommandTransport{Command: cmd}, nil)
    defer session.Close()

    // List tools, call tools, verify responses
}
```

## The `make all` Pipeline

```bash
make all
```

Runs four stages in order:

1. **fmtcheck** -- verify all files are `gofmt`-formatted
2. **vet** -- `go vet ./...` for static analysis
3. **test** -- `go test -race ./...` (all unit tests)
4. **build** -- build the agentfile CLI

Individual stages:

```bash
make fmt          # auto-format all files
make fmtcheck     # check formatting (CI-friendly, fails on unformatted)
make vet          # static analysis
make test         # unit tests with race detector
make build        # build all binaries
make integration  # integration tests (builds binary first)
make clean        # remove built binaries
```

## CI Setup Patterns

A typical CI workflow:

```yaml
steps:
  - uses: actions/setup-go@v5
    with:
      go-version: '1.24'

  - name: Lint and test
    run: make all

  - name: Integration tests
    run: make integration
```

Key considerations:

- Always use `-race` flag (already set in the Makefile)
- Integration tests need a working Go toolchain (they `go build` the binary)
- Memory tests are isolated via `t.TempDir()` -- no filesystem cleanup needed

## Distribution Testing

The distribution layer (install, uninstall, list, publish) has integration tests in `internal/integration/distribution_test.go`:

```go
func TestList(t *testing.T) {
    // Verifies list shows "No agents installed." with empty registry.
    // Uses isolated HOME via t.TempDir().
}

func TestInstallLocalWithRegistry(t *testing.T) {
    // Installs a binary from ./build/, verifies:
    // - Binary copied to .agentfile/bin/
    // - Registry entry created with source="local" and correct version
    // - agentfile list shows the agent
}

func TestUninstall(t *testing.T) {
    // Installs, then uninstalls. Verifies:
    // - Binary removed
    // - Registry cleaned up
    // - List shows empty again
}

func TestPublishDryRun(t *testing.T) {
    // Runs publish --dry-run. Verifies:
    // - Cross-compiled binaries created for all 4 targets
    // - No GitHub Release created
}
```

Distribution tests use `HOME` override for registry isolation and reuse the test agent binary built by `TestMain`.

### Unit Testing the Registry

```go
func TestSaveAndLoad(t *testing.T) {
    path := filepath.Join(t.TempDir(), "registry.json")
    r, _ := registry.Load(path)

    r.Set(registry.Entry{
        Name: "test", Source: "local", Version: "1.0.0",
        Path: "/bin/test", Scope: "local",
    })
    r.Save()

    r2, _ := registry.Load(path)
    e, ok := r2.Get("test")
    // ok == true, e.Version == "1.0.0"
}
```

### Unit Testing the GitHub Client

Tests use `httptest` to mock the GitHub API:

```go
func TestGetRelease(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        json.NewEncoder(w).Encode(github.Release{
            TagName: "myagent/v1.0.0",
            Assets:  []github.Asset{{Name: "myagent-linux-amd64"}},
        })
    }))
    defer srv.Close()

    c := &github.Client{HTTPClient: srv.Client(), BaseURL: srv.URL}
    ref := github.ReleaseRef{Owner: "owner", Repo: "repo", Agent: "myagent", Version: "1.0.0"}
    release, _ := c.GetRelease(context.Background(), ref)
    // release.TagName == "myagent/v1.0.0"
}
```

## Test Count

The project currently has 110+ passing tests across all packages:

```
pkg/agent       -- agent creation, options, defaults
pkg/tools       -- registry, executor, validation
pkg/memory      -- file store, limits, concurrency, manager tools
pkg/prompt      -- loader, override, paths
pkg/builtins    -- builtin tool implementations
pkg/definition  -- Agentfile + agent .md parsing
pkg/builder     -- code generation templates
pkg/mcp         -- bridge, tools, annotations, resources, prompts
pkg/registry    -- installed agents tracking, atomic save/load
pkg/github      -- GitHub Releases client, version comparison, ref parsing
internal/cli    -- root command, validate, flags
```

Plus integration tests that exercise the full binary end-to-end, including distribution commands (list, install, uninstall, publish --dry-run).
