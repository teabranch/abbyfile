# Agentfile Format Guide

This guide explains the two files that define agents: the `Agentfile` manifest and agent `.md` files.

## Agentfile (YAML Manifest)

The manifest lives at your project root and declares which agents to build. The CLI accepts both `Agentfile` and `agentfile.yaml` as filenames — when no `-f` flag is given, it checks for `Agentfile` first, then `agentfile.yaml`.

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
| `agents.<name>.path` | yes | Path to the agent's `.md` file (relative to Agentfile) |
| `agents.<name>.version` | yes | Semantic version for the built binary |

The agent **name** (the YAML key, e.g. `go-pro`) becomes the binary name. The **version** here overrides anything in the `.md` file.

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
---

You are a senior Go developer with deep expertise...
```

### Block 1 — Agent Identity

The first frontmatter block identifies the agent to Claude Code:

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Agent name (used as binary name if not overridden by Agentfile) |
| `description` | no | Short description shown in Claude Code's agent picker |
| `memory` | no | Set to any value (e.g. `project`) to enable persistent memory |
| `model` | no | Model hint for the runtime (surfaced in `--describe` and MCP instructions). Can be overridden at install time (`--model`) or via `config set model` |

### Block 2 — Tools and Detailed Description

The second frontmatter block declares tools and a fuller description:

| Field | Required | Description |
|-------|----------|-------------|
| `description` | no | Detailed description (overrides block 1 if present) |
| `tools` | no | Comma-separated list of builtin tool names |
| `custom_tools` | no | List of custom CLI tool definitions (see [Tools guide](tools.md)) |
| `skills` | no | List of skill definitions for plugin output (see [Plugins guide](plugins.md)) |
| `model` | no | Hint for Claude Code |

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

## Available Tools

Agents declare tools by their Claude Code name. The builder maps these to MCP tool implementations:

| Declare in `.md` | MCP tool name | Description |
|-------------------|---------------|-------------|
| `Read` | `read_file` | Read file contents at an absolute path |
| `Write` | `write_file` | Write content to a file, creating parent dirs |
| `Edit` | `edit_file` | Find-and-replace a unique string in a file |
| `Bash` | `run_command` | Execute a shell command with timeout |
| `Glob` | `glob_files` | Find files matching a glob pattern (supports `**`) |
| `Grep` | `grep_search` | Search file contents with regex |

Example:

```
tools: Read, Write, Bash
```

If `memory` is enabled, memory tools (`memory_read`, `memory_write`, `memory_list`, `memory_delete`) are added automatically.

## Minimal Example

The smallest valid setup:

**Agentfile:**
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
make build && ./build/agentfile build
./build/helper --version        # helper v0.1.0
./build/helper validate         # check wiring
```

## File Organization

A typical project layout:

```
Agentfile                        # manifest (or agentfile.yaml)
.claude/agents/
  go-pro.md                      # agent definition + prompt
  tool-eng.md                    # another agent
  skills/                        # skill markdown files (for --plugin)
    review-pr.md
    write-tests.md
build/
  agentfile                      # CLI tool (from make build)
  go-pro                         # compiled agent (from agentfile build)
  tool-eng                       # compiled agent
  go-pro.claude-plugin/          # plugin directory (from agentfile build --plugin)
.mcp.json                        # auto-generated by agentfile build
```

The `.claude/agents/` path is a convention — you can put `.md` files anywhere and point to them from the Agentfile. Skill paths are relative to the agent `.md` file.
