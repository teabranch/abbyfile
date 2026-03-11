# Reference

Complete reference for the Agentfile framework: options, subcommands, flags, and types.

## `agent.Option` Functions

All options are in `pkg/agent/options.go`.

### `WithName(name string) Option`

**Required.** Sets the agent name. Used for the CLI binary name, memory directory (`~/.agentfile/<name>/`), override path, and MCP server identity.

### `WithVersion(version string) Option`

**Required.** Sets the semantic version. Surfaces via `--version`, `--describe`, and MCP handshake.

### `WithDescription(desc string) Option`

Sets a short description. Shows in `--describe` JSON, MCP server metadata, and the Cobra help text.

### `WithPromptFS(fs embed.FS, path string) Option`

**Required.** Sets the embedded filesystem and the path within it for the system prompt. This is set automatically by `agentfile build` — you do not need to call it directly.

### `WithTools(defs ...*tools.Definition) Option`

Registers tool definitions. Variadic -- accepts multiple definitions. Can be called multiple times; definitions accumulate.

```go
agent.WithTools(
    tools.CLI("date", "date", "Get the current date"),
    myBuiltinTool(),
)
```

### `WithToolTimeout(d time.Duration) Option`

Sets the timeout for tool execution. Default: `30 * time.Second`. Applies to both CLI and builtin tools.

### `WithMemory(enabled bool) Option`

Enables or disables persistent memory. Default: `false`. When enabled, creates a `FileStore` at `~/.agentfile/<name>/memory/` and registers four memory tools (`memory_read`, `memory_write`, `memory_list`, `memory_delete`).

### `WithMemoryLimits(limits memory.Limits) Option`

Sets capacity limits for the memory store. Only meaningful when memory is enabled.

### `WithModel(model string) Option`

Sets the agent's model hint. This is informational metadata — the runtime (Claude Code, etc.) picks its own model. The value is surfaced in `--describe` JSON and as a "Model Preference" hint in MCP server instructions.

### `WithLazyToolLoading(enabled bool) Option`

Enables lazy tool loading via the `search_tools` meta-tool. When enabled, the MCP server only registers `search_tools` and `get_instructions` initially; clients discover other tools by searching.

### `WithConfigPath(path string) Option`

Overrides the default config.yaml location (`~/.agentfile/<name>/config.yaml`). Primarily useful for testing.

### `WithLogger(logger *slog.Logger) Option`

Sets the structured logger. Default: `slog.NewTextHandler(os.Stderr, nil)`. Logs go to stderr so they do not interfere with MCP protocol on stdout.

---

## CLI Subcommands and Flags

### Root Command

```
Usage:
  <agent-name> [flags]
  <agent-name> [command]

Flags:
  --version               Print version and exit
  --describe              Print agent manifest as JSON and exit
  --custom-instructions   Print the system prompt and exit
  -h, --help              Help for the agent
```

### `run-tool <name>`

Execute a registered tool by name.

```
Usage:
  <agent-name> run-tool <name> [flags]

Flags:
  --input string    Tool input as JSON object
```

Examples:

```bash
./my-agent run-tool date
./my-agent run-tool read_file --input '{"path": "go.mod"}'
./my-agent run-tool go_test --input '{"package": "./pkg/tools/..."}'
```

### `memory`

Manage persistent memory. Only available when memory is enabled.

```
Usage:
  <agent-name> memory [command]

Commands:
  read <key>              Read a value from memory
  write <key> <value>     Write a value to memory (overwrites existing)
  append <key> <value>    Append content to an existing memory key
  list                    List all memory keys
  delete <key>            Delete a key from memory
```

### `config`

Inspect and modify runtime configuration overrides stored at `~/.agentfile/<name>/config.yaml`.

```
Usage:
  <agent-name> config [command]

Commands:
  get [field]             Show configuration (compiled defaults + overrides)
  set <field> <value>     Set a config override
  reset <field>           Remove an override, reverting to compiled default
  path                    Print the config file path
```

Examples:

```bash
./my-agent config get                   # show all fields with source
./my-agent config get model             # show just model
./my-agent config set model opus        # set override
./my-agent config set tool_timeout 120s # set timeout override
./my-agent config reset model           # revert to compiled default
./my-agent config path                  # ~/.agentfile/my-agent/config.yaml
```

Output format for `get`:

```
model: opus (override)
tool_timeout: 30s (compiled)
```

Supported fields for `set`/`reset`: `model`, `tool_timeout`. Complex fields (`memory_limits`, `command_policy`) can be set by editing the YAML directly.

When `reset` removes the last field, the config file is deleted.

### `serve-mcp`

Start an MCP-over-stdio server.

```
Usage:
  <agent-name> serve-mcp
```

No flags. The server runs until the stdin stream closes or the process is killed. Logs go to stderr.

