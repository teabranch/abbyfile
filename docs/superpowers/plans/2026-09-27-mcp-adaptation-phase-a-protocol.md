# MCP 2026-07-28 Adaptation — Phase A (Protocol Conformance) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every Abbyfile agent binary speaks MCP 2026-07-28 while still serving 2025-11-25 (and 2025-06-18) clients, returns `structuredContent` for tools that declare an `outputSchema`, validates tool names per SEP-986, and emits cache hints.

**Architecture:** Upgrade `github.com/modelcontextprotocol/go-sdk` from v1.4.0 to v1.8.0, whose stdio server is dual-era (legacy `initialize` + modern `server/discover` with per-request `_meta`). The bridge (`pkg/mcp/bridge.go`) keeps its shape; we add a structured-output path that consumes a new unshaped `Executor.RunRaw`, a `SetCacheable` hook, and tool-name validation in `tools.Registry.Register`. Protocol behavior is pinned by in-memory tests for both eras plus one raw-stdio integration test.

**Tech Stack:** Go 1.26, go-sdk v1.8.0, cobra, standard `testing`.

**Spec:** `docs/superpowers/specs/2026-09-27-mcp-2026-07-28-adaptation-design.md` (Phase A section, plus defects D2 and D11).

## Global Constraints

- go-sdk version: exactly **v1.8.0** (requires Go ≥ 1.25; repo is on 1.26.0).
- Protocol eras that MUST work: `2026-07-28` (modern, discover) and `2025-11-25` (legacy, initialize). `2025-06-18` SHOULD work.
- Do **not** set any `MCPGODEBUG` escape hatch.
- Do **not** disable `GOSUMDB`, set `GONOSUMDB`, `GONOSUMCHECK`, `GOFLAGS=-insecure`, or `GOINSECURE` to get dependencies. If module download fails TLS verification, STOP and ask the user (see Task 1 prerequisite).
- Tool-name rule: `^[A-Za-z0-9_.-]{1,128}$` (SEP-986).
- Structured content is never truncated; oversized structured output is an `isError` result.
- All tool-level failures are `CallToolResult{IsError:true}`, never JSON-RPC errors.
- Logging goes to stderr only; stdout carries only JSON-RPC messages.
- Cache hints: list results (`tools/list`, `prompts/list`, `resources/list`, `resources/templates/list`) → `ttlMs: 3600000`, `cacheScope: "public"`. Memory `resources/read` → `ttlMs: 0`, `cacheScope: "private"`.
- Commit format: `<type>: <description>` + trailer `Co-Authored-By: Claude <noreply@anthropic.com>`. Never use `--no-verify`.
- Out of scope for Phase A: removing lazy loading / `get_instructions` (Phase D), sandboxing and `HandlerCtx` (Phase B), install/runtime config (Phase C).

## Review Focus

1. **A custom tool whose frontmatter `input_schema` omits `"type": "object"`.** v1.8.0's `Server.AddTool` **panics** on this (`server.go:315`). A user expects the agent to start; the bridge must default `type` to `"object"` when absent. → Task 1, Step 7.
2. **A structured tool that prints a JSON array or bare string/number** (SEP-2106 allows any JSON value). A user expects it to be accepted, not rejected as "not an object". → Task 4, Step 1 (`TestStructuredOutputNonObjectJSON`).
3. **A structured tool whose output has a trailing newline** (every shell tool does this). A user expects it to parse fine. → Task 4, Step 1 (`TestStructuredOutputTrailingNewline`).
4. **A structured tool that fails (non-zero exit).** A user expects the normal error result, with no attempt to parse stderr as JSON. → Task 4, Step 1 (`TestStructuredOutputToolFailure`).
5. **An older client on 2025-06-18.** A user expects it to keep working after the upgrade. → Task 6, Step 1 (the `2025-06-18` row in `TestDualEra`).

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `go.mod`, `go.sum` | Dependency pins | Modify: go-sdk v1.8.0 |
| `pkg/mcp/bridge.go` | Registry → MCP server translation | Modify: input-schema defaulting, structured path, `SetCacheable`, memory read cache fields |
| `pkg/mcp/structured.go` | Build `CallToolResult` from raw JSON output | Create |
| `pkg/mcp/structured_test.go` | Unit tests for `structuredResult` | Create |
| `pkg/mcp/cache.go` | `setCacheable` policy function | Create |
| `pkg/mcp/cache_test.go` | Cache hint tests | Create |
| `pkg/mcp/conformance_test.go` | Dual-era, list invariance, isError tests | Create |
| `pkg/mcp/wire_test.go` + `pkg/mcp/testdata/tools_list.golden.json` | Wire snapshot of `tools/list` | Create |
| `pkg/mcp/bridge_test.go` | Existing tests | Modify: add `startBridgeEra` helper |
| `pkg/tools/executor.go` | Tool execution + shaping | Modify: split `RunRaw` / `Shape` / `MaxOutputBytes` |
| `pkg/tools/executor_raw_test.go` | Tests for the split | Create |
| `pkg/tools/registry.go` | Tool registry | Modify: SEP-986 name validation |
| `pkg/tools/name.go` + `name_test.go` | `ValidateToolName` | Create |
| `pkg/definition/agent.go` | Frontmatter parsing | Modify: 128-char limit on custom tool names |
| `internal/integration/stdio_test.go` | Raw stdio hygiene test (build tag `integration`) | Create |

---

