# Abbyfile Examples

Example agent configurations demonstrating common patterns.

## Examples

### [`basic/`](basic/)

A minimal single-agent setup. Start here to understand the Abbyfile format.

```
basic/
  Abbyfile                # declares one agent
  agents/my-agent.md       # system prompt + tool config
```

Build and use:

```bash
cd basic
abby build            # -> ./build/my-agent + MCP config
./build/my-agent --version
./build/my-agent --describe
./build/my-agent validate
```

### [`multi-agent/`](multi-agent/)

A multi-agent repository with two focused agents: a Go developer and a code reviewer. Demonstrates the "one agent, one domain" pattern.

```
multi-agent/
  Abbyfile                # declares two agents
  agents/golang-pro.md     # Go development specialist (6 tools)
  agents/code-reviewer.md  # code review specialist (3 tools)
```

Build and use:

```bash
cd multi-agent
abby build            # -> ./build/golang-pro, ./build/code-reviewer + MCP config
```

Both agents are auto-discovered by your runtime via the generated MCP config.

### [`model-override/`](model-override/)

Demonstrates the runtime config override feature. The agent declares a model hint (`model: claude-opus-4-6`) and consumers can override it at install time or later via the `config` subcommand.

```
model-override/
  Abbyfile                     # declares one agent with a model hint
  agents/smart-reviewer.md      # code reviewer recommending opus
```

Build and use:

```bash
cd model-override
abby build                 # -> ./build/smart-reviewer + MCP config
./build/smart-reviewer --describe             # shows model in manifest
./build/smart-reviewer config get             # shows compiled defaults
./build/smart-reviewer config set model sonnet  # override at runtime
./build/smart-reviewer config get model       # sonnet (override)
./build/smart-reviewer config reset model     # revert to compiled default
```

## Creating Your Own

1. Create an `Abbyfile` at your project root
2. Add agent `.md` files with dual frontmatter (see [format guide](../docs/guides/abbyfile-format.md))
3. Run `abby build`

See the [Quickstart](../docs/quickstart.md) for a step-by-step walkthrough.