### `validate`

Check that the agent is configured correctly.

```
Usage:
  <agent-name> validate
```

Checks performed:
- **Prompt**: loads the system prompt (embedded or override)
- **Tools**: for CLI tools, verifies the command exists in PATH; for builtin tools, verifies the handler is non-nil
- **Memory**: if enabled, verifies the memory directory is writable
- **Override**: reports whether an override file is active
- **Version**: verifies the version string is set

Output format:

```
[PASS] Prompt: loaded (245 bytes)
[PASS] Tool "date": command "date" found at /bin/date
[PASS] Tool "read_file": builtin handler registered
[PASS] Memory: directory /Users/you/.agentfile/my-agent/memory is writable
[INFO] Override: not active (using embedded prompt)
[PASS] Version: 0.1.0
----------------------------------------
Validation PASSED
```

---

## `--describe` JSON Schema

```json
{
  "name": "string",
  "version": "string",
  "description": "string",
  "model": "string",
  "toolTimeout": "30s",
  "tools": [
    {
      "name": "string",
      "description": "string",
      "builtin": false,
      "inputSchema": { },
      "annotations": {
        "ReadOnlyHint": false,
        "DestructiveHint": null,
        "IdempotentHint": false,
        "OpenWorldHint": null,
        "Title": ""
      }
    }
  ],
  "memory": true,
  "memoryLimits": {
    "maxKeys": 0,
    "maxValueBytes": 0,
    "maxTotalBytes": 0
  }
}
```

Notes:
- `model` is only present if set (compiled default or config override)
- `toolTimeout` is only present if non-default
- `tools` includes both user-registered tools and memory tools (if enabled)
- `memoryLimits` is only present when memory is enabled and limits are set
- `annotations` is only present when set on the tool definition
- `builtin` is `true` for builtin tools and memory tools, `false` for CLI tools

---

## `tools.CLI`

```go
func CLI(name, command, description string) *Definition
```

Creates a `Definition` for a CLI tool that runs `command` as a subprocess.

Generated input schema:

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

The returned `Definition` can be further configured:
- `def.Args = []string{...}` -- set default arguments
- `def.WithAnnotations(&tools.Annotations{...})` -- set MCP hints

---

## `tools.BuiltinTool`

```go
func BuiltinTool(name, description string, schema any, handler func(input map[string]any) (string, error)) *Definition
```

Creates a `Definition` for a builtin tool that runs the `handler` function in-process.

Parameters:
- `name` -- unique tool name
- `description` -- shown to the LLM for tool selection
- `schema` -- JSON Schema as `map[string]any` (or `nil` for no input)
- `handler` -- function that receives parsed JSON input and returns a string result

---

## `tools.Definition`

```go
type Definition struct {
    Name        string
    Description string
    InputSchema any
    Annotations *Annotations
    Builtin     bool
    Command     string              // CLI tools only
    Args        []string            // CLI tools only, default arguments
    Handler     func(input map[string]any) (string, error)  // builtin tools only
}
```

Methods:

### `def.WithAnnotations(a *Annotations) *Definition`

Sets MCP annotation hints. Returns the definition for chaining.

### `def.ValidateInput(input map[string]any) error`

Validates input against the `InputSchema`. Checks required fields and property types. Returns `nil` if valid or if schema is nil.

---

## `tools.Annotations`

```go
type Annotations struct {
    ReadOnlyHint    bool    // tool does not modify state
    DestructiveHint *bool   // nil = MCP default (true)
    IdempotentHint  bool    // safe to call multiple times
    OpenWorldHint   *bool   // nil = MCP default (true)
    Title           string  // human-readable name
}
```

Use `tools.BoolPtr(b bool) *bool` to set pointer fields:

```go
DestructiveHint: tools.BoolPtr(false),
OpenWorldHint:   tools.BoolPtr(false),
```

---

## `tools.Executor`

```go
func NewExecutor(timeout time.Duration, logger *slog.Logger) *Executor
```

Creates an executor with the given timeout and logger. Zero timeout defaults to 30 seconds. Nil logger disables logging.

```go
func (e *Executor) Run(ctx context.Context, def *Definition, input map[string]any) (string, error)
```

Runs a tool. For CLI tools, executes the command as a subprocess. For builtin tools, calls the handler. Returns trimmed stdout (or stderr if stdout is empty) for CLI tools.

---

## `tools.Registry`

```go
func NewRegistry() *Registry
```

```go
func (r *Registry) Register(def *Definition) error    // add a tool (error if name empty or duplicate)
func (r *Registry) Get(name string) *Definition        // get by name (nil if not found)
func (r *Registry) All() []*Definition                 // all registered tools
```

---

## `memory.Limits`

