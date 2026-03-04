# Quickstart: Build an Agent in 5 Minutes

## The Problem

CLAUDE.md files work for single-repo instructions, but they break down when you need:

- **Versioning** -- no semver, no way to pin behavior to a release
- **Testing** -- no way to unit-test a markdown file's tools or memory
- **Memory** -- no persistent state across conversations
- **Sharing** -- no way to distribute agent logic as a single artifact
- **Composition** -- no way to wire multiple agents together through MCP

Agentfile packages agent logic as a compiled Go binary. Claude Code remains the LLM -- the binary is just a packaging format.

## Prerequisites

- **Claude Code** (the LLM runtime that loads and runs agents)
- **Go 1.24+** (only needed for `go install` or building from source)

## Step 1: Install the CLI

Pick one:

```bash
# Option A: Go users
go install github.com/teabranch/agentfile/cmd/agentfile@latest

# Option B: Pre-built binary
curl -sSL https://raw.githubusercontent.com/teabranch/agentfile/main/install.sh | sh

# Option C: From source
git clone https://github.com/teabranch/agentfile.git
cd agentfile
make build    # → build/agentfile
```

## Step 2: Create an Agentfile

Create an `Agentfile` (or `agentfile.yaml`) at your project root:

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

See the [Agentfile Format Guide](./guides/agentfile-format.md) for full details.

## Step 4: Build

```bash
./build/agentfile build
# Building my-agent...
#   → ./build/my-agent
# Updated .mcp.json
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
```

## Step 6: Connect to Claude Code

`agentfile build` auto-generates `.mcp.json`. Claude Code picks it up:

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
./build/agentfile install my-agent       # local: .agentfile/bin/ + .mcp.json
./build/agentfile install -g my-agent    # global: /usr/local/bin/ + ~/.claude/mcp.json
```

Claude Code auto-discovers the agent via MCP. It sees the agent's tools, can read its system prompt, and can interact with its memory.

## What You Get vs. a CLAUDE.md

| Capability | CLAUDE.md | Agentfile Binary |
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

- [Agentfile Format](./guides/agentfile-format.md) -- Agentfile YAML + agent .md reference
- [Concepts](./concepts.md) -- understand the architecture and mental model
- [Tools Guide](./guides/tools.md) -- builtin tools, annotations
- [Memory Guide](./guides/memory.md) -- persistent state management
- [Prompts Guide](./guides/prompts.md) -- prompt overrides for development
- [Distribution Guide](./guides/distribution.md) -- publish, install from GitHub, update, uninstall
- [MCP Integration](./guides/mcp.md) -- connecting to Claude Code
- [Testing Guide](./guides/testing.md) -- unit, integration, and MCP testing
- [Reference](./reference.md) -- all options, flags, and types
