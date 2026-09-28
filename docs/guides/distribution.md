---
title: Distribution
parent: Guides
nav_order: 5
---

# Distribution Guide

Abbyfile agents compile to standalone binaries. The distribution layer handles the full lifecycle: publish to GitHub Releases, install from remote, update, list, and uninstall.

## Installing the abby CLI

Before you can build or install agents, you need the `abby` CLI itself:

```bash
# Go users (requires Go 1.26+)
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
4. **Verify checksum** -- checks the download against the release's checksum asset (fails without one, unless `--insecure-skip-checksum`) -- see [Checksums](#checksums)
5. **Verify manifest** -- runs `<binary> --describe` to confirm it's a valid agent
6. **Plan** -- plans the MCP config change for every targeted runtime before touching anything else; a planning failure here leaves no binary installed and no config written -- see [What abby changes, and how to preview it](#what-abby-changes-and-how-to-preview-it)
7. **Install** -- moves the verified binary to `.abbyfile/bin/` (or `/usr/local/bin/` with `-g`)
8. **Wire MCP** -- applies the planned MCP config change for each targeted runtime (Claude Code `.mcp.json`, Codex `.codex/config.toml`, Gemini `.gemini/settings.json`)
9. **Track** -- records the install in `~/.abbyfile/registry.json`

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

This writes the override to `~/.abbyfile/<name>/config.yaml`. The agent's `--describe` manifest and MCP instructions reflect the overridden value immediately. You can change it later with `<agent> config set model <value>` or revert with `<agent> config reset model`. Under `--dry-run`, this prints `would set model override: <name> → <value>` instead, and writes nothing.

### Local Install (unchanged)

Local installs from `./build/` continue to work as before, and now also track in the registry:

```bash
abby build
abby install my-agent                  # .abbyfile/bin/ + MCP config (auto-detected runtimes) + registry
abby install -g my-agent               # /usr/local/bin/ + global MCP config + registry
abby install --runtime codex my-agent  # target Codex specifically
```

## Where abby registers agents

abby registers each installed agent as an MCP server entry in the target runtime's own config file. `--runtime` (default `auto`) selects which runtimes a given command targets: `auto` detects installed runtimes (falling back to Claude Code, project scope, if none is detected), `all` targets every supported runtime, or name one directly (`claude-code`, `codex`, `gemini`).

| Runtime | Scope | Config file | Env override |
|---------|-------|-------------|---------------|
| Claude Code | project (default) | `./.mcp.json`, in the **current directory** (not the git root) | — |
| Claude Code | user (`--global`) | `$CLAUDE_CONFIG_DIR/.claude.json` if set, else `~/.claude.json` | `CLAUDE_CONFIG_DIR` |
| Codex | project (default) | `./.codex/config.toml` | — |
| Codex | user (`--global`) | `$CODEX_HOME/config.toml` if set, else `~/.codex/config.toml` | `CODEX_HOME` |
| Gemini CLI | project (default) | `./.gemini/settings.json` | — |
| Gemini CLI | user (`--global`) | `~/.gemini/settings.json` | — |

### Detection

`--runtime auto` detects a runtime if its CLI is on `PATH`, or its config directory exists (Claude: `$CLAUDE_CONFIG_DIR` or `~/.claude/`; Codex: `$CODEX_HOME` or `~/.codex/`; Gemini: `~/.gemini/`) — never by checking whether `$HOME` exists. If nothing is detected, abby falls back to Claude Code, project scope.

### CLI vs. file

Every change abby makes is planned first, then applied through the runtime's own CLI when it can faithfully express the entry, and otherwise through a surgical, backed-up edit of the runtime's config file:

- **Claude Code** uses `claude mcp add-json -s <project|user>` for both scopes — unless the existing entry already has keys `add-json` would drop (it keeps only `type`, `command`, `args`, `env` and `timeout`), in which case abby edits `.mcp.json`/`.claude.json` directly. Replacing an existing entry runs `mcp remove` then `mcp add-json`, both in the project root for project scope; if `add-json` fails after the `remove`, abby automatically re-adds the previous entry (and, if that also fails, prints its JSON so you can restore it by hand). Removing an entry that's already absent is treated as success, not an error.
- **Gemini CLI** uses `gemini mcp add` only at **user** scope, and only when doing so wouldn't erase an existing `cwd` or `timeout`, drop a key abby doesn't own, or misinterpret an argument starting with `-` as a flag. Project scope, and any change the CLI can't safely express, uses the file method — Gemini's CLI has no way to set `cwd`, which project-scope entries need.
- **Codex** always uses the file method: `codex mcp add` only writes the global config and can't set `cwd` or timeouts.

Pass `--config-method file` to always edit the config file directly, or `--config-method cli` to require the CLI (an error names the reason when a change can't safely use it). The default, `auto`, is the rule above. Set the default for a whole session with `ABBY_CONFIG_METHOD=auto|cli|file`; the `--config-method` flag takes precedence when both are given.

## What abby changes, and how to preview it

Add `--dry-run` to `abby install`, `abby build` or `abby uninstall` to see every change without touching anything: no binary is copied, no config file is written, no registry entry changes, and no backup is made. (A remote install still downloads and checksum-verifies the release into a temp file under `--dry-run`, so the preview reflects a binary abby actually checked.) `abby update` has no `--dry-run` flag.

Every command prints a summary table of what it changed (or, under `--dry-run`, would change):

```
Runtime config changes:
RUNTIME      SCOPE    METHOD  TARGET                     SERVER    BACKUP
claude-code  project  file    /path/to/project/.mcp.json  my-agent  /path/to/project/.mcp.json.abbyfile.bak
```

- **METHOD** is `cli` or `file`; `(unchanged)` is appended when the change was a no-op (for example, re-running an identical install).
- **BACKUP** is `-` unless this run made abby's one-time backup of that file, in which case the path is shown.
- Caveats — for example "Codex loads project `.codex/config.toml` only in trusted projects", or that a running Claude Code can overwrite a user-scope edit — print once per command, after the table, as `note: ...` lines.

abby plans every targeted runtime's change **before** applying any of them: if planning fails for one runtime — an existing config file that doesn't parse, or a `--config-method cli` change that can't be expressed by the CLI — nothing is written anywhere, and (for install) no binary is copied.

### Backups

A backup is only made for a **file-method** change: before abby's first write to an existing config file in a session, it copies the original to `<file>.abbyfile.bak`, next to the file, with the same permissions, and never overwrites that backup afterwards — restore from it by hand if needed. A **CLI-method** change makes no backup (the BACKUP column shows `-`), since the runtime's own CLI is the one writing the file.

abby doesn't touch your project's `.gitignore`. Since a project-scope backup such as `.mcp.json.abbyfile.bak` lands in the project root right next to a config file that's often committed, add `*.abbyfile.bak` to your project's `.gitignore` so a backup never gets committed by accident.

### Keys abby owns

abby only ever sets `command`, `args`, `cwd` (project scope; Codex and Gemini only), the timeout key(s), `env` (only when `--env` is given), and — for Claude Code only — a constant `type: "stdio"`. Every other key already in an entry — and every other entry, table or key in the file — is preserved:

- **JSON** (Claude Code, Gemini): the file is parsed and rewritten key-by-key, in order; untouched values are byte-identical apart from re-indentation, and numbers are never round-tripped through a float.
- **TOML** (Codex): only the `[mcp_servers.<name>]` block (and its subtables) is replaced; comments and other tables elsewhere in the file are kept. Comments *inside* the replaced block are lost. The first edit to a file also normalizes any run of multiple blank lines between top-level tables down to a single blank line; a CRLF file stays CRLF.

### Refusals

abby refuses to touch a config file it can't safely round-trip, and writes nothing when it does:

- The file isn't valid JSON/TOML (comments and trailing commas aren't valid JSON — Gemini's `settings.json` is JSON and doesn't support them).
- A JSON object has a duplicate key — the error names the key.
- A Codex entry is defined in inline-table form (`x = { ... }` instead of a `[mcp_servers.name]` block) — abby only edits the table form.
- The file changed on disk between plan and apply (for example, a running runtime rewrote it): abby re-plans once and retries, then refuses with an error naming the file.

## Entry fields

Beyond `command` and `args`, abby sets:

- **`cwd`** (project scope; Codex and Gemini only — Claude Code's entry format has no `cwd` field): the project's absolute directory, so a relative `sandbox.allowed_dirs: ["."]` in the agent resolves against the right place regardless of where the runtime itself was started.
- **`env`**: set with repeatable `--env KEY=VALUE` flags. Without `--env`, an existing entry's `env` is left alone (a reinstall or `abby update` never erases it); with `--env`, the given keys replace the entry's `env` entirely. At project scope, abby warns on stderr that the values land in a file that's often committed, and suggests `${VAR}` references instead (Claude Code and Gemini CLI expand them).
- **timeout**: abby runs the agent's `--describe` and sets the runtime's per-call timeout to the agent's largest effective limit — `max(toolTimeout, sandbox.maxCommandTimeout` if the agent has `run_command`) — plus a 10-second margin. If `--describe` fails (for example, a cross-compiled binary that can't run on this machine), the timeout is omitted and the install/build still succeeds. It is recomputed on every install/build, so reinstalling refreshes it after the agent's limits change; `abby doctor` warns when a written timeout is smaller than what the current binary would need. It's written as `timeout` in milliseconds for Claude Code and Gemini (only when ≥ 1 second), and as Codex's `startup_timeout_sec = 30` (constant) plus `tool_timeout_sec` (ceiling of seconds) — a hand-edited float such as `tool_timeout_sec = 120.5` is read back rounded up (121s).

## Checksums

`abby install` (remote) requires the release to carry a checksum asset — `<agent>-sha256sums.txt`, `SHA256SUMS`, or `checksums.txt`, checked in that order — and verifies the downloaded binary against it **before** the binary is made executable or run for `--describe`. Install fails if the asset is missing, fails to download, has no entry for this platform's binary, or the hash doesn't match.

Pass `--insecure-skip-checksum` to bypass this, with a prominent warning printed to stderr. There is no equivalent bypass for `abby update`, which always requires a checksum.

`abby publish` has emitted `<agent>-sha256sums.txt` alongside the platform binaries since v0.7.0, so every agent published with `abby publish` already has one.

## `abby doctor`

`abby doctor [agent]...` checks every installed agent (or just the ones named) against its registry entry:

- the binary exists and is executable;
- `--describe` succeeds, run in the agent's own project root (so a sandbox's `allowed_dirs` reports the agent's directory, not wherever `doctor` was invoked from) — and shows the effective sandbox and any sandbox warnings;
- the MCP handshake succeeds on both protocol eras (`server/discover` for 2026-07-28, `initialize` for the legacy 2025-11-25);
- each targeted runtime has an entry for the agent, it points at the registered binary, and its timeout isn't stale versus what the current binary needs (a warning suggests reinstalling);
- Codex project-scope entries get a reminder that Codex only loads `.codex/config.toml` in trusted projects.

It also reports any entries left in the legacy `~/.claude/mcp.json` (written by abby ≤ v0.11; Claude Code never reads that file), with a removal hint. `doctor` only ever reads config — it never writes, backs up, or creates a `.abbyfile.bak`. It exits non-zero if any check failed; warnings alone don't fail the command. `--runtime` and `--config-method` are accepted like the other commands, for symmetry.

Sample output:

```
$ abby doctor
my-agent (v1.0.0, local, /Users/you/project/.abbyfile/bin/my-agent)
  ✓ binary /Users/you/project/.abbyfile/bin/my-agent
  ✓ sandbox: bash=restricted allow_commands=["go test ./..."] allowed_dirs=/Users/you/project
  ✓ serve-mcp speaks 2026-07-28 (server/discover)
  ✓ serve-mcp speaks 2025-11-25 (initialize)
  ✓ claude-code /Users/you/project/.mcp.json → /Users/you/project/.abbyfile/bin/my-agent
legacy config
  ! /Users/you/.claude/mcp.json has entries old-agent written by abby < v0.12; Claude Code never reads this file — reinstall them with `abby install --global`, then delete /Users/you/.claude/mcp.json
```

(Adapted from a captured run: the check wording, marks and no-blank-line structure are exact; the agent's version, sandbox command list and paths were simplified for the page.)

### Migrating from v0.11

1. `abby install` requires a checksum asset. Pass `--insecure-skip-checksum` to bypass.
2. Claude Code user-scope entries now go into `~/.claude.json` (or `$CLAUDE_CONFIG_DIR/.claude.json`), or through `claude mcp add-json`. Old `~/.claude/mcp.json` entries are reported by `abby doctor` with a removal hint.
3. Runtimes are detected by CLI on PATH or by config dir, not by `$HOME` existing.
4. Config edits refuse unparsable files (including JSON with duplicate keys, named in the error), keep unknown keys and comments outside abby's block, write atomically, and back up once.
5. Entries now carry `cwd`, `env` and a timeout. Codex project config loads only in trusted projects.
6. `--dry-run`, `--config-method`, `--env` and `abby doctor` are new.

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

`update` re-installs from the newer release using the same mandatory checksum verification as `abby install` (there is no `--insecure-skip-checksum` for `update`), replaces the binary in place at its existing path, and refreshes each runtime's `command` path and timeout — while leaving any existing `env` untouched, the same as a reinstall. It prints the same runtime-config summary table as install/uninstall, but has neither a `--dry-run` nor a `--config-method` flag — `ABBY_CONFIG_METHOD` is the only way to change its config method. A per-agent failure (for example a release with no checksum asset) is reported on stderr and that agent is skipped; the rest of the batch still runs, and `update` then exits non-zero with `N agent(s) failed to update`.

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

```
$ abby uninstall my-agent
Removed /path/to/project/.abbyfile/bin/my-agent
remove my-agent in /path/to/project/.mcp.json (claude-code, project scope, via file):
    - {
    -   "type": "stdio",
    -   "command": "/path/to/project/.abbyfile/bin/my-agent",
    -   "args": ["serve-mcp"],
    -   "timeout": 130000
    - }

Runtime config changes:
RUNTIME      SCOPE    METHOD  TARGET                       SERVER    BACKUP
claude-code  project  file    /path/to/project/.mcp.json  my-agent  -
Uninstalled my-agent
```

Uninstall performs three actions:

1. **Removes the binary** from its installed path
2. **Unwires MCP** -- removes the entry from all detected runtime configs (or specify `--runtime`)
3. **Removes from registry** -- cleans up `~/.abbyfile/registry.json`

Every targeted runtime is attempted even if an earlier one fails. If any runtime's removal fails, abby prints the errors, **keeps the registry entry** (so `abby uninstall` can be re-run once the problem is fixed), and exits non-zero. Add `--dry-run` to preview the removal without deleting the binary, editing any config, or touching the registry.

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