```go
type Limits struct {
    MaxKeys       int   `json:"maxKeys,omitempty"`       // max number of keys (0 = unlimited)
    MaxValueBytes int64 `json:"maxValueBytes,omitempty"` // max bytes per value (0 = unlimited)
    MaxTotalBytes int64 `json:"maxTotalBytes,omitempty"` // max total bytes (0 = unlimited)
}
```

Zero values mean unlimited. Pass the zero value `memory.Limits{}` for no limits.

---

## `memory.FileStore`

```go
func NewFileStore(agentName string, limits Limits) (*FileStore, error)
```

Creates a file store at `~/.agentfile/<agentName>/memory/`. Creates the directory if it does not exist.

```go
func NewFileStoreAt(dir string, limits Limits) (*FileStore, error)
```

Creates a file store at a specific directory. Used for testing.

```go
func (s *FileStore) Read(key string) (string, error)
func (s *FileStore) Write(key, content string) error
func (s *FileStore) Append(key, content string) error
func (s *FileStore) Delete(key string) error
func (s *FileStore) Keys() ([]string, error)
```

Keys must not be empty or contain path separators. Each key is stored as `<dir>/<key>.md`.

---

## `memory.Manager`

```go
func NewManager(store *FileStore) *Manager
```

Wraps a `FileStore` with a `sync.RWMutex` for concurrent access.

```go
func (m *Manager) Get(key string) (string, error)
func (m *Manager) Set(key, value string) error
func (m *Manager) Append(key, value string) error
func (m *Manager) Delete(key string) error
func (m *Manager) Keys() ([]string, error)
func (m *Manager) Tools() []*tools.Definition
func (m *Manager) FormatKeysAsContext() string
```

`Tools()` returns four builtin tool definitions: `memory_read`, `memory_write`, `memory_list`, `memory_delete`.

`FormatKeysAsContext()` returns a string like `"Available memory keys: notes, config"` or empty string if no keys.

---

## `prompt.Loader`

```go
func NewLoader(agentName string, fs embed.FS, path string) *Loader
```

Created automatically by generated binaries. The `embed.FS` is populated by `agentfile build`.

```go
func (l *Loader) Load() (string, error)       // load prompt (override or embedded)
func (l *Loader) IsOverridden() bool           // true if override file exists
func (l *Loader) OverridePath() string         // ~/.agentfile/<name>/override.md
```

---

## `mcp.Bridge`

```go
func NewBridge(cfg BridgeConfig) *Bridge
```

```go
type BridgeConfig struct {
    Name            string
    Version         string
    Description     string
    Model           string          // model hint, appended to instructions
    Registry        *tools.Registry
    Executor        *tools.Executor
    Loader          *prompt.Loader
    Memory          *memory.Manager // nil if memory disabled
    Logger          *slog.Logger    // nil disables logging
    LazyToolLoading bool            // only register search_tools initially
}
```

```go
func (b *Bridge) Serve(ctx context.Context) error                            // stdio transport
func (b *Bridge) ServeTransport(ctx context.Context, transport gomcp.Transport) error  // any transport
```

---

## `agentfile build`

```
Usage:
  agentfile build [flags]

Flags:
  -f, --file string     Path to Agentfile (default: auto-detect Agentfile or agentfile.yaml)
  -o, --output string   Output directory for binaries (default: "./build")
      --agent string    Build a single agent by name
      --plugin          Also generate a Claude Code plugin directory
```

Parses the Agentfile, generates Go source from each agent's `.md` file, and compiles standalone binaries. Also generates/updates `.mcp.json`.

When `--plugin` is passed, each agent also gets a `<name>.claude-plugin/` directory in the output folder containing the binary, an MCP config, and any declared skills. See [Plugins guide](guides/plugins.md).

## `agentfile install`

```
Usage:
  agentfile install <agent-name | github.com/owner/repo[/agent][@version]> [flags]

Flags:
  -g, --global        Install globally to /usr/local/bin
      --model string  Override the agent's model in ~/.agentfile/<name>/config.yaml
```

Installs an agent binary and wires it into MCP. Supports two modes:

**Local install** (from `./build/`):

```bash
agentfile install my-agent
agentfile install -g my-agent
```

**Remote install** (from GitHub Releases):

```bash
agentfile install github.com/owner/repo/agent
agentfile install github.com/owner/repo/agent@1.0.0
```

Remote install downloads the binary for the current platform (`<agent>-<GOOS>-<GOARCH>`), verifies it with `--describe`, installs it, wires MCP, and tracks it in the registry. Set `GITHUB_TOKEN` for private repos.

Both local and remote installs are tracked in `~/.agentfile/registry.json`.

## `agentfile publish`

