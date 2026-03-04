# Prompts Guide

The system prompt defines how the agent behaves -- its role, capabilities, and guidelines. In Agentfile, prompts are written as the body of an agent `.md` file, embedded into the binary at compile time by `agentfile build`, and can be overridden locally for development.

## Writing Prompts in Agent `.md` Files

The system prompt is the body of the agent's `.md` file -- everything after the second frontmatter block:

```markdown
---
name: dev-notes
memory: project
---

---
description: "A dev-notes assistant"
tools: Read
---

You are a dev-notes assistant -- a lightweight helper for developers who want
to keep track of notes, ideas, and context across sessions.

You have two capabilities:
- **read_file**: Read the contents of any file by path.
- **current_time**: Get the current date and time.

You also have persistent memory. Use it to:
- Store notes the developer asks you to remember
- Recall context from previous sessions
- Keep a running log of important facts about the project
```

When you run `agentfile build`, the prompt body is extracted and embedded into the compiled binary. No Go code required.

## How Prompts Surface

The system prompt reaches Claude Code through multiple channels:

1. **MCP server instructions** -- when `serve-mcp` starts, the prompt is set as the MCP server's `instructions` field during the handshake
2. **`--custom-instructions` flag** -- prints the prompt to stdout for direct inspection
3. **`get_instructions` MCP tool** -- a backward-compatible tool that returns the prompt text
4. **`system` MCP prompt template** -- available via the MCP prompts API

All four channels return the same text (or its override).

## Override for Development

During development, you often want to iterate on the prompt without rebuilding the binary. Place an override file at:

```
~/.agentfile/<agent-name>/override.md
```

For example, for an agent named `my-agent`:

```bash
mkdir -p ~/.agentfile/my-agent
cat > ~/.agentfile/my-agent/override.md << 'EOF'
You are my-agent in development mode.
Be extra verbose. Explain your reasoning step by step.
EOF
```

When the override file exists, it completely replaces the embedded prompt. The binary itself is unchanged.

Check override status:

```bash
./my-agent validate
# [INFO] Override: active at /Users/you/.agentfile/my-agent/override.md
```

Remove the override to go back to the embedded prompt:

```bash
rm ~/.agentfile/my-agent/override.md
./my-agent validate
# [INFO] Override: not active (using embedded prompt)
```

## How the Loader Works

The `prompt.Loader` follows this logic:

```
1. Check if ~/.agentfile/<name>/override.md exists
2. If yes: read and return override content
3. If no: read and return the compiled-in prompt
```

Both paths trim whitespace from the result.

## Append-Only Philosophy

System prompts in Agentfile are treated as append-only within a version:

- The embedded prompt is immutable once compiled
- New behavior is added by appending to the agent `.md` file body, bumping the version, and running `agentfile build`
- Destructive changes (removing instructions, changing behavior) go through a version bump

This ensures that a given version of an agent binary always produces the same behavior. The override mechanism exists solely for development iteration.

## Prompt Writing Tips

**Be explicit about tools.** List the tools by name and describe when to use each one. Claude Code can see the tool schemas via MCP, but the system prompt provides the reasoning context.

Example:

```markdown
## Available Tools
- **read_file** -- Read the contents of a file
- **write_file** -- Write content to a file
- **run_command** -- Execute a shell command
- **glob_files** -- Find files by pattern
- **grep_search** -- Search file contents with regex
```

**Describe memory usage.** If memory is enabled, tell the agent what to store and how to organize keys.

```markdown
## Memory
You have persistent memory. Use it to track:
- Test results and trends
- Known issues and their status
- Project health metrics over time
```

**Keep it focused.** Unlike CLAUDE.md (which covers repo-level instructions), an agent prompt describes one role and its capabilities. A focused prompt produces better tool selection and more predictable behavior.

## Testing Prompts

Use `t.Setenv("HOME", ...)` to test override behavior without touching the real home directory. See `pkg/prompt/prompt_test.go` for the full test patterns.