### Task 1: Wire snapshot, SDK upgrade, and compile fixes

**Prerequisite (blocking):** During design, `proxy.golang.org` and `sum.golang.org` failed TLS verification (`x509: certificate signed by unknown authority`), probably because of a corporate TLS-intercepting proxy. Before Step 4, run:

```bash
go mod download -json github.com/modelcontextprotocol/go-sdk@v1.8.0 | grep -E '"(Error|Dir)"'
```

If it prints `"Error"` with an x509 message, STOP. Ask the user to install their corporate root CA (e.g. `export SSL_CERT_FILE=/path/to/corp-ca-bundle.pem`) or to supply an approved `GOPROXY`. Do not work around checksum verification.

**Files:**
- Create: `pkg/mcp/wire_test.go`, `pkg/mcp/testdata/tools_list.golden.json`
- Modify: `go.mod`, `go.sum`, `pkg/mcp/bridge.go` (`schemaToRaw`), and any test that fails to compile
- Test: `pkg/mcp/wire_test.go`, `pkg/mcp/bridge_test.go`

**Interfaces:**
- Consumes: existing `startBridge(t, *tools.Registry)` in `pkg/mcp/bridge_test.go`.
- Produces: `schemaToRaw(schema any) json.RawMessage` now guarantees a top-level `"type":"object"` for input schemas (new helper `inputSchemaToRaw`). The golden file serves as the wire contract for later tasks.

- [ ] **Step 1: Write the wire snapshot test (on v1.4.0, before the upgrade)**

Create `pkg/mcp/wire_test.go`:

```go
package mcp_test

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/teabranch/abbyfile/pkg/tools"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

// wireRegistry is a fixed registry covering annotations, output schema, and
// a schema without explicit type, so SDK serialization changes show up in the diff.
func wireRegistry() *tools.Registry {
	r := tools.NewRegistry()
	_ = r.Register(tools.BuiltinTool("alpha", "Alpha tool",
		map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
		func(map[string]any) (string, error) { return "ok", nil },
	).WithAnnotations(&tools.Annotations{ReadOnlyHint: true, IdempotentHint: true, Title: "Alpha"}))
	beta := tools.BuiltinTool("beta", "Beta tool",
		map[string]any{"type": "object"},
		func(map[string]any) (string, error) { return `{"n":1}`, nil },
	)
	beta.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{"n": map[string]any{"type": "integer"}}}
	_ = r.Register(beta)
	return r
}

func TestWireToolsListGolden(t *testing.T) {
	session, _ := startBridge(t, wireRegistry())
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got, err := json.MarshalIndent(res.Tools, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join("testdata", "tools_list.golden.json")
	if *updateGolden {
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(append(got, '\n')) != string(want) {
		t.Errorf("tools/list wire changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
```

- [ ] **Step 2: Generate the v1.4.0 golden file and commit it as the baseline**

Run: `go test ./pkg/mcp/ -run TestWireToolsListGolden -update && go test ./pkg/mcp/ -run TestWireToolsListGolden`
Expected: second run PASS; `pkg/mcp/testdata/tools_list.golden.json` exists.

```bash
git add pkg/mcp/wire_test.go pkg/mcp/testdata/tools_list.golden.json
git commit -m "test: snapshot tools/list wire output before SDK upgrade

Co-Authored-By: Claude <noreply@anthropic.com>"
```

- [ ] **Step 3: Run the prerequisite check above.** Continue only if it prints a `"Dir"` and no `"Error"`.

- [ ] **Step 4: Upgrade the SDK**

Run:
```bash
go get github.com/modelcontextprotocol/go-sdk@v1.8.0
go mod tidy
go build ./...
```
Expected: builds. If it doesn't compile, fix each error at the call site. Known facts from reading v1.8.0 source:
- `gomcp.ToolAnnotations` still has `ReadOnlyHint bool`, `IdempotentHint bool`, `DestructiveHint *bool`, `OpenWorldHint *bool`, `Title string`, so the bridge mapping is unchanged.
- `Server.AddTool(*Tool, ToolHandler)`, `AddPrompt`, `AddResource`, `AddResourceTemplate` and `NewServer(*Implementation, *ServerOptions)` keep their signatures.
- `ServerOptions` gains `SetCacheable func(ctx, Request, *Cacheable)` and `SupportedProtocolVersions []string` (used in Task 5).

- [ ] **Step 5: Run the full unit suite**

Run: `go test ./...`
Expected: everything passes except possibly `TestWireToolsListGolden`. If any other test fails, fix it, keeping the test's intent. For example, a test asserting the resource-not-found code `-32002` should now expect `-32602`.

- [ ] **Step 6: Review the wire diff and accept it deliberately**

Run: `go test ./pkg/mcp/ -run TestWireToolsListGolden -v`
Expected diff: `"readOnlyHint": false` / `"idempotentHint": false` now appear on tools that previously omitted them. v1.7.0 dropped `omitempty` on those fields. Any **other** difference (renamed fields, missing `outputSchema`, reordered tools) is a regression; investigate before continuing. When only the expected changes remain:

Run: `go test ./pkg/mcp/ -run TestWireToolsListGolden -update && go test ./pkg/mcp/`
Expected: PASS.