```
Usage:
  agentfile publish [flags]

Flags:
  -f, --file string     Path to Agentfile (default: auto-detect Agentfile or agentfile.yaml)
      --agent string    Publish a single agent by name
      --dry-run         Cross-compile only, skip GitHub Release creation
```

Cross-compiles agent binaries for 4 platforms (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64) and creates a GitHub Release via the `gh` CLI.

Release tag format: `<agent>/v<version>`. Binary asset naming: `<agent>-<os>-<arch>`.

Requires the `gh` CLI to be installed and authenticated.

## `agentfile list`

```
Usage:
  agentfile list
```

Shows all installed agents from the registry (`~/.agentfile/registry.json`). Displays name, version, source, scope, and path in a table.

## `agentfile update`

```
Usage:
  agentfile update [agent-name]
```

Checks GitHub Releases for newer versions of installed agents and downloads updates. Only agents installed from a remote source can be updated.

If no agent name is given, checks all remote-installed agents.

## `agentfile uninstall`

```
Usage:
  agentfile uninstall <agent-name>
```

Removes an installed agent: deletes the binary, removes the MCP entry from `.mcp.json` (or `~/.claude/mcp.json` for global installs), and removes the entry from the registry.

---

## `registry.Entry`

```go
type Entry struct {
    Name        string `json:"name"`
    Source      string `json:"source"`      // "local" or "github.com/owner/repo/agent"
    Version     string `json:"version"`
    Path        string `json:"path"`        // absolute path to installed binary
    Scope       string `json:"scope"`       // "local" or "global"
    InstalledAt string `json:"installedAt"` // RFC3339 timestamp
}
```

## `registry.Registry`

```go
func DefaultPath() (string, error)         // ~/.agentfile/registry.json
func Load(path string) (*Registry, error)  // load from disk (empty if not exists)
func (r *Registry) Save() error            // atomic save (write temp + rename)
func (r *Registry) Set(e Entry)            // add or update entry
func (r *Registry) Get(name string) (Entry, bool)
func (r *Registry) Remove(name string)
func (r *Registry) List() []Entry
```

---

## `github.ReleaseRef`

```go
type ReleaseRef struct {
    Owner   string // repository owner
    Repo    string // repository name
    Agent   string // agent name (defaults to repo name)
    Version string // specific version or "" for latest
}
```

## `github.Client`

```go
func NewClient() *Client                   // reads GITHUB_TOKEN from env
func ParseRef(ref string) (ReleaseRef, error)
func IsRemoteRef(ref string) bool
func ResolveAssetName(agentName string) string  // <name>-<GOOS>-<GOARCH>
func FindAsset(release *Release, agentName string) (*Asset, error)
func VersionFromTag(tag string) string
func CompareVersions(a, b string) (int, error)  // -1, 0, or 1
```

```go
func (c *Client) LatestRelease(ctx context.Context, ref ReleaseRef) (*Release, error)
func (c *Client) GetRelease(ctx context.Context, ref ReleaseRef) (*Release, error)
func (c *Client) DownloadAsset(ctx context.Context, asset Asset, w io.Writer) error
```

---

## `builder.BuildConfig`

```go
type BuildConfig struct {
    OutputDir  string // directory for compiled binaries
    ModuleDir  string // local agentfile module path (for replace directive)
    TargetOS   string // GOOS for cross-compilation (empty = native)
    TargetArch string // GOARCH for cross-compilation (empty = native)
}
```

When `TargetOS` and `TargetArch` are set, the binary name becomes `<agent>-<os>-<arch>` and `CGO_ENABLED=0` is set for static cross-compilation.

---

## `definition.SkillDef`

```go
type SkillDef struct {
    Name        string `yaml:"name"`
    Description string `yaml:"description"`
    Path        string `yaml:"path"` // relative to agent .md file
}
```

Declared in block 2 of agent `.md` files under `skills:`. Used by `--plugin` to generate skill files in the plugin directory. All three fields are required.

---

## `plugin.GenerateConfig`

```go
type GenerateConfig struct {
    OutputDir  string // parent directory (e.g., "build")
    BinaryPath string // path to the compiled binary
}
```

## `plugin.SkillFile`

```go
type SkillFile struct {
    Name        string
    Description string
    Content     string // markdown body
}
```

## `plugin.Generate`

```go
func Generate(def *definition.AgentDef, skills []SkillFile, cfg GenerateConfig) error
```

Creates a `<outputDir>/<name>.claude-plugin/` directory containing:

- `.claude-plugin/plugin.json` — name, version, description, `"agentfile": true`
- `.mcp.json` — `{ "mcpServers": { "<name>": { "command": "./<name>", "args": ["serve-mcp"] } } }`
- `<name>` — copy of the compiled binary (executable)
- `skills/<skill-name>/SKILL.md` — for each skill, with frontmatter (name, description) + content
