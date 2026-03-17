# Distribution Guide

Abbyfile agents compile to standalone binaries. The distribution layer handles the full lifecycle: publish to GitHub Releases, install from remote, update, list, and uninstall.

## Installing the abby CLI

Before you can build or install agents, you need the `abby` CLI itself:

```bash
# Go users (requires Go 1.24+)
go install github.com/teabranch/abbyfile/cmd/abby@latest

# Pre-built binary (macOS / Linux)
curl -sSL https://raw.githubusercontent.com/teabranch/abbyfile/main/install.sh | sh

# From source
git clone https://github.com/teabranch/abbyfile.git
cd abbyfile
make build && make install
```

The install script accepts `VERSION` (e.g. `VERSION=1.0.0`) to pin a release and `INSTALL_DIR` (default `/usr/local/bin`) to change the install location.

## Overview

```
abby publish              Cross-compile + create GitHub Release
abby install <ref>        Download from GitHub Releases + wire MCP
abby update [name]        Check for newer version, re-download
abby list                 Show installed agents
abby uninstall <name>     Remove binary, MCP entry, registry entry
```

All installed agents are tracked in a registry at `~/.abbyfile/registry.json`.

## Publishing

### Prerequisites

- **`gh` CLI** installed and authenticated ([install guide](https://cli.github.com))
- Agent built and tested locally (`abby build && ./build/<name> validate`)

### Cross-Compile and Release

```bash
# Publish all agents in the Abbyfile
abby publish

# Publish a single agent
abby publish --agent my-agent

# Cross-compile only (skip release creation)
abby publish --dry-run
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

The version comes from the `Abbyfile`:

```yaml
agents:
  my-agent:
    path: agents/my-agent.md
    version: 1.0.0
```

### Dry Run

Use `--dry-run` to verify cross-compilation without creating a release:

```bash
abby publish --dry-run
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
abby install github.com/owner/repo/agent-name

# Install specific version
abby install github.com/owner/repo/agent-name@1.2.0

# When repo name matches agent name, the /agent segment is optional
abby install github.com/owner/my-agent
abby install github.com/owner/my-agent@1.0.0

# Install globally
abby install -g github.com/owner/repo/agent-name
```

### What Happens During Remote Install

1. **Resolve release** -- fetches the latest (or specified) release from GitHub
2. **Find asset** -- matches `<agent>-<os>-<arch>` for your platform
3. **Download** -- downloads the binary to a temp file
4. **Verify** -- runs `<binary> --describe` to confirm it's a valid agent
5. **Install** -- moves to `.abbyfile/bin/` (or `/usr/local/bin/` with `-g`)
6. **Wire MCP** -- updates MCP config for detected runtimes (Claude Code `.mcp.json`, Codex `.codex/config.toml`, Gemini `.gemini/settings.json`)
7. **Track** -- records the install in `~/.abbyfile/registry.json`

### Private Repositories

Authentication is resolved automatically in this order:

1. **`GITHUB_TOKEN` env var** — checked first
2. **`gh auth token`** — if the `gh` CLI is installed and authenticated, its token is used as a fallback

This means if you can run `abby publish` (which requires `gh` CLI auth), you can install from private repos automatically — no extra setup needed.

```bash
# Option 1: Explicit token
export GITHUB_TOKEN=ghp_your_token_here
abby install github.com/your-org/private-repo/agent

# Option 2: gh CLI (no env var needed)
gh auth login                    # one-time setup
abby install github.com/your-org/private-repo/agent
```

The token is also used for GitHub API rate limiting on public repos.

### Install-Time Config Overrides

Override settings at install time without editing config files:

```bash
abby install --model opus github.com/owner/repo/agent
```

This writes the override to `~/.abbyfile/<name>/config.yaml`. The agent's `--describe` manifest and MCP instructions reflect the overridden value immediately. You can change it later with `<agent> config set model <value>` or revert with `<agent> config reset model`.

### Local Install (unchanged)

Local installs from `./build/` continue to work as before, and now also track in the registry:

```bash
abby build
abby install my-agent                  # .abbyfile/bin/ + MCP config (auto-detected runtimes) + registry
abby install -g my-agent               # /usr/local/bin/ + global MCP config + registry
abby install --runtime codex my-agent  # target Codex specifically
```

## Updating

### Update All Remote Agents

```bash
abby update
# my-agent: 1.0.0 → 1.1.0
# other-agent: already up to date (v0.2.0)
```

### Update a Specific Agent

```bash
abby update my-agent
```

`update` only works for agents installed from a remote source. Locally-installed agents show a hint:

```
my-agent: installed from local build, skipping (use 'abby build && abby install my-agent' to update)
```

## Listing Installed Agents

```bash
abby list
```

Output:

```
NAME          VERSION  SOURCE                              SCOPE   PATH
my-agent      1.0.0    github.com/owner/repo/my-agent      local   /path/.abbyfile/bin/my-agent
other-agent   0.2.0    local                               global  /usr/local/bin/other-agent
```

Shows all agents tracked in the registry regardless of source.

## Uninstalling

```bash
abby uninstall my-agent
# Removed /path/.abbyfile/bin/my-agent
# Updated .mcp.json (claude-code)
# Updated .codex/config.toml (codex)
# Uninstalled my-agent
```

Uninstall performs three actions:

1. **Removes the binary** from its installed path
2. **Unwires MCP** -- removes the entry from all detected runtime configs (or specify `--runtime`)
3. **Removes from registry** -- cleans up `~/.abbyfile/registry.json`

## Registry

All installs (local and remote) are tracked in `~/.abbyfile/registry.json`:

```json
{
  "agents": {
    "my-agent": {
      "name": "my-agent",
      "source": "github.com/owner/repo/my-agent",
      "version": "1.0.0",
      "path": "/Users/you/.abbyfile/bin/my-agent",
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
abby build
./build/my-agent validate
./build/my-agent --describe

# 2. Verify cross-compilation
abby publish --dry-run

# 3. Bump version in Abbyfile if needed
# 4. Publish to GitHub
abby publish --agent my-agent
```

### Installing an Agent from a Team

```bash
# Install from your team's repo
abby install github.com/your-org/agents/code-reviewer

# Your runtime auto-discovers it via MCP config
# Later, check for updates
abby update code-reviewer
```

### Managing Your Agents

```bash
# See what's installed
abby list

# Update everything
abby update

# Remove an agent you no longer need
abby uninstall old-agent
```

## Plugin Distribution

When using `--plugin`, each agent also gets a self-contained plugin directory that can be shared:

```bash
abby build --plugin
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

The release does not have a binary for your platform. Check the release assets match the `<agent>-<os>-<arch>` naming convention. Run `abby publish` to create properly-named assets.

### "downloaded binary is not a valid agent"

The binary failed the `--describe` verification check. This means the asset is not a valid Abbyfile agent binary. Ensure the release was created with `abby publish`.

### "GitHub API error (HTTP 403)"

Rate limited. Set `GITHUB_TOKEN`:

```bash
export GITHUB_TOKEN=ghp_your_token
abby install github.com/owner/repo/agent
```

### "agent is not installed (not found in registry)"

The agent was installed before the registry existed, or was installed manually. Re-install it:

```bash
abby install github.com/owner/repo/agent
```