- [ ] **Step 7: Write a failing test for an input schema without `type` (Review Focus #1)**

Append to `pkg/mcp/wire_test.go`:

```go
func TestInputSchemaWithoutTypeDoesNotPanic(t *testing.T) {
	r := tools.NewRegistry()
	_ = r.Register(tools.CLI("notype", "echo", "schema missing type"))
	def := r.Get("notype")
	def.InputSchema = map[string]any{"properties": map[string]any{"args": map[string]any{"type": "string"}}}

	session, _ := startBridge(t, r) // v1.8.0 AddTool panics if type != "object"
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "notype" {
			continue
		}
		raw, _ := json.Marshal(tool.InputSchema)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if m["type"] != "object" {
			t.Fatalf("inputSchema.type = %v, want object", m["type"])
		}
		if _, ok := m["properties"]; !ok {
			t.Fatal("properties dropped")
		}
		return
	}
	t.Fatal("notype tool not listed")
}
```

Run: `go test ./pkg/mcp/ -run TestInputSchemaWithoutTypeDoesNotPanic -v`
Expected: FAIL. The panic surfaces as a test crash, or as the bridge goroutine dying and `ListTools` erroring.

- [ ] **Step 8: Default `type` to `object` for input schemas**

In `pkg/mcp/bridge.go`, add below `schemaToRaw`:

```go
// inputSchemaToRaw is schemaToRaw plus a guarantee that the top-level schema
// declares "type":"object". go-sdk v1.8.0 panics in AddTool otherwise, and
// frontmatter authors routinely omit it.
func inputSchemaToRaw(schema any) json.RawMessage {
	raw := schemaToRaw(schema)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	if _, ok := m["type"]; ok {
		return raw
	}
	withType := make(map[string]any, len(m)+1)
	for k, v := range m {
		withType[k] = v
	}
	withType["type"] = "object"
	out, err := json.Marshal(withType)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return out
}
```

In `addTool`, change `schema := schemaToRaw(def.InputSchema)` to `schema := inputSchemaToRaw(def.InputSchema)`.

- [ ] **Step 9: Run the tests**

Run: `go test ./pkg/mcp/ -v -run 'TestInputSchema|TestWire' && go test ./...`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add go.mod go.sum pkg/mcp/ pkg/
git commit -m "feat: upgrade go-sdk to v1.8.0 for MCP 2026-07-28 dual-era support

Default missing inputSchema type to object (v1.8.0 AddTool panics
otherwise) and accept the annotation omitempty wire change.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: SEP-986 tool-name validation

**Files:**
- Create: `pkg/tools/name.go`, `pkg/tools/name_test.go`
- Modify: `pkg/tools/registry.go:60-69` (`Register`), `pkg/definition/agent.go:285-298` (`validateCustomTools`)
- Test: `pkg/tools/name_test.go`, `pkg/definition/agent_test.go`

**Interfaces:**
- Produces: `func ValidateToolName(name string) error` in package `tools`. `Registry.Register` returns its error.

- [ ] **Step 1: Write failing tests**

`pkg/tools/name_test.go`:

```go
package tools

import (
	"strings"
	"testing"
)

func TestValidateToolName(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"read_file", true},
		{"a.b-c_D9", true},
		{strings.Repeat("x", 128), true},
		{"", false},
		{strings.Repeat("x", 129), false},
		{"has space", false},
		{"slash/name", false},
		{"émoji", false},
		{"colon:name", false},
	}
	for _, c := range cases {
		err := ValidateToolName(c.name)
		if (err == nil) != c.ok {
			t.Errorf("ValidateToolName(%q) err=%v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestRegisterRejectsInvalidName(t *testing.T) {
	r := NewRegistry()
	err := r.Register(&Definition{Name: "bad name", Builtin: true})
	if err == nil || !strings.Contains(err.Error(), "A-Za-z0-9_.-") {
		t.Fatalf("Register err = %v, want SEP-986 message", err)
	}
}
```

Append to `pkg/definition/agent_test.go` (it uses the package's existing test style; `validateCustomTools` is package-private):

```go
func TestValidateCustomToolsNameTooLong(t *testing.T) {
	long := strings.Repeat("a", 129)
	err := validateCustomTools([]CustomToolDef{{Name: long, Command: "echo"}})
	if err == nil || !strings.Contains(err.Error(), "128") {
		t.Fatalf("err = %v, want 128-char limit error", err)
	}
}
```

(Add `"strings"` to that file's imports if it isn't already there.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/tools/ -run 'TestValidateToolName|TestRegisterRejectsInvalidName' && go test ./pkg/definition/ -run TestValidateCustomToolsNameTooLong`
Expected: FAIL (`undefined: ValidateToolName`; the definition test fails because there's no length check).

- [ ] **Step 3: Implement**

`pkg/tools/name.go`:

```go
package tools

import (
	"fmt"
	"regexp"
)

// toolNamePattern is the MCP tool-name rule (SEP-986, spec 2025-11-25+).
var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// ValidateToolName reports whether name is a legal MCP tool name.
func ValidateToolName(name string) error {
	if !toolNamePattern.MatchString(name) {
		return fmt.Errorf("tool name %q is invalid: must be 1-128 characters from [A-Za-z0-9_.-]", name)
	}
	return nil
}
```

In `pkg/tools/registry.go`, replace the empty-name check in `Register`:

```go
func (r *Registry) Register(def *Definition) error {
	if err := ValidateToolName(def.Name); err != nil {
		return err
	}
	if _, exists := r.tools[def.Name]; exists {
		return fmt.Errorf("tool %q already registered", def.Name)
	}
	r.tools[def.Name] = def
	return nil
}
```

In `pkg/definition/agent.go` `validateCustomTools`, add after the `validName` check:

```go
		if len(ct.Name) > 128 {
			return fmt.Errorf("custom_tools[%d]: name %q exceeds 128 characters", i, ct.Name)
		}
```

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/tools/ ./pkg/definition/ ./pkg/agent/ ./pkg/builtins/`
Expected: PASS. Built-in and memory tool names such as `read_file` and `memory_read` already comply. If a test registers a name with a space or an empty name expecting success, fix the test's name.

- [ ] **Step 5: Commit**

```bash
git add pkg/tools/name.go pkg/tools/name_test.go pkg/tools/registry.go pkg/definition/agent.go pkg/definition/agent_test.go
git commit -m "feat: validate tool names against MCP SEP-986

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: Split executor into RunRaw / Shape / MaxOutputBytes

**Files:**
- Modify: `pkg/tools/executor.go`
- Create: `pkg/tools/executor_raw_test.go`

**Interfaces:**
- Produces (used by Task 4):
  - `func (e *Executor) RunRaw(ctx context.Context, def *Definition, input map[string]any) (string, error)`: output with **no** success-path shaping. Builtin output is returned as-is; CLI stdout (or stderr if stdout is empty) is `strings.TrimSpace`d. Error messages are still shaped, as today.
  - `func (e *Executor) Shape(toolName, output string) string`: applies the budget; identity when no budget is set.
  - `func (e *Executor) MaxOutputBytes(toolName string) int64`: the effective byte cap for that tool (per-tool override, else base); `0` when no budget or unlimited.
  - `Run` stays unchanged for callers: `Run == Shape(RunRaw)`.

- [ ] **Step 1: Write failing tests**

`pkg/tools/executor_raw_test.go`:

```go
package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func bigBuiltin(n int) *Definition {
	return BuiltinTool("big", "big", map[string]any{"type": "object"},
		func(map[string]any) (string, error) { return strings.Repeat("x", n), nil })
}

func TestRunRawSkipsShaping(t *testing.T) {
	b := DefaultContextBudget()
	b.MaxOutputBytes = 10
	e := NewExecutor(time.Second, nil, WithContextBudget(b, nil))
	raw, err := e.RunRaw(context.Background(), bigBuiltin(100), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 100 {
		t.Fatalf("RunRaw len = %d, want 100 (unshaped)", len(raw))
	}
	shaped, _ := e.Run(context.Background(), bigBuiltin(100), nil)
	if len(shaped) > 10 {
		t.Fatalf("Run len = %d, want <= 10 (shaped)", len(shaped))
	}
	if e.Shape("big", raw) != shaped {
		t.Fatal("Run must equal Shape(RunRaw)")
	}
}

func TestRunRawTrimsCLIOutput(t *testing.T) {
	e := NewExecutor(5*time.Second, nil)
	def := CLI("echo", "echo", "echo")
	def.Args = []string{"hello"}
	raw, err := e.RunRaw(context.Background(), def, nil)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "hello" {
		t.Fatalf("raw = %q, want %q", raw, "hello")
	}
}

func TestMaxOutputBytes(t *testing.T) {
	if got := NewExecutor(0, nil).MaxOutputBytes("x"); got != 0 {
		t.Fatalf("no budget: got %d, want 0", got)
	}
	b := DefaultContextBudget()
	b.PerTool = map[string]ContextBudget{"small": {MaxOutputBytes: 512}}
	e := NewExecutor(0, nil, WithContextBudget(b, nil))
	if got := e.MaxOutputBytes("small"); got != 512 {
		t.Fatalf("per-tool: got %d, want 512", got)
	}
	if got := e.MaxOutputBytes("other"); got != 262144 {
		t.Fatalf("base: got %d, want 262144", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/tools/ -run 'TestRunRaw|TestMaxOutputBytes'`
Expected: FAIL (`e.RunRaw undefined`, `e.Shape undefined`, `e.MaxOutputBytes undefined`).

- [ ] **Step 3: Implement**

In `pkg/tools/executor.go`:
1. Rename the current `Run` body to `RunRaw`. Change its three success-path returns:
   - builtin: `return e.shape(def.Name, result), nil` → `return result, nil`
   - CLI end: `return e.shape(def.Name, strings.TrimSpace(result)), nil` → `return strings.TrimSpace(result), nil`
   - Leave the failure path `shapedErrMsg := e.shape(def.Name, trimmed)` as is.
2. Rename `shape` → `Shape` (update its one remaining internal call) and change its doc comment to `// Shape applies the configured budget (if any) to output.`
3. Add:

```go
// Run executes a tool and returns budget-shaped output.
func (e *Executor) Run(ctx context.Context, def *Definition, input map[string]any) (string, error) {
	raw, err := e.RunRaw(ctx, def, input)
	if err != nil {
		return "", err
	}
	return e.Shape(def.Name, raw), nil
}

// MaxOutputBytes returns the effective byte cap for toolName, or 0 when no
// budget is configured or the cap is unlimited.
func (e *Executor) MaxOutputBytes(toolName string) int64 {
	if e.budget == nil {
		return 0
	}
	return e.budget.effectiveFor(toolName).MaxOutputBytes
}
```

Update the doc comment on `RunRaw`: `// RunRaw executes a tool and returns its output without success-path shaping. Error messages are still shaped.`

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/tools/ ./pkg/mcp/ ./internal/...`
Expected: PASS, including the existing `executor_shaping_test.go`.

- [ ] **Step 5: Commit**

```bash
git add pkg/tools/executor.go pkg/tools/executor_raw_test.go
git commit -m "refactor: split Executor.Run into RunRaw and Shape

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 4: Return structuredContent for tools with outputSchema

**Files:**
- Create: `pkg/mcp/structured.go`, `pkg/mcp/structured_test.go`
- Modify: `pkg/mcp/bridge.go` (the `addTool` handler closure)
- Test: `pkg/mcp/structured_test.go`, `pkg/mcp/bridge_test.go`

**Interfaces:**
- Consumes: `Executor.RunRaw`, `Executor.MaxOutputBytes` (Task 3).
- Produces: `func structuredResult(toolName, raw string, maxBytes int64) *gomcp.CallToolResult` (package-private).

- [ ] **Step 1: Write failing tests**

`pkg/mcp/structured_test.go` (internal package, so the unexported function is reachable):

```go
package mcp

import (
	"strings"
	"testing"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func textOf(t *testing.T, r *gomcp.CallToolResult) string {
	t.Helper()
	if len(r.Content) != 1 {
		t.Fatalf("content len = %d, want 1", len(r.Content))
	}
	tc, ok := r.Content[0].(*gomcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want *TextContent", r.Content[0])
	}
	return tc.Text
}

func TestStructuredOutputObject(t *testing.T) {
	r := structuredResult("t", `{"n":1}`, 0)
	if r.IsError {
		t.Fatalf("unexpected error: %s", textOf(t, r))
	}
	m, ok := r.StructuredContent.(map[string]any)
	if !ok || m["n"] != float64(1) {
		t.Fatalf("structured = %#v", r.StructuredContent)
	}
	if textOf(t, r) != `{"n":1}` {
		t.Fatalf("text = %q", textOf(t, r))
	}
}

func TestStructuredOutputNonObjectJSON(t *testing.T) { // Review Focus #2
	for _, raw := range []string{`[1,2]`, `"hi"`, `42`, `true`} {
		r := structuredResult("t", raw, 0)
		if r.IsError {
			t.Errorf("%s: unexpected error %s", raw, textOf(t, r))
		}
		if r.StructuredContent == nil {
			t.Errorf("%s: structured content nil", raw)
		}
	}
}

func TestStructuredOutputTrailingNewline(t *testing.T) { // Review Focus #3
	r := structuredResult("t", "{\"n\":1}\n\n", 0)
	if r.IsError {
		t.Fatalf("unexpected error: %s", textOf(t, r))
	}
	if textOf(t, r) != `{"n":1}` {
		t.Fatalf("text = %q, want trimmed", textOf(t, r))
	}
}

func TestStructuredOutputNonJSON(t *testing.T) {
	r := structuredResult("t", "not json", 0)
	if !r.IsError || !strings.Contains(textOf(t, r), "non-JSON") {
		t.Fatalf("want isError non-JSON, got %+v", r)
	}
	if r.StructuredContent != nil {
		t.Fatal("structured content must be nil on error")
	}
}

func TestStructuredOutputOverCap(t *testing.T) {
	r := structuredResult("t", `{"s":"`+strings.Repeat("x", 100)+`"}`, 50)
	if !r.IsError || !strings.Contains(textOf(t, r), "50-byte cap") {
		t.Fatalf("want isError over cap, got %+v", r)
	}
}
```

Append to `pkg/mcp/bridge_test.go`:

```go
func TestStructuredOutputEndToEnd(t *testing.T) {
	r := tools.NewRegistry()
	def := tools.BuiltinTool("stats", "stats", map[string]any{"type": "object"},
		func(map[string]any) (string, error) { return "{\"count\":3}\n", nil })
	def.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{"count": map[string]any{"type": "integer"}}}
	_ = r.Register(def)

	session, _ := startBridge(t, r)
	res, err := session.CallTool(context.Background(), &gomcp.CallToolParams{Name: "stats"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("isError: %+v", res.Content)
	}
	m, ok := res.StructuredContent.(map[string]any)
	if !ok || m["count"] != float64(3) {
		t.Fatalf("structuredContent = %#v", res.StructuredContent)
	}
}

func TestStructuredOutputToolFailure(t *testing.T) { // Review Focus #4
	r := tools.NewRegistry()
	def := tools.BuiltinTool("broken", "broken", map[string]any{"type": "object"},
		func(map[string]any) (string, error) { return "", fmt.Errorf("boom") })
	def.OutputSchema = map[string]any{"type": "object"}
	_ = r.Register(def)

	session, _ := startBridge(t, r)
	res, err := session.CallTool(context.Background(), &gomcp.CallToolParams{Name: "broken"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || res.StructuredContent != nil {
		t.Fatalf("want plain isError, got isError=%v structured=%#v", res.IsError, res.StructuredContent)
	}
	if tc, _ := res.Content[0].(*gomcp.TextContent); tc == nil || !strings.Contains(tc.Text, "boom") {
		t.Fatalf("error text = %+v", res.Content)
	}
}
```

(`bridge_test.go` already imports `fmt`, `strings`, `gomcp` and `tools`.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/mcp/ -run 'TestStructured'`
Expected: FAIL (`undefined: structuredResult`; the end-to-end test sees nil `StructuredContent`).

- [ ] **Step 3: Implement `structuredResult`**

`pkg/mcp/structured.go`:

```go
package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// structuredResult builds a CallToolResult for a tool that declares an
// outputSchema. The raw output must be a single JSON value (any type, per
// SEP-2106). Structured content is never truncated: output over maxBytes
// (when > 0) is an error rather than a corrupted JSON value. A text copy is
// included for clients that ignore structuredContent.
func structuredResult(toolName, raw string, maxBytes int64) *gomcp.CallToolResult {
	trimmed := strings.TrimSpace(raw)
	if maxBytes > 0 && int64(len(trimmed)) > maxBytes {
		return errorResult(fmt.Sprintf(
			"tool %q structured output is %d bytes, exceeding the %d-byte cap; structured output cannot be truncated (raise context_budget.per_tool.%s.max_output_bytes)",
			toolName, len(trimmed), maxBytes, toolName))
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return errorResult(fmt.Sprintf("tool %q declared outputSchema but produced non-JSON output: %v", toolName, err))
	}
	return &gomcp.CallToolResult{
		Content:           []gomcp.Content{&gomcp.TextContent{Text: trimmed}},
		StructuredContent: value,
	}
}
```

- [ ] **Step 4: Route the bridge through it**

In `pkg/mcp/bridge.go` `addTool`, replace the block from `result, err := b.cfg.Executor.Run(ctx, d, input)` to the final `return` with:

```go
		if d.OutputSchema != nil {
			raw, err := b.cfg.Executor.RunRaw(ctx, d, input)
			if err != nil {
				b.logger.Error("tool call failed", "tool", d.Name, "error", err)
				return errorResult(err.Error()), nil
			}
			return structuredResult(d.Name, raw, b.cfg.Executor.MaxOutputBytes(d.Name)), nil
		}

		result, err := b.cfg.Executor.Run(ctx, d, input)
		if err != nil {
			b.logger.Error("tool call failed", "tool", d.Name, "error", err)
			return errorResult(err.Error()), nil
		}

		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: result}},
		}, nil
```

- [ ] **Step 5: Run tests**

Run: `go test ./pkg/mcp/ -v -run TestStructured && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/mcp/structured.go pkg/mcp/structured_test.go pkg/mcp/bridge.go pkg/mcp/bridge_test.go
git commit -m "feat: return structuredContent for tools declaring outputSchema

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 5: Cache hints (ttlMs / cacheScope)

**Files:**
- Create: `pkg/mcp/cache.go`, `pkg/mcp/cache_test.go`
- Modify: `pkg/mcp/bridge.go` (`ServeTransport` server options; the two memory resource handlers)

**Interfaces:**
- Produces: `const listTTLMs = 3_600_000` and `func setCacheable(ctx context.Context, req gomcp.Request, c *gomcp.Cacheable)`, both package-private.
- Consumes: v1.8.0 `gomcp.ServerOptions.SetCacheable`, `gomcp.Cacheable{TTLMs int; CacheScope string}`, request aliases `*gomcp.ListToolsRequest`, `*gomcp.ListPromptsRequest`, `*gomcp.ListResourcesRequest`, `*gomcp.ListResourceTemplatesRequest`.

- [ ] **Step 1: Write failing tests**

`pkg/mcp/cache_test.go`:

```go
package mcp_test

import (
	"context"
	"testing"
	"time"

	agentmcp "github.com/teabranch/abbyfile/pkg/mcp"
	"github.com/teabranch/abbyfile/pkg/memory"
	"github.com/teabranch/abbyfile/pkg/tools"
)

func TestListResultsAreCacheable(t *testing.T) {
	session, _ := startBridge(t, tools.NewRegistry())
	ctx := context.Background()

	tl, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tl.TTLMs != 3_600_000 || tl.CacheScope != "public" {
		t.Errorf("tools/list cache = %d/%q, want 3600000/public", tl.TTLMs, tl.CacheScope)
	}
	pl, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pl.TTLMs != 3_600_000 || pl.CacheScope != "public" {
		t.Errorf("prompts/list cache = %d/%q", pl.TTLMs, pl.CacheScope)
	}
}

func TestMemoryReadIsPrivateAndUncached(t *testing.T) {
	store, err := memory.NewFileStoreAt(t.TempDir(), memory.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	mgr := memory.NewManager(store)
	if err := mgr.Set("k", "v"); err != nil {
		t.Fatal(err)
	}
	session, _ := startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name: "test-agent", Version: "v0.1.0",
		Registry: tools.NewRegistry(), Executor: tools.NewExecutor(30*time.Second, nil),
		Loader: newTestLoader(t), Memory: mgr,
	})
	res, err := session.ReadResource(context.Background(), &gomcp.ReadResourceParams{URI: "memory://test-agent/k"})
	if err != nil {
		t.Fatal(err)
	}
	if res.TTLMs != 0 || res.CacheScope != "private" {
		t.Errorf("memory read cache = %d/%q, want 0/private", res.TTLMs, res.CacheScope)
	}
}
```

Add the `gomcp` import (`gomcp "github.com/modelcontextprotocol/go-sdk/mcp"`). The memory calls (`memory.NewFileStoreAt`, `memory.NewManager`, `mgr.Set`) match `TestBridgeMemoryResources` in `bridge_test.go:262`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/mcp/ -run 'TestListResultsAreCacheable|TestMemoryReadIsPrivate'`
Expected: FAIL (TTL 0, scope `public`, which are the SDK defaults).

- [ ] **Step 3: Implement**

`pkg/mcp/cache.go`:

```go
package mcp

import (
	"context"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// listTTLMs is the cache lifetime for list results. Tools, prompts, and
// resource listings are fixed for the lifetime of a binary, so any client
// may cache them for an hour.
const listTTLMs = 3_600_000

// setCacheable is the ServerOptions.SetCacheable hook. It runs with the
// server mutex held and must not call back into the server.
func setCacheable(_ context.Context, req gomcp.Request, c *gomcp.Cacheable) {
	switch req.(type) {
	case *gomcp.ListToolsRequest, *gomcp.ListPromptsRequest,
		*gomcp.ListResourcesRequest, *gomcp.ListResourceTemplatesRequest:
		c.TTLMs = listTTLMs
		c.CacheScope = "public"
	}
}
```

In `pkg/mcp/bridge.go` `ServeTransport`, extend the options:

```go
	}, &gomcp.ServerOptions{
		Instructions: instructions,
		SetCacheable: setCacheable,
	})
```

In `addMemoryResources`, set cache fields on **both** `ReadResourceResult` literals:

```go
		return &gomcp.ReadResourceResult{
			Cacheable: gomcp.Cacheable{TTLMs: 0, CacheScope: "private"},
			Contents:  []*gomcp.ResourceContents{{ /* unchanged */ }},
		}, nil
```

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/mcp/ -v -run 'Cache|Memory' && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/mcp/cache.go pkg/mcp/cache_test.go pkg/mcp/bridge.go
git commit -m "feat: emit MCP ttlMs/cacheScope hints on lists and memory reads

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 6: Dual-era conformance tests and stdout hygiene

These tests pin behavior that should already work after Tasks 1–5. If one fails, it has found a real conformance bug: fix the bridge, not the test.

**Files:**
- Modify: `pkg/mcp/bridge_test.go` (add `startBridgeEra`)
- Create: `pkg/mcp/conformance_test.go`, `internal/integration/stdio_test.go`

**Interfaces:**
- Produces: test helper `startBridgeEra(t *testing.T, cfg agentmcp.BridgeConfig, protocolVersion string) *gomcp.ClientSession` (an empty version means latest).
- Consumes: `gomcp.ClientSessionOptions{ProtocolVersion string}`, `(*gomcp.ClientSession).InitializeResult()` (populated from `server/discover` in the modern era).

- [ ] **Step 1: Write the tests**

Add to `pkg/mcp/bridge_test.go`, below `startBridgeWithConfig`:

```go
// startBridgeEra is startBridgeWithConfig with an explicit client protocol
// version ("" = SDK latest, i.e. 2026-07-28 via server/discover).
func startBridgeEra(t *testing.T, cfg agentmcp.BridgeConfig, protocolVersion string) *gomcp.ClientSession {
	t.Helper()
	bridge := agentmcp.NewBridge(cfg)
	serverTransport, clientTransport := gomcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	go func() { _ = bridge.ServeTransport(ctx, serverTransport) }()
	client := gomcp.NewClient(&gomcp.Implementation{Name: "era-client", Version: "v0.1.0"}, nil)
	sess, err := client.Connect(ctx, clientTransport, &gomcp.ClientSessionOptions{ProtocolVersion: protocolVersion})
	if err != nil {
		cancel()
		t.Fatalf("connect (%q): %v", protocolVersion, err)
	}
	t.Cleanup(func() { sess.Close(); cancel() })
	return sess
}
```

`pkg/mcp/conformance_test.go`:

```go
package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	agentmcp "github.com/teabranch/abbyfile/pkg/mcp"
	"github.com/teabranch/abbyfile/pkg/tools"
)

func eraConfig(t *testing.T) agentmcp.BridgeConfig {
	return agentmcp.BridgeConfig{
		Name: "test-agent", Version: "v0.1.0", Description: "Era test agent",
		Registry: wireRegistry(), Executor: tools.NewExecutor(30*time.Second, nil),
		Loader: newTestLoader(t),
	}
}

func TestDualEra(t *testing.T) { // Review Focus #5 covers the 2025-06-18 row
	eras := []struct{ ask, want string }{
		{"", "2026-07-28"},
		{"2025-11-25", "2025-11-25"},
		{"2025-06-18", "2025-06-18"},
	}
	var baseline []byte
	for _, era := range eras {
		t.Run("era="+era.want, func(t *testing.T) {
			sess := startBridgeEra(t, eraConfig(t), era.ask)
			ir := sess.InitializeResult()
			if ir == nil || ir.ProtocolVersion != era.want {
				t.Fatalf("negotiated %+v, want %s", ir, era.want)
			}
			if !strings.Contains(ir.Instructions, "Era test agent") {
				t.Errorf("instructions not delivered: %q", ir.Instructions)
			}
			tl, err := sess.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := json.Marshal(tl.Tools)
			if baseline == nil {
				baseline = got
			} else if string(got) != string(baseline) {
				t.Errorf("tools/list differs across eras:\n%s\nvs\n%s", got, baseline)
			}
			res, err := sess.CallTool(context.Background(), &gomcp.CallToolParams{Name: "alpha"})
			if err != nil || res.IsError {
				t.Fatalf("call alpha: err=%v res=%+v", err, res)
			}
		})
	}
}

func TestToolsListInvariantAcrossConnections(t *testing.T) {
	a := startBridgeEra(t, eraConfig(t), "")
	b := startBridgeEra(t, eraConfig(t), "")
	la, _ := a.ListTools(context.Background(), nil)
	lb, _ := b.ListTools(context.Background(), nil)
	ja, _ := json.Marshal(la.Tools)
	jb, _ := json.Marshal(lb.Tools)
	if string(ja) != string(jb) {
		t.Fatalf("tools/list varies per connection")
	}
	for i := 1; i < len(la.Tools); i++ {
		if la.Tools[i-1].Name > la.Tools[i].Name {
			t.Fatalf("tools not sorted: %q before %q", la.Tools[i-1].Name, la.Tools[i].Name)
		}
	}
}

func TestInputValidationIsToolError(t *testing.T) {
	r := tools.NewRegistry()
	_ = r.Register(tools.BuiltinTool("needs_q", "needs q",
		map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}, "required": []string{"q"}},
		func(map[string]any) (string, error) { return "ok", nil }))
	cfg := eraConfig(t)
	cfg.Registry = r
	for _, era := range []string{"", "2025-11-25"} {
		sess := startBridgeEra(t, cfg, era)
		res, err := sess.CallTool(context.Background(), &gomcp.CallToolParams{Name: "needs_q", Arguments: map[string]any{}})
		if err != nil {
			t.Fatalf("era %q: protocol error %v, want isError result", era, err)
		}
		if !res.IsError {
			t.Fatalf("era %q: IsError=false for missing required arg", era)
		}
	}
}
```

`internal/integration/stdio_test.go`. It reuses `binaryPath` from `agent_test.go`'s `TestMain`:

```go
//go:build integration

package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"testing"
	"time"
)

