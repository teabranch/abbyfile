# Distribution Guide

Agentfile agents compile to standalone binaries. The distribution layer handles the full lifecycle: publish to GitHub Releases, install from remote, update, list, and uninstall.

## Installing the agentfile CLI

Before you can build or install agents, you need the `agentfile` CLI itself:

```bash
# Go users (requires Go 1.24+)
go install github.com/teabranch/agentfile/cmd/agentfile@latest

# Pre-built binary (macOS / Linux)
curl -sSL https://raw.githubusercontent.com/teabranch/agentfile/main/install.sh | sh

# From source
git clone https://github.com/teabranch/agentfile.git
cd agentfile
make build && make install
```

The install script accepts `VERSION` (e.g. `VERSION=1.0.0`) to pin a release and `INSTALL_DIR` (default `/usr/local/bin`) to change the install location.

## Overview

```
agentfile publish              Cross-compile + create GitHub Release
agentfile install <ref>        Download from GitHub Releases + wire MCP
agentfile update [name]        Check for newer version, re-download
agentfile list                 Show installed agents
agentfile uninstall <name>     Remove binary, MCP entry, registry entry
```

All installed agents are tracked in a registry at `~/.agentfile/registry.json`.

## Publishing

### Prerequisites

- **`gh` CLI** installed and authenticated ([install guide](https://cli.github.com))
- Agent built and tested locally (`agentfile build && ./build/<name> validate`)

### Cross-Compile and Release

```bash
# Publish all agents in the Agentfile
agentfile publish

# Publish a single agent
agentfile publish --agent my-agent

# Cross-compile only (skip release creation)
agentfile publish --dry-run
```

`publish` cross-compiles for four targets:

| OS | Architecture |
|----|-------------|
| `darwin` | `amd64` |
| `darwin` | `arm64` |
| `linux` | `amd64` |
| `linux` | `arm64` |

Binaries are named `<agent>-<os>-<arch>` (e.g., `my-agent-darwin-arm64`).

### Release Tag Format

Each release is tagged as `<agent>/v<version>`. This supports multiple agents in a single repo:

```
my-agent/v1.0.0
my-agent/v1.1.0
other-agent/v0.1.0
```

The version comes from the `Agentfile`:

```yaml
agents:
  my-agent:
    path: agents/my-agent.md
    version: 1.0.0
```

### Dry Run

Use `--dry-run` to verify cross-compilation without creating a release:

```bash
agentfile publish --dry-run
# Building my-agent for darwin/amd64...
# Building my-agent for darwin/arm64...
# Building my-agent for linux/amd64...
# Building my-agent for linux/arm64...
# Dry run: built 4 binaries for my-agent v1.0.0 in build/publish
```

Check the `build/publish/` directory for the compiled binaries.

## Installing from GitHub

### Remote Install

```bash
# Install latest version
agentfile install github.com/owner/repo/agent-name

# Install specific version
agentfile install github.com/owner/repo/agent-name@1.2.0

# When repo name matches agent name, the /agent segment is optional
agentfile install github.com/owner/my-agent
agentfile install github.com/owner/my-agent@1.0.0

# Install globally
agentfile install -g github.com/owner/repo/agent-name
```

### What Happens During Remote Install

1. **Resolve release** -- fetches the latest (or specified) release from GitHub
2. **Find asset** -- matches `<agent>-<os>-<arch>` for your platform
3. **Download** -- downloads the binary to a temp file
4. **Verify** -- runs `<binary> --describe` to confirm it's a valid agent
5. **Install** -- moves to `.agentfile/bin/` (or `/usr/local/bin/` with `-g`)
6. **Wire MCP** -- updates `.mcp.json` (or `~/.claude/mcp.json` with `-g`)
7. **Track** -- records the install in `~/.agentfile/registry.json`

### Private Repositories

Authentication is resolved automatically in this order:

1. **`GITHUB_TOKEN` env var** — checked first
2. **`gh auth token`** — if the `gh` CLI is installed and authenticated, its token is used as a fallback

This means if you can run `agentfile publish` (which requires `gh` CLI auth), you can install from private repos automatically — no extra setup needed.

```bash
# Option 1: Explicit token
export GITHUB_TOKEN=ghp_your_token_here
agentfile install github.com/your-org/private-repo/agent

# Option 2: gh CLI (no env var needed)
gh auth login                    # one-time setup
agentfile install github.com/your-org/private-repo/agent
```

The token is also used for GitHub API rate limiting on public repos.

### Install-Time Config Overrides

Override settings at install time without editing config files:

```bash
agentfile install --model opus github.com/owner/repo/agent
```

This writes the override to `~/.agentfile/<name>/config.yaml`. The agent's `--describe` manifest and MCP instructions reflect the overridden value immediately. You can change it later with `<agent> config set model <value>` or revert with `<agent> config reset model`.

### Local Install (unchanged)

Local installs from `./build/` continue to work as before, and now also track in the registry:

```bash
agentfile build
agentfile install my-agent        # .agentfile/bin/ + .mcp.json + registry
agentfile install -g my-agent     # /usr/local/bin/ + ~/.claude/mcp.json + registry
```

## Updating

### Update All Remote Agents

```bash
agentfile update
# my-agent: 1.0.0 → 1.1.0
# other-agent: already up to date (v0.2.0)
```

### Update a Specific Agent

```bash
agentfile update my-agent
```

`update` only works for agents installed from a remote source. Locally-installed agents show a hint:

```
my-agent: installed from local build, skipping (use 'agentfile build && agentfile install my-agent' to update)
```

## Listing Installed Agents

```bash
agentfile list
```

Output:

```
NAME          VERSION  SOURCE                              SCOPE   PATH
my-agent      1.0.0    github.com/owner/repo/my-agent      local   /path/.agentfile/bin/my-agent
other-agent   0.2.0    local                               global  /usr/local/bin/other-agent
```

Shows all agents tracked in the registry regardless of source.

## Uninstalling

```bash
agentfile uninstall my-agent
# Removed /path/.agentfile/bin/my-agent
# Updated .mcp.json
# Uninstalled my-agent
```

Uninstall performs three actions:

1. **Removes the binary** from its installed path
2. **Unwires MCP** -- removes the entry from `.mcp.json` or `~/.claude/mcp.json`
3. **Removes from registry** -- cleans up `~/.agentfile/registry.json`

## Registry

All installs (local and remote) are tracked in `~/.agentfile/registry.json`:

```json
{
  "agents": {
    "my-agent": {
      "name": "my-agent",
      "source": "github.com/owner/repo/my-agent",
      "version": "1.0.0",
      "path": "/Users/you/.agentfile/bin/my-agent",
      "scope": "local",
      "installedAt": "2025-01-15T10:30:00Z"
    }
  }
}
```

The registry is used by `list`, `update`, and `uninstall` to find and manage installed agents. It is saved atomically (write temp + rename) to avoid corruption.

### Registry Fields

| Field | Description |
|-------|-------------|
| `name` | Agent name (also the map key) |
| `source` | `"local"` or `"github.com/owner/repo/agent"` |
| `version` | Semantic version at time of install |
| `path` | Absolute path to the installed binary |
| `scope` | `"local"` or `"global"` |
| `installedAt` | RFC3339 timestamp of install |

## Typical Workflow

### Publishing an Agent

```bash
# 1. Build and test locally
agentfile build
./build/my-agent validate
./build/my-agent --describe

# 2. Verify cross-compilation
agentfile publish --dry-run

# 3. Bump version in Agentfile if needed
# 4. Publish to GitHub
agentfile publish --agent my-agent
```

### Installing an Agent from a Team

```bash
# Install from your team's repo
agentfile install github.com/your-org/agents/code-reviewer

# Claude Code auto-discovers it via .mcp.json
# Later, check for updates
agentfile update code-reviewer
```

### Managing Your Agents

```bash
# See what's installed
agentfile list

# Update everything
agentfile update

# Remove an agent you no longer need
agentfile uninstall old-agent
```

## Plugin Distribution

When using `--plugin`, each agent also gets a self-contained plugin directory that can be shared:

```bash
agentfile build --plugin
# → build/my-agent.claude-plugin/

# Share the directory or archive it
tar czf my-agent-plugin.tar.gz -C build my-agent.claude-plugin

# Recipient loads it directly
claude --plugin-dir ./my-agent.claude-plugin/
```

The plugin directory contains the binary, MCP config, and skills — everything needed to use the agent in Claude Code without any install step. See the [Plugins Guide](./plugins.md).

## Troubleshooting

### "gh CLI not found on PATH"

Install the GitHub CLI: https://cli.github.com

### "no asset found in release"

The release does not have a binary for your platform. Check the release assets match the `<agent>-<os>-<arch>` naming convention. Run `agentfile publish` to create properly-named assets.

### "downloaded binary is not a valid agent"

The binary failed the `--describe` verification check. This means the asset is not a valid Agentfile agent binary. Ensure the release was created with `agentfile publish`.

### "GitHub API error (HTTP 403)"

Rate limited. Set `GITHUB_TOKEN`:

```bash
export GITHUB_TOKEN=ghp_your_token
agentfile install github.com/owner/repo/agent
```

### "agent is not installed (not found in registry)"

The agent was installed before the registry existed, or was installed manually. Re-install it:

```bash
agentfile install github.com/owner/repo/agent
```
