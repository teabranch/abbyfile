# Development

Guide for contributing to the Abbyfile framework itself.

## Prerequisites

- Go 1.24+
- Make

## Build Commands

```bash
make all          # fmtcheck → vet → test → build
make build        # build the abby CLI → build/abby
make agents       # build agent binaries from Abbyfile
make integration  # end-to-end tests against built binary
make install      # install abby CLI to /usr/local/bin
make clean        # remove build artifacts
```

## Testing

```bash
# Unit tests (with race detector)
make test

# Integration tests (builds CLI + test agent, exercises all subcommands)
make integration

# Manual end-to-end
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

3. **CI does the rest.** The pipeline runs 4 jobs in order:

```
test → e2e-install → check-version → release
       ↓                ↓               ↓
       Build & verify   Compare const   Cross-compile for
       test agent       vs git tags     4 platforms, create
                                        GitHub Release + tag
```

- **test**: lint, vet, unit tests, integration tests
- **e2e-install**: builds CLI, creates a test agent, publishes (dry-run), installs, verifies
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
pkg/definition/     Abbyfile YAML + agent .md parser
pkg/builder/        Code generation + go build compilation
pkg/tools/          Tool registry, executor, validation
pkg/memory/         File-based KV store, limits, concurrency-safe manager
pkg/prompt/         Embed.FS loader with override support
pkg/mcp/            MCP-over-stdio bridge
pkg/plugin/         Claude Code plugin directory generation (--plugin)
pkg/registry/       Installed agents tracking (~/.abbyfile/registry.json)
pkg/github/         GitHub Releases client for remote install/update
internal/cli/       Cobra commands: root, run-tool, memory, serve-mcp, validate
cmd/abby/      CLI: build, install, publish, list, update, uninstall
```

## CLI Reference

```bash
# Build
abby build                   # build all agents (auto-finds Abbyfile or abbyfile.yaml)
abby build --agent my-agent  # build a single agent
abby build -o ./dist         # custom output directory
abby build --plugin          # also generate Claude Code plugin directories

# Install
abby install my-agent                            # install locally from ./build/
abby install -g my-agent                         # install globally (/usr/local/bin/)
abby install github.com/owner/repo/agent         # install from GitHub Releases
abby install github.com/owner/repo/agent@1.0.0   # specific version

# Publish
abby publish                 # cross-compile + create GitHub Release
abby publish --agent my-agent
abby publish --dry-run       # cross-compile only, no release

# Manage
abby list                    # show installed agents
abby update                  # update all remote agents
abby update my-agent         # update a specific agent
abby uninstall my-agent      # remove binary + MCP entry + registry
```

## Built-in Tools

Agents declare tools by Claude Code name in their `.md` frontmatter:

| Declare | MCP tool | Description |
|---------|----------|-------------|
| `Read` | `read_file` | Read file contents |
| `Write` | `write_file` | Write file with dir creation |
| `Edit` | `edit_file` | Find-and-replace in file |
| `Bash` | `run_command` | Shell command execution |
| `Glob` | `glob_files` | File pattern matching |
| `Grep` | `grep_search` | Regex content search |

Memory tools (`memory_read`, `memory_write`, `memory_list`, `memory_delete`) are added automatically when `memory` is set.
