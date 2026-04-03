---
title: Home
nav_order: 0
permalink: /
---

# Abbyfile

**Agents Binaries Built-by YAML** — a packaging format for AI agents. Build once, run on any MCP-compatible runtime.

<p><em>Find this useful? Star the repo to follow updates and show support!</em>
<iframe src="https://ghbtns.com/github-btn.html?user=teabranch&repo=abbyfile&type=star&count=true&size=large" frameborder="0" scrolling="0" width="170" height="30" title="Star on GitHub" style="vertical-align: middle;"></iframe></p>

> **Install** — `curl -sSL https://raw.githubusercontent.com/teabranch/abbyfile/main/install.sh | sh`
> or `go install github.com/teabranch/abbyfile/cmd/abby@latest`.
> See [Quickstart](docs/quickstart.md) to build your first agent in 5 minutes.

Abbyfile turns agent definitions — a system prompt, tools, and memory — into versioned, distributable CLI binaries. Authors publish to GitHub Releases. Consumers install with one command. Any MCP-compatible runtime auto-discovers the rest.

No Go code. No manual config. Just `abby build`.

---

## What's Inside

- **[Quickstart](docs/quickstart.md)** — Build an agent in 5 minutes
- **[Concepts](docs/concepts.md)** — Architecture and mental model
- **[Abbyfile Format](docs/guides/abbyfile-format.md)** — YAML manifest + agent .md reference
- **[Tools](docs/guides/tools.md)** — Built-in tools, custom CLI tools, annotations
- **[Memory](docs/guides/memory.md)** — Persistent key-value storage
- **[Prompts](docs/guides/prompts.md)** — Embedding and overriding prompts
- **[Distribution](docs/guides/distribution.md)** — Publish, install, update, uninstall
- **[Plugins](docs/guides/plugins.md)** — Claude Code plugin output with skills
- **[MCP Integration](docs/guides/mcp.md)** — Multi-runtime integration via MCP
- **[Testing](docs/guides/testing.md)** — Unit, integration, and MCP testing
- **[Benchmarks](docs/guides/benchmarks.md)** — Token cost methodology and results
- **[Reference](docs/reference.md)** — All options, subcommands, flags, types
- **[FAQ](docs/faq.md)** — Common questions
- **[Development](docs/development.md)** — Contributing to Abbyfile

---

## Why We Built This

CLAUDE.md files are great for single-repo instructions, but they don't version, don't distribute, and can't carry tools or memory. Agent Skills add progressive disclosure but still lack executable tools and persistent state. Sub-agents provide context isolation but re-pay the runtime's baseline on every invocation. MCP servers solve the tool problem but weren't designed with context efficiency in mind.

We wanted agents that follow the Unix philosophy: small, focused, composable. Each agent does one thing well with 6-15 purpose-built tools, keeping context overhead minimal while remaining easy to build, test, and share as real software — and run on any MCP-compatible runtime.

---

## Quick Start

### 1. Define your agent

**`Abbyfile`**

```yaml
version: "1"
agents:
  my-agent:
    path: agents/my-agent.md
    version: 0.1.0
```

**`agents/my-agent.md`** — dual frontmatter + system prompt:

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

### 2. Build and use

```bash
abby build
# -> ./build/my-agent binary + MCP config for detected runtimes
```

### 3. Share it

```bash
# Publish to GitHub Releases (cross-compiled for macOS + Linux)
abby publish

# Anyone can install it with one command
abby install github.com/you/my-agent
```

---

## What You Get

| | CLAUDE.md | Agent Skills | Sub-agents | Abbyfile |
|---|---|---|---|---|
| **Multi-runtime** | No | No | No | Claude Code, Codex, Gemini CLI |
| **Context cost** | File size | ~3KT loaded | ~10.6KT/call | ~1.5KT marginal |
| **Custom tools** | Described in prose | No | Inherited | Registered, validated, MCP |
| **Persistent memory** | None | None | None | Key-value store per agent |
| **Versioning** | Git history | None | None | Semantic versioning |
| **Distribution** | Copy the file | Folder copy | N/A | `abby install` from anywhere |

---

## How It Works

The runtime is the brain. The binary is the body — it provides the instructions, the hands (tools), and the memory. The runtime loads the agent's prompt, discovers its tools via MCP, and handles all reasoning.

```
LLM Runtime (Claude Code / Codex / Gemini CLI)
  |
  |  MCP-over-stdio
  v
Agent Binary
  +-- --custom-instructions  -> system prompt
  +-- serve-mcp              -> MCP server (tools + memory)
  +-- config get|set|reset   -> runtime config overrides
  +-- --version              -> semver
  +-- --describe             -> JSON manifest
  +-- validate               -> check wiring
```

**Supported runtimes:** Claude Code (`.mcp.json`), Codex (`.codex/config.toml`), Gemini CLI (`.gemini/settings.json`).

---

## Status

Alpha. Core framework and declarative build system implemented. API may change before v1.0.

## License

See [LICENSE](https://github.com/teabranch/abbyfile/blob/main/LICENSE).
