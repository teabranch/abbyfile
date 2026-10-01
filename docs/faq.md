---
title: FAQ
nav_order: 5
---

# FAQ

## Does the binary call the Claude API?

No. The binary does not call any LLM API. Claude Code is the LLM runtime. The binary is a packaging format that exposes tools, prompts, and memory through CLI subcommands and an MCP server. Claude Code connects to the binary via MCP, loads its instructions, and decides when to call its tools.

## Why not just use CLAUDE.md?

CLAUDE.md works well for repo-specific instructions in a single project. Abbyfile solves different problems:

- **Versioning** -- agent logic has semver, can be pinned and rolled back
- **Testing** -- tools, memory, and prompts are unit-testable Go code
- **Memory** -- persistent key-value store across conversations
- **Sharing** -- distribute as a single binary via `go install` or binary release
- **Validation** -- `validate` subcommand checks that tools exist, memory is writable, prompts load
- **Machine-readable** -- `--describe` returns a JSON manifest

They are not mutually exclusive. A project can have both a CLAUDE.md and Abbyfile agents registered via MCP config.

## When should I use skills vs sub-agents vs Abbyfile agents?

Quick decision guide:

- **Need just instructions?** Use Agent Skills — markdown files with progressive disclosure, zero infrastructure
- **Need context isolation?** Use Sub-agents — separate context windows for exploratory or one-shot tasks
- **Need tools + memory + versioning?** Use Abbyfile — executable MCP tools, persistent memory, and one-command distribution at marginal context cost

These approaches compose well together. An Abbyfile agent can coexist with skills in the same project, and sub-agents can invoke Abbyfile agents' MCP tools. See the **[benchmark comparison](./guides/benchmarks.md#skills-vs-sub-agents-vs-abbyfile)** for measured cost data.

## Can I use this without Claude Code?

Yes. Abbyfile supports Claude Code, Codex, and Gemini CLI out of the box. The `serve-mcp` subcommand starts a standard MCP-over-stdio server, so any MCP client can connect to it. Use `--runtime` on `build`/`install` to target your preferred runtime.

You can also use the CLI directly:

```bash
./my-agent run-tool date
./my-agent memory read notes
./my-agent --custom-instructions
```

## How do I share agents?

The recommended way is `abby publish` + `abby install`:

```bash
# Publisher: cross-compile and create a GitHub Release
abby publish --agent my-agent

# Consumer: one-command install
abby install github.com/your-org/repo/my-agent
```

This cross-compiles for macOS and Linux (amd64 + arm64), creates a GitHub Release via the `gh` CLI with an `<agent>-sha256sums.txt` checksum asset, and lets anyone install with a single command.

Other options:

- **Source** -- share the `Abbyfile` and agent `.md` files and let users run `abby build`
- **`go install`** -- `go install github.com/you/your-agent@latest` if you structure the repo as a Go module
- **Manual binary release** -- build with `GOOS=linux GOARCH=amd64 go build` and distribute however you like

Since agents compile to static Go binaries, they have no runtime dependencies.

## How do I override agent settings without rebuilding?

Use the `config` subcommand:

```bash
./my-agent config set model opus          # override the model hint
./my-agent config set tool_timeout 120s   # override tool timeout
./my-agent config get                     # see all settings with source
./my-agent config reset model             # revert to compiled default
./my-agent config set sandbox.allow_commands '["go test ./..."]'  # sandbox override
```

Overrides are stored at `~/.abbyfile/<name>/config.yaml`. `context_budget.*` and `sandbox.*` fields can be set the same way. Restart the runtime session (for example Claude Code) after a change — a running MCP session keeps the old settings. You can also set overrides at install time: `abby install --model opus github.com/owner/repo/agent`.

## What about secrets and configuration?

Do not embed secrets in the binary. Use environment variables:

```go
func myTool() *tools.Definition {
    return tools.BuiltinTool("deploy", "Deploy the app", schema,
        func(input map[string]any) (string, error) {
            token := os.Getenv("DEPLOY_TOKEN")
            if token == "" {
                return "", fmt.Errorf("DEPLOY_TOKEN not set")
            }
            // ...
        },
    )
}
```

The binary reads env vars at runtime. Nothing sensitive is compiled in.

