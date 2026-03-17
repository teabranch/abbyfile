# Plugins Guide

Abbyfile can optionally generate a [Claude Code plugin](https://docs.anthropic.com/en/docs/claude-code/plugins) directory alongside the compiled binary. The plugin wraps the binary as its MCP server and adds features like skills that the binary alone can't carry.

## Overview

The binary is always the core artifact. The plugin is an optional output format that adds richer Claude Code integration.

```
abby build              → build/my-agent + .mcp.json          (default)
abby build --plugin     → build/my-agent + .mcp.json          (same as above)
                               build/my-agent.claude-plugin/       (new)
```

## Plugin Directory Layout

```
build/my-agent.claude-plugin/
  .claude-plugin/plugin.json     # plugin metadata
  .mcp.json                      # MCP config pointing to local binary
  my-agent                       # copy of compiled binary
  skills/
    review-pr/SKILL.md           # skill files (if declared)
    write-tests/SKILL.md
```

## Declaring Skills

Skills are markdown files referenced in the agent's `.md` frontmatter (block 2):

```yaml
---
description: "A Go development assistant"
tools: Read, Write, Bash
skills:
  - name: review-pr
    description: "Review a pull request for quality"
    path: skills/review-pr.md
  - name: write-tests
    description: "Generate unit tests"
    path: skills/write-tests.md
---
```

Each skill requires:

| Field | Description |
|-------|-------------|
| `name` | Skill name — becomes the `skills/<name>/` directory |
| `description` | Short description for Claude Code |
| `path` | Path to the skill's markdown file, relative to the agent `.md` file |

The skill `.md` file is plain markdown — no frontmatter needed. Abbyfile adds the SKILL.md frontmatter (name + description) during plugin generation.

## Building a Plugin

```bash
abby build --plugin
```

This builds the binary (as normal) **and** generates the plugin directory in the output folder.

## Testing Locally

Load the plugin directory directly in Claude Code:

```bash
claude --plugin-dir ./build/my-agent.claude-plugin/
```

Claude Code will discover the MCP server and skills from the plugin directory.

## Example

**Agent definition** (`agents/go-pro.md`):

```markdown
---
name: go-pro
memory: project
---

---
description: "A Go development assistant"
tools: Read, Write, Bash
skills:
  - name: review-pr
    description: "Review a Go pull request"
    path: skills/review-pr.md
---

You are a senior Go developer...
```

**Skill file** (`agents/skills/review-pr.md`):

```markdown
Review the pull request for:
- Idiomatic Go patterns
- Error handling
- Test coverage
- Race conditions
```

**Build and test:**

```bash
abby build --plugin
claude --plugin-dir ./build/go-pro.claude-plugin/
```
