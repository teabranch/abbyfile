# Concepts

## Architecture

```
Claude Code (LLM Runtime / Orchestrator)
  |
  |  MCP-over-stdio (.mcp.json)
  |
  v
Agent Binary (Agentfile)
  |
  +-- --version              -> "my-agent v1.0.0"
  +-- --describe             -> JSON manifest (tools, memory, version)
  +-- --custom-instructions  -> system prompt text
  +-- run-tool <name>        -> execute a tool (CLI or builtin)
  +-- memory read|write|list|delete|append  -> persistent state
  +-- serve-mcp              -> MCP-over-stdio server
  +-- validate               -> check agent wiring

Optional: Claude Code Plugin (--plugin)
  |
  +-- .claude-plugin/plugin.json  -> plugin metadata
  +-- .mcp.json                   -> MCP config (wraps the binary)
  +-- <binary>                    -> copy of agent binary
  +-- skills/<name>/SKILL.md      -> Claude Code skills
```

**The binary does NOT call the Claude API.** Claude Code is the LLM. It loads the agent's prompt, sees the agent's tools via MCP, handles reasoning, and decides when to invoke tools. The binary is a packaging format -- a self-contained artifact that exposes everything through CLI subcommands and an MCP server.

Think of it this way: Claude Code is the brain, and the agent binary is the body -- it provides the instructions, the hands (tools), and the memory.

## Declarative Agent Definition

Every agent is defined by two files:

**`Agentfile`** (or **`agentfile.yaml`**) — the YAML manifest at your project root:

```yaml
version: "1"
agents:
  my-agent:
    path: agents/my-agent.md
    version: 1.0.0
```

**Agent `.md` file** — dual frontmatter plus system prompt:

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

**Block 1** sets identity: `name` and `memory` (any value enables it).
**Block 2** sets capabilities: `tools` (comma-separated), `description`, and optionally `skills` for plugin output.
**Body** is the system prompt baked into the binary.

Run `agentfile build` and the framework generates Go source and compiles a standalone binary — no Go code required. Add `--plugin` to also generate a Claude Code plugin directory with skills. See the [Agentfile Format Guide](./guides/agentfile-format.md) for full details.

## Agent Lifecycle

```
agentfile build [--plugin]    Parse Agentfile + agent .md files
       |
       v
Generated binary              Contains embedded prompt, tool references, memory config
       |
       v (if --plugin)
Plugin directory              .claude-plugin/ with binary, MCP config, skills
       |
       v
agent.Execute()        Wire Cobra CLI, register tools, init memory
       |
       +-- prompt.NewLoader()        Load embedded prompt (or override)
       +-- tools.NewRegistry()       Register all tool definitions
       +-- memory.NewFileStore()     Init file-based KV store (if enabled)
       +-- memory.NewManager()       Wrap store with concurrency + tool handlers
       +-- cli.NewRootCommand()      Build root command (--version, --describe, --custom-instructions)
       +-- cli.NewRunToolCommand()   Add run-tool subcommand
       +-- cli.NewServeMCPCommand()  Add serve-mcp subcommand
       +-- cli.NewValidateCommand()  Add validate subcommand
       +-- cli.NewMemoryCommand()    Add memory subcommand group (if enabled)
       |
       v
cmd.Execute()          Run the Cobra command tree
```

`agentfile build` parses the Agentfile and each agent's `.md` file, generates Go source with the prompt embedded, and compiles a standalone binary. With `--plugin`, it also generates a Claude Code plugin directory wrapping the binary with its MCP config and any declared skills. At runtime, `Execute()` creates the prompt loader, tool registry, memory store, and the full Cobra CLI tree, then hands off to Cobra.

## Distribution Lifecycle

Agents are distributed as repos. An author maintains an Agentfile manifest and markdown definitions in a Git repository, publishes versioned releases to GitHub, and consumers install with a single command. No cloning, no building from source — just a binary that works.

```
agentfile build        Compile agent binaries from Agentfile + .md
       |
       v
agentfile publish      Cross-compile for darwin/linux × amd64/arm64
       |               Create GitHub Release: <agent>/v<version>
       v
GitHub Releases        Versioned binary assets per platform
       |
       v
agentfile install      Download binary → verify (--describe) → wire MCP
       |               Track in ~/.agentfile/registry.json
       v
agentfile update       Check for newer release → re-download → replace
       |
       v
agentfile uninstall    Remove binary + MCP entry + registry entry
```

The registry at `~/.agentfile/registry.json` tracks every installed agent with its source (local or remote), version, path, and scope. `agentfile list` shows all tracked agents.

### Repository Structure

A minimal agent repo contains just two files:

```
my-agent/
  Agentfile              # manifest: agent name, path, version
  agents/my-agent.md     # system prompt + frontmatter config
```

