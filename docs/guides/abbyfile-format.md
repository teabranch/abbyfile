---
title: Abbyfile Format
parent: Guides
nav_order: 1
---

# Abbyfile Format Guide

This guide explains the two files that define agents: the `Abbyfile` manifest and agent `.md` files.

## Abbyfile (YAML Manifest)

The manifest lives at your project root and declares which agents to build. The CLI accepts both `Abbyfile` and `abbyfile.yaml` as filenames — when no `-f` flag is given, it checks for `Abbyfile` first, then `abbyfile.yaml`.

```yaml
version: "1"
agents:
  go-pro:
    path: .claude/agents/go-pro.md
    version: 0.1.0
  tool-eng:
    path: .claude/agents/tool-eng.md
    version: 0.2.0
```

### Fields

| Field | Required | Description |
|-------|----------|-------------|
| `version` | yes | Manifest format version (currently `"1"`) |
| `agents` | yes | Map of agent name → agent reference |
| `agents.<name>.path` | yes | Path to the agent's `.md` file (relative to Abbyfile) |
| `agents.<name>.version` | yes | Semantic version for the built binary |
| `agents.<name>.dependencies` | no | Names of other agents in this Abbyfile. Validated (each must exist, and an agent can't depend on itself); not otherwise used by the build |
| `publish.targets` | no | List of `{os, arch}` pairs that `abby publish` cross-compiles for, replacing the default four (`darwin`/`linux` × `amd64`/`arm64`) |

The agent **name** (the YAML key, e.g. `go-pro`) becomes the binary name. It must start with a letter or digit and contain only letters, digits, `-` and `_`. The **version** here overrides anything in the `.md` file.

## Agent .md Files (Dual Frontmatter)

Each agent `.md` file has two YAML frontmatter blocks followed by the system prompt body:

```markdown
---
name: go-pro
description: "when editing Go code files"
memory: project
---

---
description: "A Go development assistant for idiomatic, concurrent systems"
tools: Read, Write, Edit, Bash, Glob, Grep
sandbox:
  allow_commands: ["go test ./...", "go vet ./..."]
---

You are a senior Go developer with deep expertise...
```

### Block 1 — Agent Identity

The first frontmatter block identifies the agent to Claude Code:

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Agent name (used as binary name if not overridden by Abbyfile) |
| `description` | no | Short description shown in Claude Code's agent picker |
| `memory` | no | Set to any value (e.g. `project`) to enable persistent memory |
| `model` | no | Accepted, but not currently compiled into the binary. To give an agent a model hint (surfaced in `--describe` and MCP instructions), use `abby install --model` or `<agent> config set model` |

### Block 2 — Tools and Detailed Description

The second frontmatter block declares tools and a fuller description:

| Field | Required | Description |
|-------|----------|-------------|
| `description` | no | Detailed description (overrides block 1 if present) |
| `tools` | no | Comma-separated list of builtin tool names |
| `custom_tools` | no | List of custom CLI tool definitions (see [Tools guide](tools.md)) |
| `skills` | no | List of skill definitions for plugin output (see [Plugins guide](plugins.md)) |
| `context_budget` | no | Tool-output caps and instructions behaviour (see [Context Budget guide](context-budget.md)) |
| `sandbox` | no | Confinement for the built-in tools: `allowed_dirs`, `bash`, `allow_commands`, `max_command_timeout` (see [Tools guide → Sandbox](tools.md#sandbox)) |
| `model` | no | Accepted, but not currently compiled into the binary (same as block 1) |

#### Skills

Skills are markdown files that get packaged into a Claude Code plugin directory when building with `--plugin`. Each skill becomes a `skills/<name>/SKILL.md` in the plugin.

```yaml
skills:
  - name: review-pr
    description: "Review a pull request for quality"
    path: skills/review-pr.md
  - name: write-tests
    description: "Generate unit tests"
    path: skills/write-tests.md
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Skill name (used as directory name) |
| `description` | yes | Short description for Claude Code |
| `path` | yes | Path to skill markdown file (relative to agent .md file) |

### Prompt Body

Everything after the second `---` delimiter is the system prompt. This gets embedded into the compiled binary and is returned by `--custom-instructions` and exposed via MCP.

### Single-Block Format

Instead of two blocks, an agent `.md` file can use one frontmatter block with the abbyfile settings under an `abbyfile:` key. In this form `tools` is a YAML list:

```markdown
---
name: go-pro
description: "A Go development assistant"
abbyfile:
  tools: [Read, Write, Bash]
  memory: project
  sandbox:
    allow_commands: ["go test ./..."]
---

You are a senior Go developer...
```

The `abbyfile:` block accepts `tools`, `memory`, `custom_tools`, `skills`, `context_budget` and `sandbox`, with the same meaning as above. The file is parsed as dual-block first; the single-block form is used only when that fails, and it requires the `abbyfile:` key.

## Available Tools

Agents declare tools by their Claude Code name. The builder maps these to MCP tool implementations:

| Declare in `.md` | MCP tool name | Description |
|-------------------|---------------|-------------|
| `Read` | `read_file` | Read a file's contents, confined to the [sandbox](tools.md#sandbox) |
| `Write` | `write_file` | Write content to a file, creating parent dirs, confined to the [sandbox](tools.md#sandbox) |
| `Edit` | `edit_file` | Find-and-replace a unique string in a file, confined to the [sandbox](tools.md#sandbox) |
| `Bash` | `run_command` | Run an allowlisted command with no shell, per the [sandbox](tools.md#sandbox) |
| `Glob` | `glob_files` | Find files matching a glob pattern (supports `**`), confined to the [sandbox](tools.md#sandbox) |
| `Grep` | `grep_search` | Search file contents with regex, confined to the [sandbox](tools.md#sandbox) |

Example:

```
tools: Read, Write, Bash
```

`run_command` (from `Bash`) refuses every call until the agent lists commands in `sandbox.allow_commands` or sets `sandbox.bash: unrestricted`; `abby build` prints a note for a Bash agent with neither.

If `memory` is enabled, memory tools (`memory_read`, `memory_write`, `memory_list`, `memory_delete`, `memory_search`) are added automatically.

## Minimal Example

The smallest valid setup:

**Abbyfile:**
```yaml
version: "1"
agents:
  helper:
    path: agents/helper.md
    version: 0.1.0
```

**agents/helper.md:**
```markdown
---
name: helper
---

---
tools: Read
---

You are a helpful assistant.
```

Build and run:
```bash
make build && ./build/abby build
./build/helper --version        # helper v0.1.0
./build/helper validate         # check wiring
```

## File Organization

A typical project layout:

```
Abbyfile                        # manifest (or abbyfile.yaml)
.claude/agents/
  go-pro.md                      # agent definition + prompt
  tool-eng.md                    # another agent
  skills/                        # skill markdown files (for --plugin)
    review-pr.md
    write-tests.md
build/
  abby                           # CLI tool (from make build)
  go-pro                         # compiled agent (from abby build)
  tool-eng                       # compiled agent
  go-pro.claude-plugin/          # plugin directory (from abby build --plugin)
  .claude/agents/go-pro.md       # sub-agent file (from abby build --subagent or --plugin)
.mcp.json                        # project-scope MCP config written by abby build (Claude Code)
.codex/config.toml               # ... for Codex, when detected
.gemini/settings.json            # ... for Gemini CLI, when detected
```

The first time abby edits an existing config file it leaves a one-time `*.abbyfile.bak` backup next to it; consider adding `*.abbyfile.bak` to `.gitignore`. See the [Distribution guide](distribution.md) for how runtimes are detected and how entries are written.

The `.claude/agents/` path is a convention — you can put `.md` files anywhere and point to them from the Abbyfile. Skill paths are relative to the agent `.md` file.
