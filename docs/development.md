---
title: Development
nav_order: 6
---

# Development

Guide for contributing to the Abbyfile framework itself.

## Prerequisites

- Go 1.26+
- Make

## Build Commands

```bash
make all          # fmtcheck → vet → test → build
make build        # build the abby CLI → build/abby
make agents       # build agent binaries from Abbyfile
make integration  # end-to-end tests against built binary
make bench        # benchmarks (also bench-integration, bench-report, bench-all)
make install      # install abby CLI to /usr/local/bin (or $PREFIX/bin)
make clean        # remove build/, .abbyfile/, .mcp.json, .codex/config.toml, .gemini/settings.json
```

`make agents` (and any `abby build` in the repo) writes project-scope MCP config for every detected runtime, possibly through the `claude`/`gemini` CLIs. Add `--dry-run` to preview, or set `ABBY_CONFIG_METHOD=file` to keep abby away from the runtime CLIs. `make clean` deletes those config files outright.

## Testing

```bash
# Unit tests (with race detector)
make test

# Integration tests (builds CLI + test agent, exercises all subcommands)
make integration

# Manual end-to-end
make build && ./build/abby build --dry-run   # preview the MCP config changes
make build && ./build/abby build
./build/go-pro validate
./build/go-pro --describe
```

## Releasing a New Version

The abby CLI uses **auto-release**: bump the version constant, push to main, and CI handles the rest.

### Steps

1. **Bump the version** in `cmd/abby/main.go`:

```go
const cliVersion = "0.3.0"  // ← change this
```

2. **Commit and push to main:**

```bash
git add cmd/abby/main.go
git commit -m "Bump version to 0.3.0 for release"
git push
```

3. **CI does the rest.** The pipeline runs 4 jobs. On a push to main, `e2e-install` and `check-version` run in parallel after `test`, and `release` waits for all three:

```
test ─┬─ e2e-install ───┬─ release
      └─ check-version ─┘
```

- **test**: format check, vet, unit tests (`-race`), integration tests (also runs on pull requests)
- **e2e-install**: builds the CLI, creates a test agent, publishes a real GitHub Release for it (binaries + checksum file), installs it from that release (checksum-verified), runs `--describe`/`--version`/`validate`, then deletes the release
- **check-version**: extracts `cliVersion` from source, checks if `v<version>` tag exists
- **release**: if tag is new, cross-compiles (`darwin/linux × amd64/arm64`), creates GitHub Release with tag `v<version>`

### How It Works

The version source of truth is `const cliVersion` in `cmd/abby/main.go:12`. CI extracts it with grep, checks `git rev-parse "v${VERSION}"`, and sets `should_release=true/false`. The release job only runs when the tag doesn't exist yet.

### Common Issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| Release skipped | Tag already exists for current version | Bump `cliVersion` to a new version |
| Tests fail | Code changes broke something | Fix tests before bumping version |
| e2e-install fails | Build or install regression | Check `make integration` locally |

### Version Scheme

Semantic versioning: `MAJOR.MINOR.PATCH`

- **PATCH** (0.3.0 → 0.3.1): bug fixes, doc updates
- **MINOR** (0.3.0 → 0.4.0): new features, non-breaking changes
- **MAJOR** (0.x → 1.0): breaking API changes (reserved for v1.0 stability)

### Don'ts

- Don't create tags manually — CI creates them
- Don't use `git tag` or `git push --tags` — the pipeline handles it
- Don't bump the version without pushing — the tag is created by CI, not locally

## Project Structure

```
Abbyfile           Manifest declaring agents to build (also accepts abbyfile.yaml)
.claude/agents/     Agent .md files (prompt + frontmatter)
build/              Compiled binaries (abby CLI + agents)

pkg/agent/          Core runtime: New(), Execute(), functional options
pkg/builtins/       Shared tool implementations (read, write, edit, bash, glob, grep)
pkg/sandbox/        Built-in tool sandbox: path confinement, command allowlist, argv parsing
pkg/definition/     Abbyfile YAML + agent .md parser
pkg/builder/        Code generation + go build compilation
pkg/tools/          Tool registry, executor, validation, output shaping
pkg/memory/         File-based KV store, limits, concurrency-safe manager
pkg/prompt/         Embed.FS loader with override support
pkg/config/         Runtime overrides (~/.abbyfile/<name>/config.yaml)
pkg/mcp/            MCP-over-stdio bridge (2026-07-28 and older protocol eras)
pkg/plugin/         Claude Code plugin directory generation (--plugin)
pkg/subagent/       Claude Code sub-agent file generation (--subagent)
pkg/runtimecfg/     Runtime MCP config writers (Claude Code, Codex, Gemini; CLI and file methods)
pkg/fsutil/         Atomic writes, ownership preservation, file snapshots
pkg/registry/       Installed agents tracking (~/.abbyfile/registry.json)
pkg/github/         GitHub Releases client for remote install/update, checksum verification
internal/cli/       Cobra commands: root, run-tool, memory, config, serve-mcp, validate
internal/integration/  Integration tests (build tag: integration)
benchmarks/         Context-cost benchmarks
cmd/abby/      CLI: build, install, publish, list, update, uninstall, doctor, diff
```

## CLI Reference

```bash
# Build
abby build                   # build all agents (auto-finds Abbyfile or abbyfile.yaml)
abby build --agent my-agent  # build a single agent
abby build -o ./dist         # custom output directory
abby build --plugin          # also generate Claude Code plugin directories
abby build --subagent        # also emit .claude/agents/<name>.md
abby build --dry-run         # show planned MCP config changes, build nothing

# Install
abby install my-agent                            # install locally from ./build/
abby install -g my-agent                         # install globally (/usr/local/bin/, user-scope MCP config)
abby install github.com/owner/repo/agent         # install from GitHub Releases (checksum-verified)
abby install github.com/owner/repo/agent@1.0.0   # specific version
abby install --all                               # every agent in ./build/
abby install --dry-run my-agent                  # preview every change, write nothing
abby install --runtime codex my-agent            # auto (default), all, claude-code, codex, gemini
abby install --env KEY=VALUE my-agent            # set env on the MCP entry (repeatable)
abby install --config-method file my-agent       # auto | cli | file (or ABBY_CONFIG_METHOD)

# Publish
abby publish                 # cross-compile + create GitHub Release
abby publish --agent my-agent
abby publish --dry-run       # cross-compile only, no release

# Manage
abby list                    # show installed agents
abby update                  # update all remote agents
abby update my-agent         # update a specific agent
abby uninstall my-agent      # remove binary + MCP entry + registry
abby doctor                  # check installed agents: binary, config entries, handshake, sandbox
```

See the [Distribution guide](guides/distribution.md) for where each runtime's config lives (Claude Code user scope is `~/.claude.json`, or `$CLAUDE_CONFIG_DIR/.claude.json`) and how entries are written.

## Built-in Tools

Agents declare tools by Claude Code name in their `.md` frontmatter:

| Declare | MCP tool | Description |
|---------|----------|-------------|
| `Read` | `read_file` | Read file contents |
| `Write` | `write_file` | Write file with dir creation |
| `Edit` | `edit_file` | Find-and-replace in file |
| `Bash` | `run_command` | Run an allowlisted command, no shell (see [Sandbox](guides/tools.md#sandbox)) |
| `Glob` | `glob_files` | File pattern matching |
| `Grep` | `grep_search` | Regex content search |

Memory tools (`memory_read`, `memory_write`, `memory_list`, `memory_delete`, `memory_search`) are added automatically when `memory` is set.
