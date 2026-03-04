# Agentfile

A Go framework for packaging AI agent logic as executable CLI binaries. Declare agents in YAML + markdown, run `agentfile build`, and get standalone binaries — zero Go code required. Claude Code is the LLM runtime — the binary is a packaging format that exposes everything through CLI subcommands and an MCP server.

## Architecture

```
Claude Code (LLM Runtime)
  |
  |  MCP-over-stdio
  v
Agent Binary (Agentfile)
  +-- --version              -> semver
  +-- --describe             -> JSON manifest
  +-- --custom-instructions  -> system prompt
  +-- run-tool <name>        -> execute a tool
  +-- memory read|write|...  -> persistent state
  +-- serve-mcp              -> MCP server
  +-- validate               -> check wiring
```

The binary does **not** call the Claude API. Claude Code loads the agent's prompt, discovers its tools via MCP, and handles all reasoning.

## Install

Requires **Go 1.24+**.

```bash
# Clone and build the agentfile CLI
git clone https://github.com/teabranch/agentfile.git
cd agentfile
make build            # → build/agentfile

# (Optional) Install to PATH
make install          # → /usr/local/bin/agentfile
# Or with a custom prefix:
make install PREFIX=~/.local
```

## Quick Start

### 1. Create an `Agentfile` (or `agentfile.yaml`)

```yaml
version: "1"
agents:
  my-agent:
    path: agents/my-agent.md
    version: 0.1.0
```

### 2. Write the agent `.md` file

Agent files use **dual frontmatter** — two `---` blocks followed by the system prompt:

```markdown
---
name: my-agent
memory: project
---

---
description: "A helpful coding assistant"
tools: Read, Write, Bash
---

You are a helpful coding assistant. Use your tools to read and modify files.
```

**Block 1** — identity: `name`, `memory` (set to any value to enable)
**Block 2** — capabilities: `tools` (comma-separated), `description`
**Body** — the system prompt embedded in the binary

### 3. Build

```bash
agentfile build
# Building my-agent...
#   → ./build/my-agent
# Updated .mcp.json
```

### 4. Verify

```bash
./build/my-agent --version             # my-agent v0.1.0
./build/my-agent validate              # check all wiring
./build/my-agent --describe            # JSON manifest
./build/my-agent --custom-instructions # print system prompt
```

### 5. Connect to Claude Code

`agentfile build` auto-generates `.mcp.json`. Claude Code picks it up automatically:

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

Or install the binary and register it:

```bash
agentfile install my-agent        # → .agentfile/bin/ + .mcp.json
agentfile install -g my-agent     # → /usr/local/bin/ + ~/.claude/mcp.json
```

Install directly from GitHub Releases:

```bash
agentfile install github.com/owner/repo/my-agent        # latest version
agentfile install github.com/owner/repo/my-agent@1.0.0  # specific version
```

## Built-in Tools

Agents declare tools by Claude Code name in their `.md` frontmatter:

| Declare | MCP tool | Description |
|---------|----------|-------------|
| `Read` | `read_file` | Read file contents |
| `Write` | `write_file` | Write file with dir creation |
| `Edit` | `edit_file` | Find-and-replace in file |
| `Bash` | `run_command` | Shell command execution |
| `Glob` | `glob_files` | File pattern matching |
| `Grep` | `grep_search` | Regex content search |

Memory tools (`memory_read`, `memory_write`, `memory_list`, `memory_delete`) are added automatically when `memory` is set.

## CLI Reference

```bash
# Build
agentfile build                   # build all agents (auto-finds Agentfile or agentfile.yaml)
agentfile build --agent my-agent  # build a single agent
agentfile build -o ./dist         # custom output directory

# Install
agentfile install my-agent                            # install locally from ./build/
agentfile install -g my-agent                         # install globally (/usr/local/bin/)
agentfile install github.com/owner/repo/agent         # install from GitHub Releases
agentfile install github.com/owner/repo/agent@1.0.0   # specific version

# Publish
agentfile publish                 # cross-compile + create GitHub Release
agentfile publish --agent my-agent
agentfile publish --dry-run       # cross-compile only, no release

# Manage
agentfile list                    # show installed agents
agentfile update                  # update all remote agents
agentfile update my-agent         # update a specific agent
agentfile uninstall my-agent      # remove binary + MCP entry + registry
```

## Documentation

- **[Agentfile Format](docs/guides/agentfile-format.md)** — Agentfile YAML + agent .md format reference
- **[Quickstart](docs/quickstart.md)** — build an agent in 5 minutes
- **[Concepts](docs/concepts.md)** — architecture and mental model
- **[Tools Guide](docs/guides/tools.md)** — CLI tools, builtin tools, annotations
- **[Memory Guide](docs/guides/memory.md)** — persistent key-value storage
- **[Prompts Guide](docs/guides/prompts.md)** — embedding and overriding prompts
- **[Distribution Guide](docs/guides/distribution.md)** — publish, install, update, uninstall
- **[MCP Integration](docs/guides/mcp.md)** — Claude Code integration via MCP
- **[Testing Guide](docs/guides/testing.md)** — unit, integration, and MCP testing
- **[Reference](docs/reference.md)** — all options, subcommands, flags, types
- **[FAQ](docs/faq.md)** — common questions answered directly

## Project Structure

```
Agentfile           Manifest declaring agents to build (also accepts agentfile.yaml)
.claude/agents/     Agent .md files (prompt + frontmatter)
build/              Compiled binaries (agentfile CLI + agents)

pkg/agent/          Core runtime: New(), Execute(), functional options
pkg/builtins/       Shared tool implementations (read, write, edit, bash, glob, grep)
pkg/definition/     Agentfile YAML + agent .md parser
pkg/builder/        Code generation + go build compilation
pkg/tools/          Tool registry, executor, validation
pkg/memory/         File-based KV store, limits, concurrency-safe manager
pkg/prompt/         Embed.FS loader with override support
pkg/mcp/            MCP-over-stdio bridge
pkg/registry/       Installed agents tracking (~/.agentfile/registry.json)
pkg/github/         GitHub Releases client for remote install/update
internal/cli/       Cobra commands: root, run-tool, memory, serve-mcp, validate
cmd/agentfile/      CLI: build, install, publish, list, update, uninstall
```

## Development

```bash
make all          # fmtcheck → vet → test → build
make agents       # build agent binaries from Agentfile
make integration  # end-to-end tests against built binary
make install      # install agentfile CLI to /usr/local/bin
make clean        # remove build artifacts
```

## Testing

```bash
# Unit tests
make test

# Integration tests (builds CLI + test agent, exercises all subcommands)
make integration

# Manual end-to-end
make build && ./build/agentfile build
./build/go-pro validate
./build/go-pro --describe
```

## Status

Alpha. Core framework and declarative build system implemented. API may change before v1.0.

## License

See [LICENSE](LICENSE).