// TestStdoutIsOnlyJSONRPC drives serve-mcp with raw legacy-era JSON-RPC and
// asserts every stdout line is a JSON-RPC 2.0 message (logs must go to stderr).
func TestStdoutIsOnlyJSONRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath, "serve-mcp")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	msgs := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"raw","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"does_not_exist","arguments":{}}}`,
	}
	for _, m := range msgs {
		if _, err := io.WriteString(stdin, m+"\n"); err != nil {
			t.Fatal(err)
		}
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	seen := map[float64]bool{}
	for sc.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			t.Fatalf("non-JSON on stdout: %q", sc.Text())
		}
		if msg["jsonrpc"] != "2.0" {
			t.Fatalf("stdout message without jsonrpc 2.0: %q", sc.Text())
		}
		if id, ok := msg["id"].(float64); ok {
			seen[id] = true
		}
		if seen[1] && seen[2] && seen[3] {
			return
		}
	}
	t.Fatalf("stream ended before responses 1-3; seen=%v err=%v", seen, sc.Err())
}
```

- [ ] **Step 2: Run the unit conformance tests**

Run: `go test ./pkg/mcp/ -v -run 'TestDualEra|TestToolsListInvariant|TestInputValidationIsToolError'`
Expected: PASS. If `TestDualEra/era=2026-07-28` fails with empty instructions, check that `ServerOptions.Instructions` still feeds `DiscoverResult.Instructions` in v1.8.0 (`server.go` discover handler) and fix the bridge.

- [ ] **Step 3: Run the integration test**

Run: `make integration`
Expected: PASS, including `TestStdoutIsOnlyJSONRPC`.

- [ ] **Step 4: Run everything with the race detector**

Run: `go vet ./... && go test -race ./... && make integration`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/mcp/bridge_test.go pkg/mcp/conformance_test.go internal/integration/stdio_test.go
git commit -m "test: pin dual-era MCP conformance and stdout hygiene

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 7: Docs for Phase A

**Files:**
- Modify: `docs/guides/mcp.md`, `README.md` (Go badge only if needed)

- [ ] **Step 1: Update `docs/guides/mcp.md`**

Add a `## Protocol Versions` section after "What `serve-mcp` Exposes":