For agents with custom tools, skills, or additional resources, the layout grows naturally:

```
my-agent/
  Agentfile
  agents/my-agent.md
  agents/skills/          # skill markdown files (for --plugin)
    review-pr.md
    write-tests.md
  tools/lint.sh           # custom tool scripts
  README.md
```

### Publisher vs. Consumer

**As a publisher**, you maintain the repo, write the prompt and tool definitions, and run `agentfile publish` to create a GitHub Release with cross-compiled binaries. Versioning follows semver — bump the version in the Agentfile, publish, and consumers get a deterministic upgrade path.

**As a consumer**, you run `agentfile install github.com/org/repo/agent` and the framework handles everything: downloading the right binary for your OS/arch, verifying it, wiring the MCP entry, and tracking it for future updates. You never touch Go, YAML, or config files.

### Repository Patterns

**Single-agent repo** — one agent per repository. Simple to version and publish:

```yaml
# Agentfile
version: "1"
agents:
  code-reviewer:
    path: agents/code-reviewer.md
    version: 1.0.0
```

**Multi-agent monorepo** — several related agents in one repository. Each agent gets its own release tag (`<agent>/v<version>`), so consumers can install them independently:

```yaml
# Agentfile
version: "1"
agents:
  go-pro:
    path: .claude/agents/go-pro.md
    version: 0.3.0
  tool-eng:
    path: .claude/agents/tool-eng.md
    version: 0.2.0
```

Consumers install individual agents: `agentfile install github.com/org/repo/go-pro`. The monorepo pattern works well for teams that maintain a suite of related agents with shared conventions.

## Versioning Model

Agents use semantic versioning. The version is set in the `Agentfile`:

```yaml
agents:
  my-agent:
    path: agents/my-agent.md
    version: 1.2.0
```

It surfaces in three places:

- `--version` flag: prints `my-agent v1.2.0`
- `--describe` JSON manifest: `"version": "1.2.0"`
- MCP server implementation: reported during the MCP handshake

This means you can pin agent behavior to a specific version, roll back, and track changes over time -- just like any other software. When publishing, the version determines the release tag (`<agent>/v<version>`) and users can install a specific version with `agentfile install github.com/owner/repo/agent@1.2.0`.

## System Prompts are Append-Only

Within a version, the embedded system prompt is immutable. It is baked into the binary at compile time. To change the prompt in production, you:

1. Edit the agent's `.md` file (the prompt body after the frontmatter)
2. Bump the version in the `Agentfile`
3. Run `agentfile build` and redistribute

For development, use the override mechanism: place an `override.md` file at `~/.agentfile/<name>/override.md` and it replaces the embedded prompt without rebuilding. See the [Prompts Guide](./guides/prompts.md).

## When to Use What

**Use CLAUDE.md when:**
- You have repo-specific instructions for a single project
- No tools, memory, or versioning needed
- Instructions change frequently during active development

**Use Agent Skills when:**
- You need context-only instructions (no executable tools)
- Progressive disclosure is important (many skills, few active at a time)
- You want maximum portability (just markdown folders)

**Use Sub-agents when:**
- You need context isolation (verbose output shouldn't pollute main context)
- The task is exploratory or one-shot (no persistent state needed)
- You're delegating independent work units

**Use Agentfile when:**
- You want to version and distribute agent logic
- You need persistent memory across conversations
- You want testable, validated tool integrations
- You are building reusable agents shared across projects or teams
- You need MCP-based composition with other agents or tools
- You want one-command install from GitHub: `agentfile install github.com/org/repo/agent`

**Composition patterns:** These are not mutually exclusive. A project can have a CLAUDE.md for repo-level instructions, Agent Skills for lightweight context injection, and Agentfile binaries for specialized agent capabilities registered via `.mcp.json`. Sub-agents can also invoke Agentfile agents' MCP tools.

## Plugins: Beyond the Binary

The binary is always the core artifact, but some Claude Code features — like skills — exist as directory structures, not compiled code. The `--plugin` flag bridges this gap: it builds the binary (as normal) and wraps it in a Claude Code plugin directory.

```
agentfile build --plugin
→ build/my-agent                        # standalone binary (always)
→ build/my-agent.claude-plugin/         # plugin directory (with --plugin)
    .claude-plugin/plugin.json
    .mcp.json
    my-agent                            # binary copy
    skills/review-pr/SKILL.md           # skills from frontmatter
```

Skills are declared in the agent `.md` frontmatter and reference plain markdown files. The plugin generator wraps them in the SKILL.md format Claude Code expects.

Test locally with `claude --plugin-dir ./build/my-agent.claude-plugin/`. See the [Plugins Guide](./guides/plugins.md) for details.
