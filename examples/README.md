# Agentfile Examples

Example agent configurations demonstrating common patterns.

## Examples

### [`basic/`](basic/)

A minimal single-agent setup. Start here to understand the Agentfile format.

```
basic/
  Agentfile                # declares one agent
  agents/my-agent.md       # system prompt + tool config
```

Build and use:

```bash
cd basic
agentfile build            # -> ./build/my-agent + .mcp.json
./build/my-agent --version
./build/my-agent --describe
./build/my-agent validate
```

### [`multi-agent/`](multi-agent/)

A multi-agent repository with two focused agents: a Go developer and a code reviewer. Demonstrates the "one agent, one domain" pattern.

```
multi-agent/
  Agentfile                # declares two agents
  agents/golang-pro.md     # Go development specialist (6 tools)
  agents/code-reviewer.md  # code review specialist (3 tools)
```

Build and use:

```bash
cd multi-agent
agentfile build            # -> ./build/golang-pro, ./build/code-reviewer + .mcp.json
```

Both agents are auto-discovered by Claude Code via the generated `.mcp.json`.

## Creating Your Own

1. Create an `Agentfile` at your project root
2. Add agent `.md` files with dual frontmatter (see [format guide](../docs/guides/agentfile-format.md))
3. Run `agentfile build`

See the [Quickstart](../docs/quickstart.md) for a step-by-step walkthrough.