```markdown
## Protocol Versions

Agent binaries speak MCP **2026-07-28** (stateless: `server/discover` plus
per-request `_meta`) and still accept legacy `initialize` clients on
**2025-11-25** and **2025-06-18**. The client picks the version; no
configuration is needed.

- Server instructions are returned by `server/discover` (modern) or
  `initialize` (legacy).
- `tools/list`, `prompts/list` and resource listings are cacheable for one
  hour (`ttlMs: 3600000`, `cacheScope: public`). Memory reads are never
  cached (`ttlMs: 0`, `cacheScope: private`).
- Tools that declare an `outputSchema` return `structuredContent`, plus a
  JSON text copy. Their output must be one JSON value within the tool's
  `max_output_bytes`; structured output is never truncated.
- Tool names must match `^[A-Za-z0-9_.-]{1,128}$`.
```

- [ ] **Step 2: Commit**

```bash
git add docs/guides/mcp.md
git commit -m "docs: document MCP protocol versions, caching, structured output

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Notes for later phases (not Phase A work)

- `pkg/builder/templates/go.mod.tmpl` pins generated agents to the published `github.com/teabranch/abbyfile {{.ModuleVersion}}`. The SDK reaches generated agents transitively, so they only get v1.8.0 after an abbyfile release that includes this phase and a bump of the builder's module version. Do this as part of the v0.10.0 release, not here.
- Frontmatter `custom_tools` can't declare `output_schema` today, so structured output is only reachable from the Go library API. Adding frontmatter support is a separate change outside this spec.
