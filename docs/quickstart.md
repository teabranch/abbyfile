# Quickstart: Build an Agent in 5 Minutes

## The Problem

CLAUDE.md files work for single-repo instructions, but they break down when you need:

- **Versioning** -- no semver, no way to pin behavior to a release
- **Testing** -- no way to unit-test a markdown file's tools or memory
- **Memory** -- no persistent state across conversations
- **Sharing** -- no way to distribute agent logic as a single artifact
- **Composition** -- no way to wire multiple agents together through MCP

Abbyfile packages agent logic as a compiled Go binary. Claude Code remains the LLM -- the binary is just a packaging format.

## Prerequisites

- **Claude Code** (the LLM runtime that loads and runs agents)
- **Go 1.24+** (only needed for `go install` or building from source)

## Step 1: Install the CLI

Pick one:

```bash
# Option A: Go users
go install github.com/teabranch/abbyfile/cmd/abby@latest

# Option B: Pre-built binary
curl -sSL https://raw.githubusercontent.com/teabranch/abbyfile/main/install.sh | sh

# Option C: From source
git clone https://github.com/teabranch/abbyfile.git
cd abbyfile
make build    # → build/abby
```

## Step 2: Create an Abbyfile

Create an `Abbyfile` (or `abbyfile.yaml`) at your project root:

```yaml
version: "1"
agents:
  my-agent:
    path: agents/my-agent.md
    version: 0.1.0
```

This declares one agent named `my-agent`, pointing to its `.md` file.

## Step 3: Write the Agent Definition

Create `agents/my-agent.md` with **dual frontmatter** (two `---` blocks) followed by the system prompt:

```markdown
---
name: my-agent
memory: project
---

---
description: "A helpful assistant"
tools: Read, Bash
---

You are a helpful assistant. You answer questions clearly and concisely.
When you have tools available, use them to gather information before responding.
```

**Block 1** sets identity: `name` and `memory` (any value enables it).
**Block 2** sets capabilities: `tools` (comma-separated) and `description`.
**Body** is the system prompt baked into the binary.

See the [Abbyfile Format Guide](./guides/abbyfile-format.md) for full details.

## Step 4: Build

```bash
./build/abby build
# Building my-agent...
#   → ./build/my-agent
# Updated .mcp.json (claude-code)

# Target a specific runtime or all runtimes
./build/abby build --runtime codex    # → .codex/config.toml
./build/abby build --runtime all      # → all detected runtimes

# Optional: also generate a Claude Code plugin directory (with skills support)
./build/abby build --plugin
# → ./build/my-agent.claude-plugin/
```

## Step 5: Explore

```bash
# Version (semver, machine-parseable)
./build/my-agent --version
# my-agent v0.1.0

# JSON manifest (tools, memory, version)
./build/my-agent --describe

# System prompt
./build/my-agent --custom-instructions

# Run a tool directly
./build/my-agent run-tool read_file --input '{"path":"/etc/hostname"}'

# Memory operations
./build/my-agent memory write notes "Remember this for later"
./build/my-agent memory read notes
./build/my-agent memory list
./build/my-agent memory delete notes

# Validate wiring (checks prompt, tools, memory, version)
./build/my-agent validate

# Runtime config overrides (no rebuild needed)
./build/my-agent config get                    # show compiled defaults
./build/my-agent config set model opus         # override the model hint
./build/my-agent config reset model            # revert to default
```

## Step 6: Connect to Your Runtime

`abby build` auto-generates MCP config for detected runtimes. For Claude Code, this is `.mcp.json`:

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

Or install the binary explicitly:

```bash
./build/abby install my-agent                  # local install, auto-detect runtimes
./build/abby install -g my-agent               # global install
./build/abby install --runtime codex my-agent  # target Codex specifically
```

Supported runtimes:

| Runtime | Local Config | Global Config |
|---------|-------------|---------------|
| Claude Code | `.mcp.json` | `~/.claude/mcp.json` |
| Codex | `.codex/config.toml` | `~/.codex/config.toml` |
| Gemini CLI | `.gemini/settings.json` | `~/.gemini/settings.json` |

Your runtime auto-discovers the agent via MCP. It sees the agent's tools, can read its system prompt, and can interact with its memory.

## What You Get vs. a CLAUDE.md

| Capability | CLAUDE.md | Abbyfile Binary |
|---|---|---|
| System prompt | Markdown file in repo | Baked into binary, override for dev |
| Versioning | Git history only | Semantic versioning (`--version`) |
| Tools | Described in prose | Registered, validated, executable |
| Memory | None | Persistent KV store per agent |
| Testing | Manual | `go test`, integration tests, `validate` |
| Sharing | Copy the file | Binary distribution |
| Discovery | Claude reads the file | MCP auto-discovery via `serve-mcp` |
| Machine-readable | No | `--describe` returns JSON manifest |

## Next Steps

- [Examples](../examples/) -- working single-agent and multi-agent configurations
- [Abbyfile Format](./guides/abbyfile-format.md) -- Abbyfile YAML + agent .md reference
- [Concepts](./concepts.md) -- understand the architecture and mental model
- [Tools Guide](./guides/tools.md) -- builtin tools, annotations
- [Memory Guide](./guides/memory.md) -- persistent state management
- [Prompts Guide](./guides/prompts.md) -- prompt overrides for development
- [Plugins Guide](./guides/plugins.md) -- Claude Code plugin output with skills
- [Distribution Guide](./guides/distribution.md) -- publish, install from GitHub, update, uninstall
- [MCP Integration](./guides/mcp.md) -- connecting to Claude Code
- [Testing Guide](./guides/testing.md) -- unit, integration, and MCP testing
- [Reference](./reference.md) -- all options, flags, and types