To have the runtime pass an env var to the MCP server, set it on the entry at install time with the repeatable `--env KEY=VALUE` flag. Project-scope config files are often committed, so prefer a `${VAR}` reference over a literal secret (Claude Code and Gemini CLI expand it):

```bash
abby install --env 'DEPLOY_TOKEN=${DEPLOY_TOKEN}' github.com/owner/repo/agent
```

`--dry-run` and the change summary show env values as `***`. See [Entry fields](./guides/distribution.md#entry-fields).

## How is memory stored?

Plain text files at `~/.abbyfile/<agent-name>/memory/`. Each key is a `.md` file. The content is whatever string the agent writes -- there is no enforced format. You can inspect and edit memory files directly:

```bash
ls ~/.abbyfile/my-agent/memory/
cat ~/.abbyfile/my-agent/memory/notes.md
```

## Can two agents share memory?

Not directly. Each agent has its own memory directory based on its name. If two agents need to share state, they can:

- Read each other's files from the filesystem (only if that directory is inside the reading agent's `sandbox.allowed_dirs`)
- Use a shared external store (database, file) accessed via custom tools
- Have Claude Code mediate between them using MCP tool calls

## What happens if a tool command is not found?

The `validate` subcommand catches this:

```
[FAIL] Tool "lint": command "golangci-lint" not found in PATH
```

At runtime, `run-tool` returns an error: `tool "lint": command "golangci-lint" not found in PATH`.

The MCP bridge returns the error to the client with `IsError: true`.

## Why does `run_command` refuse every call?

Built-in tools are sandboxed. An agent with `tools: Bash` needs commands listed in `sandbox.allow_commands` (or `sandbox.bash: unrestricted`); until then `run_command` refuses every call, and `abby build` prints a note. Restricted mode runs commands without a shell, so pipes, redirects and `&&` need `bash: unrestricted`. File tools are likewise confined to `sandbox.allowed_dirs` (default: the working directory).

```yaml
sandbox:
  allow_commands: ["go test ./...", "go vet ./..."]
```

See [Tools → Sandbox](./guides/tools.md#sandbox).

## How do I update the system prompt?

For development: write `~/.abbyfile/<name>/override.md`. Takes effect immediately without rebuilding.

For production: edit the agent's `.md` file body, bump the version in the `Abbyfile`, run `abby build`.

## Can I have multiple prompts or dynamic prompts?

The embedded prompt is a single file. For dynamic behavior, use the system prompt to describe conditional behavior based on tool results and memory contents. Claude Code handles the reasoning.

If you need to inject context from memory into the prompt, the MCP bridge exposes a `memory-context` prompt template that MCP clients can request.

## What Go version is required?

Go 1.26 or later. The `go.mod` specifies `go 1.26.0`.

## How do I add the agent to an existing project?

1. Create an `Abbyfile` (YAML) and an agent `.md` file with dual frontmatter
2. Build: `abby build`
3. MCP config is auto-generated for detected runtimes (use `--runtime` to target a specific one)

## What is the `abby build` command?

Reads your `Abbyfile`, parses each agent's `.md` file, generates Go source, and compiles standalone binaries:

```bash
abby build              # build all agents
abby build --agent foo  # build a single agent
abby build --plugin     # also generate Claude Code plugin directories
```

Flags: `-f` (Abbyfile path), `-o` (output dir), `--agent` (single agent), `--plugin` (generate plugin dir), `--subagent` (also emit a Claude Code sub-agent), `--parallelism` (max concurrent builds), `--runtime` (target runtime: auto, all, claude-code, codex, gemini), `--config-method` (auto, cli, file), `--dry-run` (show planned changes without building or writing anything).

## What is a plugin?

A Claude Code plugin directory that wraps the agent binary. The `--plugin` flag generates it alongside the normal binary build. The plugin includes the binary, an MCP config, and any skills declared in the agent's `.md` frontmatter.

```
build/my-agent.claude-plugin/
  .claude-plugin/plugin.json
  .mcp.json
  my-agent
  skills/review-pr/SKILL.md
```

Test locally with `claude --plugin-dir ./build/my-agent.claude-plugin/`. See the [Plugins Guide](./guides/plugins.md).

## What are skills?

Skills are markdown files that provide Claude Code with specialized capabilities (like `/review-pr` or `/write-tests`). They are declared in the agent's `.md` frontmatter and packaged into the plugin directory:

```yaml
skills:
  - name: review-pr
    description: "Review a pull request"
    path: skills/review-pr.md
```

Skills require the `--plugin` flag — they are a plugin feature, not a binary feature.

## How do I debug MCP communication?

Start with `abby doctor`: it checks each installed agent's binary, its entry in every runtime config, the MCP handshake on both protocol eras, and the effective sandbox. See [`abby doctor`](./guides/distribution.md#abby-doctor).

Agent logs go to stderr. Redirect them:

```bash
./my-agent serve-mcp 2>agent.log
```

The MCP protocol itself runs over stdin/stdout. The separation means logs never corrupt the protocol stream.

## What MCP SDK does Abbyfile use?

The official Go MCP SDK: `github.com/modelcontextprotocol/go-sdk`. Version `v1.8.0` as of the current `go.mod`. Agents speak MCP 2026-07-28 (`server/discover`) and still accept legacy `initialize` clients on 2025-11-25 and 2025-06-18 — see [Protocol Versions](./guides/mcp.md#protocol-versions).

## How do I publish an agent?

Use `abby publish`. It cross-compiles for 4 platforms and creates a GitHub Release via the `gh` CLI:

```bash
abby publish --agent my-agent
```

Requires the `gh` CLI to be installed and authenticated. Use `--dry-run` to test cross-compilation without creating a release. See the [Distribution Guide](./guides/distribution.md).

## How do I install an agent from GitHub?

```bash
abby install github.com/owner/repo/agent-name
abby install github.com/owner/repo/agent-name@1.0.0
```

This downloads the binary for your platform, verifies it against the release's checksum asset and with `--describe`, installs it, and wires up MCP. The checksum is mandatory: a release without one fails to install unless you pass `--insecure-skip-checksum`. Add `--dry-run` to preview every change without installing anything. For private repos, set `GITHUB_TOKEN` or log in with `gh auth login`. See the [Distribution Guide](./guides/distribution.md#checksums).

## Where does abby register agents?

In each runtime's own config file — project scope by default, user scope with `--global`:

| Runtime | Project | `--global` |
|---------|---------|------------|
| Claude Code | `./.mcp.json` | `$CLAUDE_CONFIG_DIR/.claude.json` if set, else `~/.claude.json` |
| Codex | `./.codex/config.toml` | `$CODEX_HOME/config.toml` if set, else `~/.codex/config.toml` |
| Gemini CLI | `./.gemini/settings.json` | `~/.gemini/settings.json` |

abby uses the runtime's CLI when it can express the entry (Codex always uses the file), otherwise it edits the file, making a one-time `<file>.abbyfile.bak` backup first. Force one method with `--config-method cli|file` or `ABBY_CONFIG_METHOD`. See [Where abby registers agents](./guides/distribution.md#where-abby-registers-agents).

## My global agent doesn't show up in Claude Code, and I have a `~/.claude/mcp.json`

abby versions before v0.12 wrote user-scope entries to `~/.claude/mcp.json`, which Claude Code never reads. `abby doctor` reports those leftovers. Reinstall the agents with `abby install --global` (which writes `~/.claude.json`), then delete `~/.claude/mcp.json`.

## How do I update installed agents?

```bash
abby update              # update all remote agents
abby update my-agent     # update a specific agent
```

Only agents installed from a remote source can be auto-updated. For locally-built agents, rebuild and reinstall.

`update` always verifies the release checksum (there is no bypass flag), replaces the binary at its existing path, and keeps any `env` set on the runtime entries. If any agent fails to update, the rest still run and the command exits non-zero.

## Where is the registry file?

`~/.abbyfile/registry.json`. It tracks all installed agents (local and remote) with their source, version, path, and scope. You can inspect it directly, but it's managed by `abby install`, `abby uninstall`, and `abby update`.

## How do I uninstall an agent?

```bash
abby uninstall my-agent
```

This removes the binary, unwires it from all detected runtime configs, and removes it from the registry. Add `--dry-run` to preview the removal. If a config removal fails, the registry entry is kept so you can fix the problem and re-run `abby uninstall`.
