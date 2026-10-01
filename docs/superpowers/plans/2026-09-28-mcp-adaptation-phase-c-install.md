# MCP 2026-07-28 Adaptation — Phase C (Install and Runtime-Config Safety & UX) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `abby install`, `abby build`, `abby uninstall` and `abby update` register agents with Claude Code, Codex and Gemini CLI safely:
- They use each runtime's own CLI when it can express the entry.
- Otherwise they edit the runtime's config file surgically: never on a parse failure, atomically, with a one-time backup, and without dropping keys abby doesn't own.
- Entries carry `cwd`, `env` and a timeout that won't kill a call the agent allows.
- Downloads must pass checksum verification before anything runs.
- Every change is previewable (`--dry-run`) and summarised.
- `abby doctor` diagnoses an installed agent end to end.

**Architecture:**
- `pkg/fsutil` gains `Snapshot`, a read-then-commit helper. It resolves symlinks, preserves the file mode (0600 for new files), makes a one-time `.abbyfile.bak`, and detects a concurrent writer before an atomic rename.
- `pkg/runtimecfg` is rebuilt around a plan/apply model:
  - `ConfigWriter.PlanAdd` / `PlanRemove` return a `Change` that carries a preview and an `Apply`.
  - File edits go through two surgical editors: an order-preserving JSON object editor that decodes only raw values, and a TOML block editor that replaces or appends only the `[mcp_servers.<name>]` block.
  - CLI-backed methods (`claude mcp add-json/remove`, `gemini mcp add/remove -s user`) are chosen per change, and only when the CLI can express every field.
- `cmd/abby` threads an `installOptions` value through install, build, uninstall and update. It adds `--dry-run`, `--config-method`, `--env` and `--insecure-skip-checksum`, prints one summary table, and gains `abby doctor`.

**Tech Stack:** Go 1.26, cobra, `github.com/BurntSushi/toml` (already a dependency), go-sdk v1.8.0 (client side, for `doctor`), standard `testing`/`httptest`.

**Spec:** `docs/superpowers/specs/2026-09-27-mcp-2026-07-28-adaptation-design.md`: Phase C (C1–C6), defects D7, D8, D9, the Testing bullets "Runtime config" and "Install", and Migration note items 5–6. This plan also carries the Phase B carry-forward to C4: the timeout rule in B3.

## Global Constraints

- **Release: v0.12.0**, bumped via `cliVersion` after merge, not in this plan. Before tagging, run `go test -race ./...` **and** `make integration` locally. CI runs both with `-race`; a test-only race blocked the v0.11.0 tag.
- No new dependencies. go-sdk stays at **v1.8.0**. Do **not** set `MCPGODEBUG`, disable `GOSUMDB`, or use `GONOSUMDB`/`GOINSECURE`/`GOFLAGS=-insecure`.
- **Scope mapping:** a default (project-local) install → `project` scope; `--global` → `user` scope; `abby build` → `project` scope.
- **Config paths (verified 2026-09-28):**
  - Claude project: `./.mcp.json`, in the **current directory**, not the git root (observed with `claude mcp add-json -s project`).
  - Claude user: `$CLAUDE_CONFIG_DIR/.claude.json` if `CLAUDE_CONFIG_DIR` is set, else `~/.claude.json`. The server goes under the top-level `mcpServers` key (observed).
  - Codex project: `./.codex/config.toml`. Codex user: `$CODEX_HOME/config.toml` if `CODEX_HOME` is set, else `~/.codex/config.toml`.
  - Gemini project: `./.gemini/settings.json`. Gemini user: `~/.gemini/settings.json`.
- **Legacy path (D7):** `~/.claude/mcp.json`, which older abby versions wrote and Claude Code never read. This plan never writes it; `doctor` reports it.
- **Claude CLI behaviour (observed with claude 2026-09, isolated `HOME`):**
  - `claude mcp add-json -s <project|user> <name> '<json>'` keeps `type`, `command`, `args`, `env` and `timeout` (ms), and **drops unknown keys**.
  - A second add with the same name exits **1** with `MCP server <name> already exists in …`.
  - `claude mcp remove -s <scope> <name>` exits **1** with `No MCP server named "<name>" in …` when the server is absent.
  - `mcp list` and `mcp get` have no JSON output.
- **Gemini CLI (source-verified):**
  - `gemini mcp add [-s user|project] [-e K=V]... [--timeout <ms>] <name> <command> [args...]` overwrites silently. `-s` defaults to `project`.
  - `gemini mcp remove -s <scope> <name>` on a missing name prints a message and exits 0.
  - The CLI cannot set `cwd`. `trust` is **never** set by abby.
- **Codex CLI (source-verified):** `codex mcp add` writes only the global config and can't set `cwd` or timeouts. **abby always uses the file method for Codex.**
- **Entry fields per runtime (C4):**
  - Claude: `type: "stdio"`, `command`, `args`, `env` (when given), and `timeout` in ms (only when ≥ 1000). Claude has no `cwd` field.
  - Codex: `command`, `args`, `env` (subtable), `cwd`, `startup_timeout_sec = 30`, and `tool_timeout_sec` = ceil(timeout in seconds).
  - Gemini: `command`, `args`, `env`, `cwd`, and `timeout` in ms.
- **Timeout rule (B3 → C4):** runtime timeout = largest effective agent limit + **10s**.
  - The largest effective limit is `max(toolTimeout (default 30s), sandbox.maxCommandTimeout if the agent has run_command)`, read from `--describe`.
  - If `--describe` fails (for example, a cross-compiled binary), omit the timeout. The build or install still succeeds.
- **Owned keys only:** abby sets `command`, `args`, `cwd` (project scope), the timeout keys, and `env` only when `--env` is given. Every other key in an existing entry is preserved.
- **Safe file edits (C2):**
  - If an existing file fails to parse, return an error naming the file and write nothing.
  - Write atomically. Keep the existing mode; new files get 0600.
  - Symlinks are resolved and the **target** is written.
  - Make a one-time backup, `<target>.abbyfile.bak`, with the source's mode; never overwrite it.
  - If the file changed between read and write, re-plan once, then refuse.
- **Detection (C3):** a runtime is detected if its CLI is on PATH or its config dir exists: Claude `$CLAUDE_CONFIG_DIR` or `~/.claude/`, Codex `$CODEX_HOME` or `~/.codex/`, Gemini `~/.gemini/`. Never infer a runtime from `$HOME` existing. When nothing is detected, fall back to Claude project scope.
- **Checksums (C5):**
  - `abby install` fails when the release has no checksum asset (`<agent>-sha256sums.txt`, `SHA256SUMS`, `checksums.txt`), when that asset's download fails, when it has no entry for the binary, or on a mismatch.
  - `--insecure-skip-checksum` bypasses this with a prominent stderr warning.
  - The checksum is verified **before** the downloaded binary is executed for `--describe`.
  - `abby publish` has emitted `<agent>-sha256sums.txt` since v0.7.0, so existing published agents keep working.
- **No environment variable may redirect the GitHub API base URL.** The client sends `GITHUB_TOKEN` / `gh auth token` to it. Tests inject the client through a package variable instead.
- **Test isolation:** integration tests set `ABBY_CONFIG_METHOD=file`. Fake-CLI tests set `PATH` to a fake-bin dir only and use `#!/bin/sh` scripts, so a real `claude`, `codex` or `gemini` on the machine is never invoked.
- Tool/agent runtime logging still goes to stderr. `abby` user-facing output goes to stdout, and warnings to stderr.
- TDD per task. Keep ≥ 80% coverage on `pkg/runtimecfg` and `pkg/fsutil`, and don't lower any other package's coverage.
- Commit format: `<type>: <description>` plus the trailer `Co-Authored-By: Claude <noreply@anthropic.com>`. Never use `--no-verify`. Never rewrite existing commits.
- **Hook trap:** a local hook blocks any Bash command that combines `git commit` with an `-n` flag (for example `grep -n`). Run `git add` and `git commit` as separate commands.
- LSP may show stale go-sdk v1.4.0 errors. Ignore them. `go build ./...` / `go test ./...` are authoritative.
- Out of scope:
  - Signing (cosign/sigstore).
  - Windows config paths beyond what `os.UserHomeDir` gives.
  - HTTP/SSE server entries.
  - Codex project-trust management: abby only reports it.

## Decisions taken in this plan

- **Method selection is per change.** A change uses the runtime's CLI (`MethodCLI`) only if all of these hold:
  - the CLI is on PATH;
  - the user did not pass `--config-method file`;
  - the CLI can express every field abby sets **and** won't drop keys the user owns.

  Otherwise the change uses `MethodFile`. Concretely:
  - **Claude:** CLI for both scopes. `add-json` drops unknown keys, so if the existing entry has keys abby doesn't own, use the file method.
  - **Gemini:** CLI only for user scope, with no `Cwd`, no existing unowned keys, and no argument starting with `-`.
  - **Codex:** always the file method.

  `--config-method cli` on a change that can't use the CLI is an error that names the reason.
- **Claude's remove → add is made recoverable.** If `add-json` fails after a successful remove, abby re-adds the previous entry with `add-json`. If that also fails, the error prints the old entry's JSON so the user can restore it.
- **Previews show the entry, not the whole file.** Some config files are huge (`~/.claude.json`). The preview is a line diff of the server entry's JSON or TOML, before and after, plus the exact CLI command(s) for `MethodCLI`.
- **JSON editing preserves order.** The top-level object, the `mcpServers` object and the entry object are parsed as ordered key → `json.RawMessage` lists. Untouched values are byte-identical, apart from whitespace re-indentation, and numbers are never round-tripped through `float64`.
- **TOML editing is surgical.**
  - Only the lines of `[mcp_servers.<name>]` and its `[mcp_servers.<name>.*]` subtables are replaced. That block is decoded, merged with the owned keys, and re-rendered. Comments inside the block are lost; comments elsewhere survive.
  - If `mcp_servers.<name>` is defined in inline form (for example `[mcp_servers]` followed by `x = { … }`), abby refuses and names the file.
  - The result is re-parsed to validate it before it's written.
- **`--env` never erases.**
  - Without `--env`, an existing entry's `env` is kept, so `abby update` or a reinstall preserves it.
  - With `--env`, the given keys replace the entry's `env` wholesale.
  - At project scope, abby warns on stderr that the values are written to a file that is often committed, and suggests `${VAR}` references, which Claude and Gemini expand.
- **Backups are listed.** The summary table has a `BACKUP` column. A project-root backup such as `.mcp.json.abbyfile.bak` is visible to the user, and the docs suggest adding `*.abbyfile.bak` to `.gitignore`.
- **`ABBY_CONFIG_METHOD`** (`auto|cli|file`) sets the default for `--config-method`. It exists for tests and CI, and is documented.
- **The GitHub client is injectable.** `cmd/abby` has `var newGitHubClient = github.NewClient`, and tests replace it with a client pointing at `httptest`. No environment variable does this.
- **Dry-run touches nothing.** `--dry-run` copies no binary, writes no config and changes no registry entry. A remote install still downloads and verifies into a temp file, so the preview is honest about the checksum.
- **Doctor checks each Codex entry** and adds the note "Codex loads project `.codex/config.toml` only in trusted projects".

## Review Focus

1. **Reinstall or update of an agent whose runtime entry has user-added keys**: Codex `enabled`/`tools.*`, Gemini `trust`, or a hand-set `env`. The user expects those keys to survive. → Task 2, Step 1 (`TestUpsertJSONServer_PreservesUnownedKeys`); Task 3, Step 1 (`TestUpsertTOMLServer_PreservesUnownedKeysAndComments`); Task 6, Step 1 (`TestInstallKeepsExistingEnv`).
2. **A config file that is a symlink**, as dotfile managers create. The user expects the link to stay a link and its target to be edited. → Task 1, Step 1 (`TestSnapshotCommitThroughSymlink`).
3. **Gemini `settings.json` with comments, or any unparsable config.** The user expects a clear refusal naming the file, the file untouched, and no backup created. → Task 4, Step 1 (`TestFileWriterRefusesUnparsable`).
4. **`claude mcp add-json` fails after `remove` succeeded.** The user expects the previous entry to be restored. → Task 5, Step 1 (`TestClaudeCLIRestoresOnAddFailure`).
5. **Installing an agent released before this phase, or one with a tampered binary.** A release with the standard `<agent>-sha256sums.txt` installs. A mismatch is refused **before** the binary runs. → Task 7, Step 1 (`TestRemoteInstallVerifiesBeforeExecuting`, `TestVerifyReleaseAsset`).

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `pkg/fsutil/snapshot.go`, `snapshot_test.go` | Read-then-commit with symlink resolution, mode, backup and a concurrency check | Create (Task 1) |
| `pkg/runtimecfg/jsonobj.go`, `jsonobj_test.go` | Order-preserving JSON object and `mcpServers` entry editing | Create (Task 2) |
| `pkg/runtimecfg/tomlblock.go`, `tomlblock_test.go` | Surgical `[mcp_servers.<name>]` block editing | Create (Task 3) |
| `pkg/runtimecfg/runtime.go` | `Runtime`, `Scope`, `Method`, `ServerEntry`, `Change`, `ConfigWriter`, `Options`, `For`/`Detect`/`Resolve` | Rewrite (Task 4) |
| `pkg/runtimecfg/paths.go` | Per-runtime config paths and detection dirs, including env overrides and the legacy path | Create (Task 4) |
| `pkg/runtimecfg/render.go` | Per-runtime owned-key rendering of a `ServerEntry` | Create (Task 4) |
| `pkg/runtimecfg/filewriter.go` | File-method plan and apply for JSON and TOML runtimes | Create (Task 4) |
| `pkg/runtimecfg/diff.go` | Small line diff for previews | Create (Task 4) |
| `pkg/runtimecfg/claude.go`, `codex.go`, `gemini.go` | Replaced by the files above | Delete (Task 4) |
| `pkg/runtimecfg/runtime_test.go`, `filewriter_test.go`, `testdata/*` | Golden round trips and detection | Rewrite / Create (Task 4) |
| `pkg/runtimecfg/cliwriter.go`, `cliwriter_test.go` | CLI-method plan and apply (Claude and Gemini), with Claude restore | Create (Task 5) |
| `cmd/abby/options.go` | `installOptions`, `--env` parsing, scope mapping, `ABBY_CONFIG_METHOD` | Create (Task 6) |
| `cmd/abby/configchanges.go`, `configchanges_test.go` | Plan, preview and apply changes; summary table | Create (Task 6) |
| `cmd/abby/describe.go` | Richer manifest; `runtimeTimeout` | Modify (Task 6) |
| `cmd/abby/install.go`, `build.go`, `uninstall.go`, `update.go` | Use options, plans and the summary; new flags | Modify (Task 4 minimally, Task 6 fully) |
| `cmd/abby/checksum.go`, `checksum_test.go`, `install_remote_test.go` | Mandatory checksum, verify before execute | Create (Task 7) |
| `cmd/abby/doctor.go`, `doctor_test.go`, `main.go` | `abby doctor` | Create / Modify (Task 8) |
| `internal/integration/distribution_test.go`, `install_config_test.go`, `doctor_test.go` | Integration coverage | Modify / Create (Tasks 6, 8, 9) |
| `docs/guides/distribution.md`, `docs/reference.md`, `README.md` | Docs and v0.12 migration notes | Modify (Task 9) |

---

### Task 1: `fsutil.Snapshot` — safe read-then-commit

**Files:**
- Create: `pkg/fsutil/snapshot.go`, `pkg/fsutil/snapshot_test.go`

**Interfaces:**
- Consumes: the existing `fsutil.WriteAtomic` (see the note in Step 3).
- Produces:
  - `var ErrChangedOnDisk = errors.New(...)`
  - `type Snapshot struct { Path, Target string; Exists bool; Data []byte; Mode fs.FileMode }` (it also has unexported fields)
  - `func ReadSnapshot(path string) (*Snapshot, error)`
  - `func (s *Snapshot) Commit(data []byte) (backupPath string, err error)`
  - `const BackupSuffix = ".abbyfile.bak"`

- [ ] **Step 1: Write the failing tests**

`pkg/fsutil/snapshot_test.go`:

```go
package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotNewFileGets0600(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "cfg.json")
	s, err := ReadSnapshot(p)
	if err != nil || s.Exists {
		t.Fatalf("ReadSnapshot = %+v, %v", s, err)
	}
	backup, err := s.Commit([]byte("{}\n"))
	if err != nil || backup != "" {
		t.Fatalf("Commit = %q, %v (no backup expected for a new file)", backup, err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestSnapshotPreservesModeAndBacksUpOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	os.WriteFile(p, []byte("v1"), 0o640)
	s, _ := ReadSnapshot(p)
	backup, err := s.Commit([]byte("v2"))
	if err != nil || backup != p+BackupSuffix {
		t.Fatalf("Commit = %q, %v", backup, err)
	}
	if b, _ := os.ReadFile(backup); string(b) != "v1" {
		t.Errorf("backup = %q, want v1", b)
	}
	if fi, _ := os.Stat(backup); fi.Mode().Perm() != 0o640 {
		t.Errorf("backup mode = %v, want source mode 0640", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
		t.Errorf("file mode = %v, want preserved 0640", fi.Mode().Perm())
	}
	s2, _ := ReadSnapshot(p)
	if b2, err := s2.Commit([]byte("v3")); err != nil || b2 != "" {
		t.Fatalf("second Commit = %q, %v (backup must not be overwritten or reported)", b2, err)
	}
	if b, _ := os.ReadFile(backup); string(b) != "v1" {
		t.Errorf("backup overwritten: %q", b)
	}
}

// Review Focus #2.
func TestSnapshotCommitThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "claude.json")
	os.MkdirAll(filepath.Dir(real), 0o755)
	os.WriteFile(real, []byte("old"), 0o600)
	link := filepath.Join(dir, ".claude.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	s, err := ReadSnapshot(link)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced by a regular file")
	}
	if b, _ := os.ReadFile(real); string(b) != "new" {
		t.Errorf("target = %q, want new", b)
	}
	if _, err := os.Stat(real + BackupSuffix); err != nil {
		t.Errorf("backup should sit next to the target: %v", err)
	}
}

func TestSnapshotDanglingSymlinkRefused(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "cfg.json")
	os.Symlink(filepath.Join(dir, "missing", "x.json"), link)
	if _, err := ReadSnapshot(link); err == nil {
		t.Fatal("dangling symlink must be refused")
	}
}

func TestSnapshotDetectsConcurrentWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	os.WriteFile(p, []byte("v1"), 0o600)
	s, _ := ReadSnapshot(p)
	os.WriteFile(p, []byte("someone else"), 0o600)
	if _, err := s.Commit([]byte("v2")); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("err = %v, want ErrChangedOnDisk", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "someone else" {
		t.Errorf("file clobbered: %q", b)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/fsutil/ -run Snapshot -v`
Expected: FAIL with `undefined: ReadSnapshot`.

- [ ] **Step 3: Implement**

`pkg/fsutil/snapshot.go`:

```go
package fsutil

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// BackupSuffix is appended to a config file's path for abby's one-time backup.
const BackupSuffix = ".abbyfile.bak"

// ErrChangedOnDisk means the file changed between ReadSnapshot and Commit
// (for example a running runtime rewrote it). Callers re-plan once, then give up.
var ErrChangedOnDisk = errors.New("file changed on disk since it was read")

// Snapshot is a config file's content at read time, used to write it back
// safely: symlinks are followed to their target, the mode is preserved
// (0600 for new files), a one-time backup is made, and a concurrent change
// is detected before the atomic rename.
type Snapshot struct {
	Path   string      // path as given
	Target string      // symlink-resolved path that Commit writes
	Exists bool
	Data   []byte      // content at read time (nil when !Exists)
	Mode   fs.FileMode // permission bits to write with
}

// ReadSnapshot reads path (following a symlink to its target). A missing
// file is not an error; a dangling symlink is.
func ReadSnapshot(path string) (*Snapshot, error) {
	target := path
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, fmt.Errorf("%s is a symlink whose target cannot be resolved: %w", path, err)
		}
		target = resolved
	}
	s := &Snapshot{Path: path, Target: target, Mode: 0o600}
	data, err := os.ReadFile(target)
	switch {
	case err == nil:
		fi, statErr := os.Stat(target)
		if statErr != nil {
			return nil, statErr
		}
		s.Exists, s.Data, s.Mode = true, data, fi.Mode().Perm()
	case errors.Is(err, fs.ErrNotExist):
	default:
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return s, nil
}

// Commit atomically replaces the target with data. If the file existed and
// no backup exists yet, it first copies the original to Target+BackupSuffix
// (same mode) and returns that path; otherwise backupPath is "". Returns
// ErrChangedOnDisk, writing nothing, if the file no longer matches Data.
func (s *Snapshot) Commit(data []byte) (backupPath string, err error) {
	current, readErr := os.ReadFile(s.Target)
	switch {
	case readErr == nil && (!s.Exists || !bytes.Equal(current, s.Data)):
		return "", fmt.Errorf("%s: %w", s.Path, ErrChangedOnDisk)
	case readErr != nil && !errors.Is(readErr, fs.ErrNotExist):
		return "", fmt.Errorf("re-reading %s: %w", s.Path, readErr)
	case readErr != nil && s.Exists:
		return "", fmt.Errorf("%s: %w (it was deleted)", s.Path, ErrChangedOnDisk)
	}
	if err := os.MkdirAll(filepath.Dir(s.Target), 0o755); err != nil {
		return "", fmt.Errorf("creating directory for %s: %w", s.Path, err)
	}
	if s.Exists {
		bp := s.Target + BackupSuffix
		if _, statErr := os.Stat(bp); errors.Is(statErr, fs.ErrNotExist) {
			if err := WriteAtomic(bp, s.Data, s.Mode); err != nil {
				return "", fmt.Errorf("writing backup %s: %w", bp, err)
			}
			backupPath = bp
		}
	}
	if err := WriteAtomic(s.Target, data, s.Mode); err != nil {
		return backupPath, fmt.Errorf("writing %s: %w", s.Path, err)
	}
	return backupPath, nil
}
```

`WriteAtomic` creates its temp file in `filepath.Dir(path)`, so passing the resolved target keeps the rename on the same filesystem as the target, not the link.

- [ ] **Step 4: Run the tests**

Run: `go test ./pkg/fsutil/ -cover -v`
Expected: PASS, coverage ≥ 80%.

- [ ] **Step 5: Commit**

```bash
git add pkg/fsutil/snapshot.go pkg/fsutil/snapshot_test.go
```
```bash
git commit -m "feat: fsutil.Snapshot — symlink-aware, mode-preserving, backed-up atomic config writes

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: Order-preserving JSON editing of `mcpServers` entries

**Files:**
- Create: `pkg/runtimecfg/jsonobj.go`, `pkg/runtimecfg/jsonobj_test.go`

**Interfaces:**
- Produces (package-internal, used by Task 4):
  - `type jsonField struct { Key string; Value json.RawMessage }`
  - `type jsonObject []jsonField`
  - `func parseJSONObject(data []byte) (jsonObject, error)`: empty or whitespace-only input gives an empty object; a non-object gives an error.
  - `func (o jsonObject) get(key string) (json.RawMessage, bool)`
  - `func (o jsonObject) with(key string, v json.RawMessage) jsonObject`: returns a copy with the key replaced in place, or appended.
  - `func (o jsonObject) without(key string) jsonObject`
  - `func (o jsonObject) marshal() ([]byte, error)`: 2-space indent plus a trailing newline.
  - `func upsertJSONServer(data []byte, name string, set jsonObject) (out []byte, before, after jsonObject, err error)`
  - `func removeJSONServer(data []byte, name string) (out []byte, before jsonObject, found bool, err error)`
  - `func lookupJSONServer(data []byte, name string) (jsonObject, bool, error)`

- [ ] **Step 1: Write the failing tests**

`pkg/runtimecfg/jsonobj_test.go`:

```go
package runtimecfg

import (
	"encoding/json"
	"strings"
	"testing"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestParseJSONObjectPreservesOrderAndNumbers(t *testing.T) {
	in := `{"z":1,"big":12345678901234567890,"a":{"k":[1,2]}}`
	o, err := parseJSONObject([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if o[0].Key != "z" || o[1].Key != "big" || o[2].Key != "a" {
		t.Fatalf("order lost: %+v", o)
	}
	out, _ := o.marshal()
	if !strings.Contains(string(out), "12345678901234567890") {
		t.Errorf("big number mangled: %s", out)
	}
	if strings.Index(string(out), `"z"`) > strings.Index(string(out), `"a"`) {
		t.Errorf("order not preserved on marshal: %s", out)
	}
}

func TestParseJSONObjectRejectsNonObjectAndComments(t *testing.T) {
	for _, in := range []string{`[1]`, `{"a":1 // c` + "\n}", `{"a":1,}`, `nope`} {
		if _, err := parseJSONObject([]byte(in)); err == nil {
			t.Errorf("parseJSONObject(%q) must fail", in)
		}
	}
	if o, err := parseJSONObject([]byte("  \n")); err != nil || len(o) != 0 {
		t.Errorf("empty input = %v, %v; want empty object", o, err)
	}
}

// Review Focus #1.
func TestUpsertJSONServer_PreservesUnownedKeys(t *testing.T) {
	in := `{"theme":"dark","mcpServers":{"other":{"command":"x"},"a":{"command":"/old","trust":true,"env":{"K":"v"},"includeTools":["t"]}},"tail":{"n":1}}`
	set := jsonObject{{"command", raw(`"/new"`)}, {"args", raw(`["serve-mcp"]`)}}
	out, before, after, err := upsertJSONServer([]byte(in), "a", set)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`"theme": "dark"`, `"other"`, `"trust": true`, `"K": "v"`, `"includeTools"`, `"/new"`, `"tail"`} {
		if !strings.Contains(s, want) {
			t.Errorf("output lost %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"/old"`) {
		t.Errorf("command not replaced:\n%s", s)
	}
	if _, ok := before.get("trust"); !ok {
		t.Error("before must report the previous entry")
	}
	if v, _ := after.get("command"); string(v) != `"/new"` {
		t.Errorf("after.command = %s", v)
	}
}

func TestUpsertJSONServer_CreatesContainer(t *testing.T) {
	out, before, _, err := upsertJSONServer(nil, "a", jsonObject{{"command", raw(`"/bin/x"`)}})
	if err != nil || before != nil {
		t.Fatalf("err=%v before=%v", err, before)
	}
	if !strings.Contains(string(out), `"mcpServers"`) || !strings.HasSuffix(string(out), "}\n") {
		t.Errorf("out = %s", out)
	}
}

func TestUpsertJSONServer_RejectsNonObjectContainer(t *testing.T) {
	if _, _, _, err := upsertJSONServer([]byte(`{"mcpServers":[]}`), "a", jsonObject{}); err == nil {
		t.Fatal("mcpServers that is not an object must be refused")
	}
}

func TestRemoveAndLookupJSONServer(t *testing.T) {
	in := `{"mcpServers":{"a":{"command":"x"},"b":{"command":"y"}},"k":1}`
	e, ok, err := lookupJSONServer([]byte(in), "a")
	if err != nil || !ok {
		t.Fatalf("lookup = %v %v", ok, err)
	}
	if v, _ := e.get("command"); string(v) != `"x"` {
		t.Errorf("lookup command = %s", v)
	}
	out, before, found, err := removeJSONServer([]byte(in), "a")
	if err != nil || !found || before == nil {
		t.Fatalf("remove = %v %v", found, err)
	}
	if strings.Contains(string(out), `"a"`) || !strings.Contains(string(out), `"b"`) || !strings.Contains(string(out), `"k": 1`) {
		t.Errorf("remove output = %s", out)
	}
	if _, _, found, _ := removeJSONServer([]byte(in), "missing"); found {
		t.Error("missing name must report found=false")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/runtimecfg/ -run 'JSON' -v`
Expected: FAIL with `undefined: parseJSONObject`.

- [ ] **Step 3: Implement**

`pkg/runtimecfg/jsonobj.go`:

```go
package runtimecfg

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// mcpServersKey is the container key Claude Code and Gemini CLI use.
const mcpServersKey = "mcpServers"

type jsonField struct {
	Key   string
	Value json.RawMessage
}

// jsonObject is a JSON object as an ordered list of raw values, so editing
// one key leaves every other key's order and bytes untouched (numbers are
// never round-tripped through float64).
type jsonObject []jsonField

// parseJSONObject parses a JSON object strictly (no comments, no trailing
// commas). Empty input is an empty object.
func parseJSONObject(data []byte) (jsonObject, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return jsonObject{}, nil
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("not valid JSON (comments and trailing commas are not supported)")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("top level is not a JSON object")
	}
	var o jsonObject
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := kt.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		o = append(o, jsonField{Key: key, Value: v})
	}
	return o, nil
}

func (o jsonObject) get(key string) (json.RawMessage, bool) {
	for _, f := range o {
		if f.Key == key {
			return f.Value, true
		}
	}
	return nil, false
}

// with returns a copy of o with key set to v (replaced in place, else appended).
func (o jsonObject) with(key string, v json.RawMessage) jsonObject {
	out := make(jsonObject, 0, len(o)+1)
	replaced := false
	for _, f := range o {
		if f.Key == key {
			out = append(out, jsonField{Key: key, Value: v})
			replaced = true
			continue
		}
		out = append(out, f)
	}
	if !replaced {
		out = append(out, jsonField{Key: key, Value: v})
	}
	return out
}

// without returns a copy of o without key.
func (o jsonObject) without(key string) jsonObject {
	out := make(jsonObject, 0, len(o))
	for _, f := range o {
		if f.Key != key {
			out = append(out, f)
		}
	}
	return out
}

func (o jsonObject) compact() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(f.Key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(f.Value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshal renders o with 2-space indentation and a trailing newline.
func (o jsonObject) marshal() ([]byte, error) {
	c, err := o.compact()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, c, "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// servers returns the mcpServers object of top (empty if absent).
func servers(top jsonObject) (jsonObject, error) {
	v, ok := top.get(mcpServersKey)
	if !ok {
		return jsonObject{}, nil
	}
	s, err := parseJSONObject(v)
	if err != nil {
		return nil, fmt.Errorf("%q is not a JSON object: %w", mcpServersKey, err)
	}
	return s, nil
}

// upsertJSONServer sets the fields in set on mcpServers[name], keeping all
// other keys of that entry, of mcpServers and of the document. before is
// the previous entry (nil when new); after is the new entry.
func upsertJSONServer(data []byte, name string, set jsonObject) (out []byte, before, after jsonObject, err error) {
	top, err := parseJSONObject(data)
	if err != nil {
		return nil, nil, nil, err
	}
	srv, err := servers(top)
	if err != nil {
		return nil, nil, nil, err
	}
	entry := jsonObject{}
	if v, ok := srv.get(name); ok {
		if entry, err = parseJSONObject(v); err != nil {
			return nil, nil, nil, fmt.Errorf("%s.%s is not a JSON object: %w", mcpServersKey, name, err)
		}
		before = entry
	}
	after = entry
	for _, f := range set {
		after = after.with(f.Key, f.Value)
	}
	ec, _ := after.compact()
	srv = srv.with(name, ec)
	sc, _ := srv.compact()
	top = top.with(mcpServersKey, sc)
	out, err = top.marshal()
	return out, before, after, err
}

// removeJSONServer deletes mcpServers[name]; found=false leaves data unchanged.
func removeJSONServer(data []byte, name string) (out []byte, before jsonObject, found bool, err error) {
	top, err := parseJSONObject(data)
	if err != nil {
		return nil, nil, false, err
	}
	srv, err := servers(top)
	if err != nil {
		return nil, nil, false, err
	}
	v, ok := srv.get(name)
	if !ok {
		return data, nil, false, nil
	}
	before, _ = parseJSONObject(v)
	sc, _ := srv.without(name).compact()
	out, err = top.with(mcpServersKey, sc).marshal()
	return out, before, true, err
}

// lookupJSONServer returns mcpServers[name].
func lookupJSONServer(data []byte, name string) (jsonObject, bool, error) {
	top, err := parseJSONObject(data)
	if err != nil {
		return nil, false, err
	}
	srv, err := servers(top)
	if err != nil {
		return nil, false, err
	}
	v, ok := srv.get(name)
	if !ok {
		return nil, false, nil
	}
	e, err := parseJSONObject(v)
	return e, err == nil, err
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./pkg/runtimecfg/ -run 'JSON' -v`
Expected: PASS. The old `claude.go`/`gemini.go` still compile alongside it; Task 4 replaces them.

- [ ] **Step 5: Commit**

```bash
git add pkg/runtimecfg/jsonobj.go pkg/runtimecfg/jsonobj_test.go
```
```bash
git commit -m "feat: order-preserving JSON editing of mcpServers entries

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: Surgical TOML block editing for Codex

**Files:**
- Create: `pkg/runtimecfg/tomlblock.go`, `pkg/runtimecfg/tomlblock_test.go`

**Interfaces:**
- Produces (package-internal, used by Task 4):
  - `func upsertTOMLServer(data []byte, name string, set map[string]any) (out []byte, before, after map[string]any, err error)`
  - `func removeTOMLServer(data []byte, name string) (out []byte, before map[string]any, found bool, err error)`
  - `func lookupTOMLServer(data []byte, name string) (map[string]any, bool, error)`
  - `func renderTOMLServer(name string, entry map[string]any) (string, error)`, the block text, also used by previews.
  - `const codexServersKey = "mcp_servers"`

- [ ] **Step 1: Write the failing tests**

`pkg/runtimecfg/tomlblock_test.go`:

```go
package runtimecfg

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

const codexFixture = `# user settings
model = "o3"  # keep me

[mcp_servers.other]
command = "/bin/other"

[mcp_servers.agent]
command = "/old/agent"
args = ["serve-mcp"]
enabled = false

[mcp_servers.agent.env]
TOKEN = "t"

[mcp_servers.agent.tools.search]
approval_mode = "prompt"

[profiles.fast]
model = "o4-mini"
`

// Review Focus #1.
func TestUpsertTOMLServer_PreservesUnownedKeysAndComments(t *testing.T) {
	set := map[string]any{"command": "/new/agent", "args": []string{"serve-mcp"}, "cwd": "/proj", "tool_timeout_sec": int64(130)}
	out, before, after, err := upsertTOMLServer([]byte(codexFixture), "agent", set)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"# user settings", "# keep me", "[mcp_servers.other]", "[profiles.fast]", `model = "o4-mini"`} {
		if !strings.Contains(s, want) {
			t.Errorf("lost %q:\n%s", want, s)
		}
	}
	if before["command"] != "/old/agent" {
		t.Errorf("before = %v", before)
	}
	if after["enabled"] != false {
		t.Errorf("unowned key enabled lost: %v", after)
	}
	var doc map[string]any
	if _, err := toml.Decode(s, &doc); err != nil {
		t.Fatalf("output is not valid TOML: %v\n%s", err, s)
	}
	got, ok, _ := lookupTOMLServer(out, "agent")
	if !ok || got["command"] != "/new/agent" || got["cwd"] != "/proj" || got["enabled"] != false {
		t.Errorf("round trip = %v", got)
	}
	env, _ := got["env"].(map[string]any)
	tools, _ := got["tools"].(map[string]any)
	if env["TOKEN"] != "t" || tools["search"] == nil {
		t.Errorf("subtables lost: env=%v tools=%v", env, tools)
	}
	if strings.Count(s, "[mcp_servers.agent]") != 1 {
		t.Errorf("block duplicated:\n%s", s)
	}
}

func TestUpsertTOMLServer_AppendsWhenAbsentAndQuotesNames(t *testing.T) {
	out, before, _, err := upsertTOMLServer([]byte("model = \"o3\"\n"), "my.agent", map[string]any{"command": "/x"})
	if err != nil || before != nil {
		t.Fatalf("err=%v before=%v", err, before)
	}
	if !strings.Contains(string(out), `[mcp_servers."my.agent"]`) {
		t.Errorf("name not quoted:\n%s", out)
	}
	if got, ok, _ := lookupTOMLServer(out, "my.agent"); !ok || got["command"] != "/x" {
		t.Errorf("lookup = %v %v", got, ok)
	}
}

// Files written by abby <= v0.11 via the BurntSushi encoder (indented, one [mcp_servers] header).
func TestUpsertTOMLServer_LegacyEncoderOutput(t *testing.T) {
	legacy := "[mcp_servers]\n  [mcp_servers.agent]\n    command = \"/old\"\n    args = [\"serve-mcp\"]\n"
	out, _, _, err := upsertTOMLServer([]byte(legacy), "agent", map[string]any{"command": "/new"})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if _, err := toml.Decode(string(out), &doc); err != nil {
		t.Fatalf("invalid TOML: %v\n%s", err, out)
	}
	if got, _, _ := lookupTOMLServer(out, "agent"); got["command"] != "/new" {
		t.Errorf("got %v\n%s", got, out)
	}
}

func TestUpsertTOMLServer_RefusesInlineForm(t *testing.T) {
	in := "[mcp_servers]\nagent = { command = \"/x\" }\n"
	if _, _, _, err := upsertTOMLServer([]byte(in), "agent", map[string]any{"command": "/y"}); err == nil || !strings.Contains(err.Error(), "inline") {
		t.Fatalf("err = %v, want inline-form refusal", err)
	}
}

func TestUpsertTOMLServer_RejectsInvalidFile(t *testing.T) {
	if _, _, _, err := upsertTOMLServer([]byte("model = \n"), "a", map[string]any{"command": "/x"}); err == nil {
		t.Fatal("unparsable TOML must be refused")
	}
}

func TestRemoveTOMLServer(t *testing.T) {
	out, before, found, err := removeTOMLServer([]byte(codexFixture), "agent")
	if err != nil || !found || before["command"] != "/old/agent" {
		t.Fatalf("remove = %v %v %v", found, before, err)
	}
	s := string(out)
	if strings.Contains(s, "mcp_servers.agent") || !strings.Contains(s, "[mcp_servers.other]") || !strings.Contains(s, "# keep me") {
		t.Errorf("remove output:\n%s", s)
	}
	if _, _, found, _ := removeTOMLServer([]byte(codexFixture), "missing"); found {
		t.Error("missing must be found=false")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/runtimecfg/ -run 'TOML' -v`
Expected: FAIL with `undefined: upsertTOMLServer`.

- [ ] **Step 3: Implement**

`pkg/runtimecfg/tomlblock.go`:

```go
package runtimecfg

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

const codexServersKey = "mcp_servers"

var tomlHeader = regexp.MustCompile(`^\s*\[\[?\s*([^\[\]]+?)\s*\]\]?\s*(#.*)?$`)

// splitTOMLKey splits a dotted TOML key, honouring "basic" and 'literal' quotes.
func splitTOMLKey(s string) []string {
	var parts []string
	var cur strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			} else if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '.':
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		case c == ' ' || c == '\t':
		default:
			cur.WriteByte(c)
		}
	}
	return append(parts, strings.TrimSpace(cur.String()))
}

// serverBlocks returns [start,end) line ranges of the [mcp_servers.<name>]
// table and its subtables, plus the index of every header line.
func serverBlocks(lines []string, name string) [][2]int {
	var headers []int
	for i, l := range lines {
		if tomlHeader.MatchString(l) {
			headers = append(headers, i)
		}
	}
	var blocks [][2]int
	for hi, start := range headers {
		m := tomlHeader.FindStringSubmatch(lines[start])
		if strings.HasPrefix(strings.TrimSpace(lines[start]), "[[") {
			continue
		}
		path := splitTOMLKey(m[1])
		if len(path) < 2 || path[0] != codexServersKey || path[1] != name {
			continue
		}
		end := len(lines)
		if hi+1 < len(headers) {
			end = headers[hi+1]
		}
		blocks = append(blocks, [2]int{start, end})
	}
	return blocks
}

func decodeServer(text, name string) (map[string]any, bool, error) {
	var doc map[string]any
	if _, err := toml.Decode(text, &doc); err != nil {
		return nil, false, err
	}
	srv, _ := doc[codexServersKey].(map[string]any)
	e, ok := srv[name].(map[string]any)
	return e, ok, nil
}

// renderTOMLServer renders entry as a [mcp_servers.<name>] block (with any
// nested tables as dotted subtable headers), ending in a newline. name is the
// raw server name; the encoder quotes it when it isn't a bare key.
func renderTOMLServer(name string, entry map[string]any) (string, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(map[string]any{codexServersKey: map[string]any{name: entry}}); err != nil {
		return "", err
	}
	// Drop the encoder's bare "[mcp_servers]" header: defining that table
	// again would be invalid if the file already has one.
	var out []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.TrimSpace(l) == "["+codexServersKey+"]" {
			continue
		}
		out = append(out, l)
	}
	s := strings.TrimLeft(strings.Join(out, "\n"), "\n")
	return strings.TrimRight(s, "\n") + "\n", nil
}

// upsertTOMLServer merges set into [mcp_servers.<name>] and replaces only
// that block's lines (appending a new block when absent).
func upsertTOMLServer(data []byte, name string, set map[string]any) (out []byte, before, after map[string]any, err error) {
	text := string(data)
	existing, inDoc, err := decodeServer(text, name)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("not valid TOML: %w", err)
	}
	lines := strings.Split(text, "\n")
	blocks := serverBlocks(lines, name)
	if inDoc && len(blocks) == 0 {
		return nil, nil, nil, fmt.Errorf("mcp_servers.%s is defined inline; abby only edits [mcp_servers.%s] tables — edit it by hand or remove it and re-run", name, name)
	}
	after = map[string]any{}
	if inDoc {
		before = existing
		for k, v := range existing {
			after[k] = v
		}
	}
	for k, v := range set {
		after[k] = v
	}
	block, err := renderTOMLServer(name, after)
	if err != nil {
		return nil, nil, nil, err
	}
	var b strings.Builder
	if len(blocks) == 0 {
		b.WriteString(strings.TrimRight(text, "\n"))
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(block)
	} else {
		pos := 0
		for i, r := range blocks {
			b.WriteString(strings.Join(lines[pos:r[0]], "\n"))
			if r[0] > pos {
				b.WriteString("\n")
			}
			if i == 0 {
				b.WriteString(block)
				if r[1] < len(lines) {
					b.WriteString("\n")
				}
			}
			pos = r[1]
		}
		b.WriteString(strings.Join(lines[pos:], "\n"))
	}
	out = []byte(b.String())
	if _, ok, verr := decodeServer(string(out), name); verr != nil || !ok {
		return nil, nil, nil, fmt.Errorf("internal error: edited TOML does not round-trip (%v)", verr)
	}
	return out, before, after, nil
}

// removeTOMLServer deletes the [mcp_servers.<name>] block(s).
func removeTOMLServer(data []byte, name string) (out []byte, before map[string]any, found bool, err error) {
	text := string(data)
	existing, inDoc, err := decodeServer(text, name)
	if err != nil {
		return nil, nil, false, fmt.Errorf("not valid TOML: %w", err)
	}
	if !inDoc {
		return data, nil, false, nil
	}
	lines := strings.Split(text, "\n")
	blocks := serverBlocks(lines, name)
	if len(blocks) == 0 {
		return nil, nil, false, fmt.Errorf("mcp_servers.%s is defined inline; remove it by hand", name)
	}
	var kept []string
	pos := 0
	for _, r := range blocks {
		kept = append(kept, lines[pos:r[0]]...)
		pos = r[1]
	}
	kept = append(kept, lines[pos:]...)
	out = []byte(strings.Join(kept, "\n"))
	if _, still, verr := decodeServer(string(out), name); verr != nil || still {
		return nil, nil, false, fmt.Errorf("internal error: removal did not round-trip (%v)", verr)
	}
	return out, existing, true, nil
}

// lookupTOMLServer returns [mcp_servers.<name>].
func lookupTOMLServer(data []byte, name string) (map[string]any, bool, error) {
	e, ok, err := decodeServer(string(data), name)
	if err != nil {
		return nil, false, fmt.Errorf("not valid TOML: %w", err)
	}
	return e, ok, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./pkg/runtimecfg/ -run 'TOML' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/runtimecfg/tomlblock.go pkg/runtimecfg/tomlblock_test.go
```
```bash
git commit -m "feat: surgical TOML editing of Codex [mcp_servers.<name>] blocks

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 4: `runtimecfg` plan/apply model with the file method (D7, D8, C2, C3, C4)

**Files:**
- Rewrite: `pkg/runtimecfg/runtime.go`
- Create: `pkg/runtimecfg/paths.go`, `pkg/runtimecfg/render.go`, `pkg/runtimecfg/filewriter.go`, `pkg/runtimecfg/diff.go`
- Delete: `pkg/runtimecfg/claude.go`, `pkg/runtimecfg/codex.go`, `pkg/runtimecfg/gemini.go`
- Rewrite: `pkg/runtimecfg/runtime_test.go`
- Create: `pkg/runtimecfg/filewriter_test.go`, and fixtures `pkg/runtimecfg/testdata/claude.json`, `pkg/runtimecfg/testdata/codex.toml`, `pkg/runtimecfg/testdata/gemini.json`
- Modify (minimal, to compile): `cmd/abby/install.go` (`mergeRuntimeConfigs`), `cmd/abby/build.go` (the runtime loop), `cmd/abby/uninstall.go` (the unwire loop), `cmd/abby/update.go` (`runtimecfg.Detect(...)`)
- Modify: `internal/integration/*.go` (`TestMain` env, `abby build` working directories) and `.gitignore` (Step 5)

**Interfaces:**
- Consumes: `fsutil.ReadSnapshot`, `(*Snapshot).Commit`, `fsutil.ErrChangedOnDisk` (Task 1); `upsertJSONServer`/`removeJSONServer`/`lookupJSONServer`/`jsonObject` (Task 2); `upsertTOMLServer`/`removeTOMLServer`/`lookupTOMLServer`/`renderTOMLServer` (Task 3).
- Produces:
  - Kept: `type Runtime string`, the constants `ClaudeCode`/`Codex`/`Gemini`, `AllRuntimes()`, `Parse(s string) (Runtime, error)`.
  - `type Scope string` with `ScopeProject = "project"`, `ScopeUser = "user"`.
  - `type Method string` with `MethodAuto = "auto"`, `MethodCLI = "cli"`, `MethodFile = "file"`; `func ParseMethod(s string) (Method, error)`.
  - `type ServerEntry struct { Command string; Args []string; Env map[string]string; Cwd string; Timeout time.Duration }`
  - `type Change struct { Runtime Runtime; Scope Scope; Method Method; Target, Server string; Remove, Noop bool; Preview string; Notes []string }` plus an unexported apply func.
  - `func (c Change) Apply() (backup string, err error)`
  - `type ConfigWriter interface { Runtime() Runtime; ConfigPath(Scope) (string, error); Lookup(Scope, string) (ServerEntry, bool, error); PlanAdd(Scope, string, ServerEntry) (Change, error); PlanRemove(Scope, string) (Change, error) }`
  - `type Options struct { Method Method; ProjectRoot string; LookPath func(string) (string, error); Run CommandRunner }`. `ProjectRoot` is the absolute root that project-scope paths are joined to; `""` means the process working directory at call time.
  - `type CommandRunner func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)`
  - `func For(r Runtime, opts Options) ConfigWriter`
  - `func Detect(opts Options) []ConfigWriter`
  - `func Resolve(flag string, opts Options) ([]ConfigWriter, error)`
  - `func LegacyClaudePath() (string, error)`
  - `const CodexStartupTimeoutSec = 30`
  - `func cliName(r Runtime) string`, internal; returns "claude"/"codex"/"gemini" and is used in Task 5.

- [ ] **Step 1: Write the failing tests**

Fixtures:
- `pkg/runtimecfg/testdata/claude.json`: a realistic `~/.claude.json` excerpt:

```json
{
  "numStartups": 412,
  "userID": "f00dfeedf00dfeedf00dfeedf00dfeedf00dfeedf00dfeedf00dfeedf00dfeed",
  "tipsHistory": {"new-user-warmup": 7},
  "projects": {"/Users/x/proj": {"allowedTools": [], "mcpServers": {"local-one": {"command": "/bin/l"}}}},
  "mcpServers": {"existing": {"type": "stdio", "command": "/opt/existing", "args": []}},
  "cachedStatsigGates": {"tengu_x": true},
  "lastReleaseNotesSeen": "2.9.1"
}
```

- `pkg/runtimecfg/testdata/codex.toml`: the `codexFixture` content from Task 3.
- `pkg/runtimecfg/testdata/gemini.json`:

```json
{
  "theme": "GitHub",
  "mcpServers": {"agent": {"command": "/old", "args": ["serve-mcp"], "trust": true, "includeTools": ["read_file"]}},
  "selectedAuthType": "oauth-personal"
}
```

`pkg/runtimecfg/filewriter_test.go`:

```go
package runtimecfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fileOpts forces the file method with no CLIs on PATH.
var fileOpts = Options{Method: MethodFile, LookPath: func(string) (string, error) { return "", os.ErrNotExist }}

func chdirTemp(t *testing.T) string {
	t.Helper()
	d, _ := filepath.EvalSymlinks(t.TempDir())
	old, _ := os.Getwd()
	if err := os.Chdir(d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	return d
}

func copyFixture(t *testing.T, name, dst string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(dst), 0o755)
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

var fixtureDir = func() string { d, _ := filepath.Abs("testdata"); return d }()

func entry() ServerEntry {
	return ServerEntry{Command: "/abs/bin/agent", Args: []string{"serve-mcp"}, Cwd: "/abs/proj", Timeout: 130 * time.Second}
}

func TestClaudeUserScopeFileRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	path := filepath.Join(home, ".claude.json")
	copyFixture(t, "claude.json", path)
	orig, _ := os.ReadFile(path)

	w := For(ClaudeCode, fileOpts)
	if p, _ := w.ConfigPath(ScopeUser); p != path {
		t.Fatalf("ConfigPath(user) = %q, want %q (D7)", p, path)
	}
	c, err := w.PlanAdd(ScopeUser, "agent", entry())
	if err != nil {
		t.Fatal(err)
	}
	if c.Method != MethodFile || c.Target != path || !strings.Contains(c.Preview, "+") {
		t.Fatalf("change = %+v", c)
	}
	if b, _ := os.ReadFile(path); string(b) != string(orig) {
		t.Fatal("PlanAdd must not write")
	}
	backup, err := c.Apply()
	if err != nil || backup != path+".abbyfile.bak" {
		t.Fatalf("Apply = %q, %v", backup, err)
	}
	var got map[string]json.RawMessage
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	var want map[string]json.RawMessage
	json.Unmarshal(orig, &want)
	for k := range want {
		if k == "mcpServers" {
			continue
		}
		if compactJSON(got[k]) != compactJSON(want[k]) {
			t.Errorf("key %q changed: %s -> %s", k, want[k], got[k])
		}
	}
	e, ok, _ := w.Lookup(ScopeUser, "agent")
	if !ok || e.Command != "/abs/bin/agent" || e.Timeout != 130*time.Second || e.Cwd != "" {
		t.Errorf("Lookup = %+v %v (Claude has no cwd; timeout in ms)", e, ok)
	}
	if _, ok, _ := w.Lookup(ScopeUser, "existing"); !ok {
		t.Error("existing server lost")
	}
}

func TestClaudeConfigDirOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if p, _ := For(ClaudeCode, fileOpts).ConfigPath(ScopeUser); p != filepath.Join(dir, ".claude.json") {
		t.Errorf("ConfigPath = %q", p)
	}
}

func TestProjectRootIsExplicit(t *testing.T) {
	chdirTemp(t) // process cwd is somewhere else
	root, _ := filepath.EvalSymlinks(t.TempDir())
	opts := fileOpts
	opts.ProjectRoot = root
	c, err := For(Codex, opts).PlanAdd(ScopeProject, "agent", entry())
	if err != nil || c.Target != filepath.Join(root, ".codex", "config.toml") {
		t.Fatalf("target = %q, %v; project scope must follow Options.ProjectRoot, not the process cwd", c.Target, err)
	}
}

func TestClaudeProjectScopeInCwd(t *testing.T) {
	d := chdirTemp(t)
	c, err := For(ClaudeCode, fileOpts).PlanAdd(ScopeProject, "agent", entry())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(d, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"type": "stdio"`, `"command": "/abs/bin/agent"`, `"timeout": 130000`} {
		if !strings.Contains(string(b), want) {
			t.Errorf(".mcp.json missing %s:\n%s", want, b)
		}
	}
	if fi, _ := os.Stat(filepath.Join(d, ".mcp.json")); fi.Mode().Perm() != 0o600 {
		t.Errorf("new file mode = %v", fi.Mode().Perm())
	}
}

func TestGeminiProjectKeepsTrustAndSetsCwd(t *testing.T) {
	d := chdirTemp(t)
	copyFixture(t, "gemini.json", filepath.Join(d, ".gemini", "settings.json"))
	w := For(Gemini, fileOpts)
	c, err := w.PlanAdd(ScopeProject, "agent", entry())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(d, ".gemini", "settings.json"))
	for _, want := range []string{`"trust": true`, `"includeTools"`, `"cwd": "/abs/proj"`, `"timeout": 130000`, `"selectedAuthType"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("settings.json missing %s:\n%s", want, b)
		}
	}
}

func TestCodexUserScopeHonoursCodexHome(t *testing.T) {
	ch := t.TempDir()
	t.Setenv("CODEX_HOME", ch)
	copyFixture(t, "codex.toml", filepath.Join(ch, "config.toml"))
	w := For(Codex, fileOpts)
	c, err := w.PlanAdd(ScopeUser, "agent", ServerEntry{Command: "/n", Args: []string{"serve-mcp"}, Timeout: 125500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if c.Target != filepath.Join(ch, "config.toml") {
		t.Errorf("target = %q", c.Target)
	}
	if _, err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(ch, "config.toml"))
	for _, want := range []string{"tool_timeout_sec = 126", "startup_timeout_sec = 30", "enabled = false", "# keep me"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("config.toml missing %q:\n%s", want, b)
		}
	}
}

func TestCodexProjectNotesTrust(t *testing.T) {
	chdirTemp(t)
	c, err := For(Codex, fileOpts).PlanAdd(ScopeProject, "agent", entry())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Notes) == 0 || !strings.Contains(c.Notes[0], "trusted") {
		t.Errorf("notes = %v", c.Notes)
	}
}

// Review Focus #3.
func TestFileWriterRefusesUnparsable(t *testing.T) {
	d := chdirTemp(t)
	p := filepath.Join(d, ".gemini", "settings.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	commented := "{\n  // my settings\n  \"theme\": \"x\"\n}\n"
	os.WriteFile(p, []byte(commented), 0o644)
	_, err := For(Gemini, fileOpts).PlanAdd(ScopeProject, "agent", entry())
	if err == nil || !strings.Contains(err.Error(), p) {
		t.Fatalf("err = %v, want refusal naming %s", err, p)
	}
	if b, _ := os.ReadFile(p); string(b) != commented {
		t.Error("file modified")
	}
	if _, err := os.Stat(p + ".abbyfile.bak"); !os.IsNotExist(err) {
		t.Error("no backup may be created on refusal")
	}
}

func TestPlanRemove(t *testing.T) {
	d := chdirTemp(t)
	w := For(ClaudeCode, fileOpts)
	c, _ := w.PlanAdd(ScopeProject, "agent", entry())
	c.Apply()
	rm, err := w.PlanRemove(ScopeProject, "agent")
	if err != nil || rm.Noop || !rm.Remove {
		t.Fatalf("PlanRemove = %+v, %v", rm, err)
	}
	if _, err := rm.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := w.Lookup(ScopeProject, "agent"); ok {
		t.Error("still present")
	}
	again, _ := w.PlanRemove(ScopeProject, "agent")
	if !again.Noop {
		t.Error("second remove must be a no-op")
	}
	_ = d
}

func TestApplyReplansOnceOnConcurrentWrite(t *testing.T) {
	d := chdirTemp(t)
	p := filepath.Join(d, ".mcp.json")
	os.WriteFile(p, []byte(`{"mcpServers":{}}`), 0o600)
	c, _ := For(ClaudeCode, fileOpts).PlanAdd(ScopeProject, "agent", entry())
	os.WriteFile(p, []byte(`{"mcpServers":{"someone":{"command":"x"}}}`), 0o600)
	if _, err := c.Apply(); err != nil {
		t.Fatalf("one concurrent change must be absorbed by re-planning: %v", err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "someone") || !strings.Contains(string(b), "agent") {
		t.Errorf("re-plan lost data: %s", b)
	}
}

func compactJSON(r json.RawMessage) string {
	var v any
	json.Unmarshal(r, &v)
	b, _ := json.Marshal(v)
	return string(b)
}
```

Rewrite `pkg/runtimecfg/runtime_test.go` to cover `Parse`, `ParseMethod`, `Resolve` (`all` gives 3, `codex` gives 1, `nope` errors) and detection:

```go
package runtimecfg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAndParseMethod(t *testing.T) {
	if r, err := Parse("codex"); err != nil || r != Codex {
		t.Errorf("Parse(codex) = %v %v", r, err)
	}
	if _, err := Parse("nope"); err == nil {
		t.Error("Parse(nope) must fail")
	}
	for _, m := range []string{"auto", "cli", "file"} {
		if _, err := ParseMethod(m); err != nil {
			t.Errorf("ParseMethod(%s): %v", m, err)
		}
	}
	if _, err := ParseMethod("magic"); err == nil {
		t.Error("ParseMethod(magic) must fail")
	}
}

func TestResolve(t *testing.T) {
	if ws, _ := Resolve("all", fileOpts); len(ws) != 3 {
		t.Errorf("all = %d", len(ws))
	}
	if ws, _ := Resolve("gemini", fileOpts); len(ws) != 1 || ws[0].Runtime() != Gemini {
		t.Error("gemini")
	}
	if _, err := Resolve("nope", fileOpts); err == nil {
		t.Error("nope must fail")
	}
}

// C3: detection is by CLI on PATH or runtime config dir, never by $HOME existing.
func TestDetect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	none := Options{Method: MethodFile, LookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	if ws := Detect(none); len(ws) != 1 || ws[0].Runtime() != ClaudeCode {
		t.Errorf("fallback = %v, want [claude-code]", ws)
	}
	os.MkdirAll(filepath.Join(home, ".gemini"), 0o755)
	if ws := Detect(none); len(ws) != 1 || ws[0].Runtime() != Gemini {
		t.Errorf("gemini dir → %v", ws)
	}
	onPath := Options{Method: MethodFile, LookPath: func(n string) (string, error) {
		if n == "codex" {
			return "/fake/codex", nil
		}
		return "", os.ErrNotExist
	}}
	got := map[Runtime]bool{}
	for _, w := range Detect(onPath) {
		got[w.Runtime()] = true
	}
	if !got[Codex] || !got[Gemini] || got[ClaudeCode] {
		t.Errorf("detect = %v, want codex (PATH) + gemini (dir) only", got)
	}
}

func TestLegacyClaudePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if p, _ := LegacyClaudePath(); p != filepath.Join(home, ".claude", "mcp.json") {
		t.Errorf("LegacyClaudePath = %q", p)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/runtimecfg/ -v`
Expected: FAIL to build. `For` has the wrong signature and `ScopeUser` is undefined.

- [ ] **Step 3: Implement the model, paths, rendering, diff and file writer**

Delete `claude.go`, `codex.go` and `gemini.go`, then write the files below.

`pkg/runtimecfg/runtime.go`:

```go
// Package runtimecfg registers MCP servers with AI coding runtimes (Claude
// Code, Codex, Gemini CLI). Every edit is planned first — a Change carries a
// preview — and applied through the runtime's own CLI when it can express the
// entry, otherwise through a surgical, backed-up edit of its config file.
package runtimecfg

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Runtime identifies an AI coding runtime that supports MCP servers.
type Runtime string

const (
	ClaudeCode Runtime = "claude-code"
	Codex      Runtime = "codex"
	Gemini     Runtime = "gemini"
)

// AllRuntimes returns all supported runtimes in deterministic order.
func AllRuntimes() []Runtime { return []Runtime{ClaudeCode, Codex, Gemini} }

// Parse converts a string to a Runtime.
func Parse(s string) (Runtime, error) {
	for _, r := range AllRuntimes() {
		if string(r) == s {
			return r, nil
		}
	}
	return "", fmt.Errorf("unknown runtime %q (supported: claude-code, codex, gemini)", s)
}

// Scope is where an entry is registered.
type Scope string

const (
	ScopeProject Scope = "project" // project root config (default install, abby build)
	ScopeUser    Scope = "user"    // user-global config (--global)
)

// Method is how a change is applied.
type Method string

const (
	MethodAuto Method = "auto" // CLI when it can express the entry, else file
	MethodCLI  Method = "cli"
	MethodFile Method = "file"
)

// ParseMethod parses --config-method / ABBY_CONFIG_METHOD values.
func ParseMethod(s string) (Method, error) {
	switch Method(s) {
	case MethodAuto, MethodCLI, MethodFile:
		return Method(s), nil
	}
	return "", fmt.Errorf("unknown config method %q (want auto, cli or file)", s)
}

// ServerEntry is the part of an MCP server entry abby owns. Zero values mean
// "leave the existing value alone": nil Env keeps an existing env, "" Cwd and
// 0 Timeout set nothing.
type ServerEntry struct {
	Command string
	Args    []string
	Env     map[string]string
	Cwd     string
	Timeout time.Duration
}

// Change is one planned edit to one runtime's config.
type Change struct {
	Runtime Runtime
	Scope   Scope
	Method  Method   // MethodCLI or MethodFile
	Target  string   // config file written (directly, or by the runtime CLI)
	Server  string   // MCP server name
	Remove  bool
	Noop    bool     // nothing to do (e.g. removing an absent entry)
	Preview string   // entry diff; for MethodCLI also the commands to run
	Notes   []string // user-facing caveats, e.g. Codex project trust
	apply   func() (string, error)
}

// Apply performs the change and returns the backup it created, if any.
func (c Change) Apply() (backup string, err error) {
	if c.Noop || c.apply == nil {
		return "", nil
	}
	return c.apply()
}

// ConfigWriter plans edits to one runtime's MCP config.
type ConfigWriter interface {
	Runtime() Runtime
	ConfigPath(scope Scope) (string, error)
	Lookup(scope Scope, name string) (ServerEntry, bool, error)
	PlanAdd(scope Scope, name string, e ServerEntry) (Change, error)
	PlanRemove(scope Scope, name string) (Change, error)
}

// CommandRunner runs a runtime CLI (injectable for tests).
type CommandRunner func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)

// Options configures writers. Zero values use real PATH lookup, a real
// process runner, MethodAuto and the current directory as project root.
type Options struct {
	Method      Method
	ProjectRoot string // absolute; project-scope configs live here ("" = os.Getwd())
	LookPath    func(string) (string, error)
	Run         CommandRunner
}

func (o Options) normalized() Options {
	if o.Method == "" {
		o.Method = MethodAuto
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.Run == nil {
		o.Run = runCommand
	}
	if o.ProjectRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			o.ProjectRoot = wd
		}
	}
	return o
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	return out.Bytes(), errb.Bytes(), err
}

// For returns the writer for r.
func For(r Runtime, opts Options) ConfigWriter {
	return newWriter(r, opts.normalized())
}

// Detect returns writers for runtimes whose CLI is on PATH or whose config
// directory exists; Claude Code when none is found.
func Detect(opts Options) []ConfigWriter {
	opts = opts.normalized()
	var ws []ConfigWriter
	for _, r := range AllRuntimes() {
		if detected(r, opts) {
			ws = append(ws, newWriter(r, opts))
		}
	}
	if len(ws) == 0 {
		ws = append(ws, newWriter(ClaudeCode, opts))
	}
	return ws
}

// Resolve maps a --runtime flag ("auto", "all" or a runtime name) to writers.
func Resolve(flag string, opts Options) ([]ConfigWriter, error) {
	switch flag {
	case "auto", "":
		return Detect(opts), nil
	case "all":
		var ws []ConfigWriter
		for _, r := range AllRuntimes() {
			ws = append(ws, For(r, opts))
		}
		return ws, nil
	}
	r, err := Parse(flag)
	if err != nil {
		return nil, err
	}
	return []ConfigWriter{For(r, opts)}, nil
}
```


`pkg/runtimecfg/paths.go`:

```go
package runtimecfg

import (
	"os"
	"path/filepath"
)

// cliName is the runtime's executable name.
func cliName(r Runtime) string {
	switch r {
	case ClaudeCode:
		return "claude"
	case Codex:
		return "codex"
	default:
		return "gemini"
	}
}

func claudeConfigDir() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

func codexHome() (string, error) {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// configPath returns the config file a runtime reads for scope; project
// scope is joined to root (an absolute project directory).
func configPath(r Runtime, scope Scope, root string) (string, error) {
	switch r {
	case ClaudeCode:
		if scope == ScopeProject {
			return filepath.Join(root, ".mcp.json"), nil
		}
		if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
			return filepath.Join(d, ".claude.json"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude.json"), nil
	case Codex:
		if scope == ScopeProject {
			return filepath.Join(root, ".codex", "config.toml"), nil
		}
		d, err := codexHome()
		if err != nil {
			return "", err
		}
		return filepath.Join(d, "config.toml"), nil
	default:
		if scope == ScopeProject {
			return filepath.Join(root, ".gemini", "settings.json"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".gemini", "settings.json"), nil
	}
}

// detectionDir is the runtime's own config directory (C3).
func detectionDir(r Runtime) (string, error) {
	switch r {
	case ClaudeCode:
		return claudeConfigDir()
	case Codex:
		return codexHome()
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".gemini"), nil
	}
}

func detected(r Runtime, opts Options) bool {
	if _, err := opts.LookPath(cliName(r)); err == nil {
		return true
	}
	d, err := detectionDir(r)
	if err != nil {
		return false
	}
	fi, err := os.Stat(d)
	return err == nil && fi.IsDir()
}

// LegacyClaudePath is ~/.claude/mcp.json, which abby <= v0.11 wrote for
// Claude Code's user scope; Claude Code never reads it.
func LegacyClaudePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "mcp.json"), nil
}
```

`pkg/runtimecfg/render.go`:

```go
package runtimecfg

import (
	"encoding/json"
	"math"
	"sort"
	"time"
)

// CodexStartupTimeoutSec is written as startup_timeout_sec for Codex entries.
const CodexStartupTimeoutSec = 30

// ownedJSON renders the keys abby owns for a JSON runtime entry.
func ownedJSON(r Runtime, e ServerEntry) jsonObject {
	m := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	var o jsonObject
	if r == ClaudeCode {
		o = append(o, jsonField{"type", m("stdio")})
	}
	args := e.Args
	if args == nil {
		args = []string{}
	}
	o = append(o, jsonField{"command", m(e.Command)}, jsonField{"args", m(args)})
	if len(e.Env) > 0 {
		o = append(o, jsonField{"env", m(e.Env)})
	}
	if r == Gemini && e.Cwd != "" {
		o = append(o, jsonField{"cwd", m(e.Cwd)})
	}
	if e.Timeout >= time.Second {
		o = append(o, jsonField{"timeout", m(e.Timeout.Milliseconds())})
	}
	return o
}

// ownedTOML renders the keys abby owns for a Codex entry.
func ownedTOML(e ServerEntry) map[string]any {
	args := e.Args
	if args == nil {
		args = []string{}
	}
	m := map[string]any{"command": e.Command, "args": args, "startup_timeout_sec": int64(CodexStartupTimeoutSec)}
	if len(e.Env) > 0 {
		env := map[string]any{}
		for k, v := range e.Env {
			env[k] = v
		}
		m["env"] = env
	}
	if e.Cwd != "" {
		m["cwd"] = e.Cwd
	}
	if e.Timeout > 0 {
		m["tool_timeout_sec"] = int64(math.Ceil(e.Timeout.Seconds()))
	}
	return m
}

// ownedKeys lists the keys ownedJSON/ownedTOML may set, per runtime; used
// to decide whether an existing entry has keys a CLI would drop.
func ownedKeys(r Runtime) map[string]bool {
	switch r {
	case ClaudeCode:
		return map[string]bool{"type": true, "command": true, "args": true, "env": true, "timeout": true}
	case Gemini:
		return map[string]bool{"command": true, "args": true, "env": true, "cwd": true, "timeout": true}
	default:
		return map[string]bool{"command": true, "args": true, "env": true, "cwd": true, "startup_timeout_sec": true, "tool_timeout_sec": true}
	}
}

// entryFromJSON reads a ServerEntry back from a JSON entry.
func entryFromJSON(o jsonObject) ServerEntry {
	var e ServerEntry
	if v, ok := o.get("command"); ok {
		json.Unmarshal(v, &e.Command)
	}
	if v, ok := o.get("args"); ok {
		json.Unmarshal(v, &e.Args)
	}
	if v, ok := o.get("env"); ok {
		json.Unmarshal(v, &e.Env)
	}
	if v, ok := o.get("cwd"); ok {
		json.Unmarshal(v, &e.Cwd)
	}
	if v, ok := o.get("timeout"); ok {
		var ms int64
		if json.Unmarshal(v, &ms) == nil {
			e.Timeout = time.Duration(ms) * time.Millisecond
		}
	}
	return e
}

// entryFromTOML reads a ServerEntry back from a Codex entry.
func entryFromTOML(m map[string]any) ServerEntry {
	var e ServerEntry
	e.Command, _ = m["command"].(string)
	if as, ok := m["args"].([]any); ok {
		for _, a := range as {
			if s, ok := a.(string); ok {
				e.Args = append(e.Args, s)
			}
		}
	}
	if env, ok := m["env"].(map[string]any); ok {
		e.Env = map[string]string{}
		for k, v := range env {
			if s, ok := v.(string); ok {
				e.Env[k] = s
			}
		}
	}
	e.Cwd, _ = m["cwd"].(string)
	if s, ok := m["tool_timeout_sec"].(int64); ok {
		e.Timeout = time.Duration(s) * time.Second
	}
	return e
}

func sortedKeys(m map[string]bool) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
```

`pkg/runtimecfg/diff.go`:

```go
package runtimecfg

import "strings"

// lineDiff renders a minimal line diff ("- ", "+ ", "  " prefixes) of two
// short texts via LCS. Inputs are entry-sized, so O(n*m) is fine.
func lineDiff(before, after string) string {
	a := splitLines(before)
	b := splitLines(after)
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out strings.Builder
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			out.WriteString("  " + a[i] + "\n")
			i, j = i+1, j+1
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			out.WriteString("+ " + b[j] + "\n")
			j++
		default:
			out.WriteString("- " + a[i] + "\n")
			i++
		}
	}
	return out.String()
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
```

`pkg/runtimecfg/filewriter.go`:

```go
package runtimecfg

import (
	"errors"
	"fmt"

	"github.com/teabranch/abbyfile/pkg/fsutil"
)

// writer implements ConfigWriter for one runtime. Task 5 adds CLI methods;
// here every change uses the file method.
type writer struct {
	r    Runtime
	opts Options
}

func newWriter(r Runtime, opts Options) ConfigWriter { return &writer{r: r, opts: opts} }

func (w *writer) Runtime() Runtime                       { return w.r }
func (w *writer) ConfigPath(s Scope) (string, error)      { return configPath(w.r, s, w.opts.ProjectRoot) }
func (w *writer) isTOML() bool                           { return w.r == Codex }

// notes are caveats shown for file-method changes.
func (w *writer) notes(scope Scope) []string {
	switch {
	case w.r == Codex && scope == ScopeProject:
		return []string{"Codex loads project .codex/config.toml only in trusted projects"}
	case w.r == ClaudeCode && scope == ScopeUser:
		return []string{"a running Claude Code rewrites ~/.claude.json and can overwrite this entry; install with `claude` on PATH or quit Claude Code first"}
	}
	return nil
}

// Lookup reads the entry from the config file (whatever method wrote it).
func (w *writer) Lookup(scope Scope, name string) (ServerEntry, bool, error) {
	path, err := w.ConfigPath(scope)
	if err != nil {
		return ServerEntry{}, false, err
	}
	snap, err := fsutil.ReadSnapshot(path)
	if err != nil || !snap.Exists {
		return ServerEntry{}, false, err
	}
	if w.isTOML() {
		m, ok, err := lookupTOMLServer(snap.Data, name)
		if err != nil {
			return ServerEntry{}, false, fmt.Errorf("%s: %w", path, err)
		}
		return entryFromTOML(m), ok, nil
	}
	o, ok, err := lookupJSONServer(snap.Data, name)
	if err != nil {
		return ServerEntry{}, false, fmt.Errorf("%s: %w", path, err)
	}
	return entryFromJSON(o), ok, nil
}

// edit computes the new file content for data; before/after are rendered entries for previews.
type edit func(data []byte) (out []byte, before, after string, changed bool, err error)

func (w *writer) addEdit(name string, e ServerEntry) edit {
	if w.isTOML() {
		return func(data []byte) ([]byte, string, string, bool, error) {
			out, before, after, err := upsertTOMLServer(data, name, ownedTOML(e))
			if err != nil {
				return nil, "", "", false, err
			}
			return out, renderTOMLPreview(name, before), renderTOMLPreview(name, after), true, nil
		}
	}
	return func(data []byte) ([]byte, string, string, bool, error) {
		out, before, after, err := upsertJSONServer(data, name, ownedJSON(w.r, e))
		if err != nil {
			return nil, "", "", false, err
		}
		return out, renderJSONPreview(before), renderJSONPreview(after), true, nil
	}
}

func (w *writer) removeEdit(name string) edit {
	if w.isTOML() {
		return func(data []byte) ([]byte, string, string, bool, error) {
			out, before, found, err := removeTOMLServer(data, name)
			return out, renderTOMLPreview(name, before), "", found, err
		}
	}
	return func(data []byte) ([]byte, string, string, bool, error) {
		out, before, found, err := removeJSONServer(data, name)
		return out, renderJSONPreview(before), "", found, err
	}
}

// fileChange plans an edit of path and returns a Change whose Apply re-reads
// the file, re-applies the edit and commits; one concurrent modification is
// absorbed by re-planning, a second is an error.
func (w *writer) fileChange(scope Scope, name string, remove bool, ed edit) (Change, error) {
	path, err := w.ConfigPath(scope)
	if err != nil {
		return Change{}, err
	}
	snap, err := fsutil.ReadSnapshot(path)
	if err != nil {
		return Change{}, err
	}
	_, before, after, changed, err := ed(snap.Data)
	if err != nil {
		return Change{}, fmt.Errorf("%s: %w; abby did not modify it", path, err)
	}
	c := Change{Runtime: w.r, Scope: scope, Method: MethodFile, Target: path, Server: name, Remove: remove,
		Noop: !changed, Preview: lineDiff(before, after), Notes: w.notes(scope)}
	c.apply = func() (string, error) {
		for attempt := 0; ; attempt++ {
			s, err := fsutil.ReadSnapshot(path)
			if err != nil {
				return "", err
			}
			out, _, _, changed, err := ed(s.Data)
			if err != nil {
				return "", fmt.Errorf("%s: %w; abby did not modify it", path, err)
			}
			if !changed {
				return "", nil
			}
			backup, err := s.Commit(out)
			if errors.Is(err, fsutil.ErrChangedOnDisk) && attempt == 0 {
				continue
			}
			if errors.Is(err, fsutil.ErrChangedOnDisk) {
				return "", fmt.Errorf("%s kept changing while abby was writing it (is %s running?); nothing was written, retry: %w", path, w.r, err)
			}
			return backup, err
		}
	}
	return c, nil
}

func (w *writer) PlanAdd(scope Scope, name string, e ServerEntry) (Change, error) {
	return w.fileChange(scope, name, false, w.addEdit(name, e))
}

func (w *writer) PlanRemove(scope Scope, name string) (Change, error) {
	return w.fileChange(scope, name, true, w.removeEdit(name))
}

func renderJSONPreview(o jsonObject) string {
	if o == nil {
		return ""
	}
	b, err := o.marshal()
	if err != nil {
		return ""
	}
	return string(b)
}

func renderTOMLPreview(name string, m map[string]any) string {
	if m == nil {
		return ""
	}
	s, err := renderTOMLServer(name, m)
	if err != nil {
		return ""
	}
	return s
}

```


- [ ] **Step 4: Minimally adapt `cmd/abby` so the module builds**

These are behaviour-preserving adaptations. Task 6 replaces them.

- `cmd/abby/install.go`:
  - Replace the call `runtimecfg.Resolve(runtimeFlag)` with `runtimecfg.Resolve(runtimeFlag, runtimecfg.Options{Method: runtimecfg.MethodFile})`.
  - Replace the body of `mergeRuntimeConfigs` with:

```go
	scope := runtimecfg.ScopeProject
	if global {
		scope = runtimecfg.ScopeUser
	}
	for _, w := range writers {
		for name, e := range entries {
			c, err := w.PlanAdd(scope, name, e)
			if err != nil {
				return fmt.Errorf("updating %s config: %w", w.Runtime(), err)
			}
			if _, err := c.Apply(); err != nil {
				return fmt.Errorf("updating %s for %s: %w", c.Target, w.Runtime(), err)
			}
			fmt.Printf("Updated %s (%s)\n", c.Target, w.Runtime())
		}
	}
	return nil
```

- `cmd/abby/build.go`:
  - Change `runtimecfg.Resolve(runtimeFlag)` the same way.
  - Replace the `for _, w := range writers { if err := w.Merge(...` loop with a call to `mergeRuntimeConfigs(writers, false, entries)`.
- `cmd/abby/uninstall.go`:
  - Change `Resolve` the same way.
  - Replace the unwire loop body with `PlanRemove(scope, name)` + `Apply()` (`scope` from `entry.Scope == "global"`), keeping the existing warning and "Updated" prints.
- `cmd/abby/update.go`: change `runtimecfg.Detect()` to `runtimecfg.Detect(runtimecfg.Options{Method: runtimecfg.MethodFile})`.

- [ ] **Step 5: Isolate the integration suite from the repository and from real runtime CLIs**

From this task on, abby detects runtimes by CLI on PATH. On a developer machine `claude` is usually installed, and every config edit may drop a `*.abbyfile.bak`. The integration suite must touch neither the repo nor the real CLIs:

- At the top of `TestMain` in `internal/integration/agent_test.go`, add `os.Setenv("ABBY_CONFIG_METHOD", "file")`. Every child process inherits it, and Task 6 makes abby honour it.
- Every `abby build` invocation in `internal/integration/*.go` (in `TestMain`, and in the budget, sandbox, stdio and plugin helpers) must run with `cmd.Dir` set to its temp dir, not `projectRoot`. The `-f`, `-o` and `--module-dir` arguments are already absolute. Check each one with `grep -n "Dir = projectRoot" internal/integration/*.go`.
- Add `*.abbyfile.bak` to `.gitignore`.
- Then run `git check-ignore .mcp.json .gemini/settings.json`. This shows which of the repo's own runtime configs are tracked.

- [ ] **Step 6: Run the tests**

Run: `go build ./... && go test ./pkg/runtimecfg/ -cover -v && go test ./cmd/... && make integration && git status --short`
Expected: PASS, and `git status --short` shows only this task's source changes. The integration run must leave no `.mcp.json`, `.gemini/`, `.codex/` or `*.abbyfile.bak` changes in the repository.
Expected: PASS, with `pkg/runtimecfg` coverage ≥ 80%.

The existing distribution integration tests must still pass. They now write `.mcp.json` through the new file method. If a test asserted a byte-exact old format, update it to parse the JSON.

- [ ] **Step 7: Commit**

```bash
git add -A pkg/runtimecfg cmd/abby internal/integration .gitignore
```
```bash
git commit -m "feat: runtimecfg plan/apply model with safe file edits; Claude user scope in ~/.claude.json (D7, D8)

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 5: CLI-backed changes: Claude `add-json`/`remove` with restore, and Gemini user scope (C1)

**Files:**
- Create: `pkg/runtimecfg/cliwriter.go`, `pkg/runtimecfg/cliwriter_test.go`
- Modify: `pkg/runtimecfg/filewriter.go`: `PlanAdd`/`PlanRemove` dispatch through `choose`

**Interfaces:**
- Consumes: `writer`, `fileChange`, `ownedJSON`, `ownedKeys`, `lookupJSONServer`, `renderJSONPreview`, `lineDiff`, `cliName`, `Options.Run`, `Options.LookPath` (Task 4).
- Produces:
  - `func (w *writer) existingJSON(scope Scope, name string) (jsonObject, error)`
  - `func (w *writer) cliReason(scope Scope, e *ServerEntry, existing jsonObject) string`
  - `func (w *writer) choose(scope Scope, e *ServerEntry, existing jsonObject) (Method, error)`
  - `func (w *writer) cliAddChange(scope Scope, name string, e ServerEntry, existing jsonObject) (Change, error)`
  - `func (w *writer) cliRemoveChange(scope Scope, name string, existing jsonObject) (Change, error)`
  - the unexported const `cliTimeout = 30 * time.Second`

- [ ] **Step 1: Write the failing tests**

`pkg/runtimecfg/cliwriter_test.go`:

```go
package runtimecfg

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}

// fakeRunner records calls; fail maps "subcommand" (args[1]) to an error.
func fakeRunner(calls *[]call, fail map[string]error) CommandRunner {
	return func(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
		*calls = append(*calls, call{name, append([]string(nil), args...)})
		if len(args) > 1 {
			if err, ok := fail[args[1]]; ok {
				return nil, []byte(err.Error()), err
			}
		}
		return nil, nil, nil
	}
}

func cliOpts(calls *[]call, fail map[string]error, onPath ...string) Options {
	return Options{Method: MethodAuto, Run: fakeRunner(calls, fail), LookPath: func(n string) (string, error) {
		for _, p := range onPath {
			if p == n {
				return "/fake/" + n, nil
			}
		}
		return "", os.ErrNotExist
	}}
}

func TestClaudeUsesCLIWhenOnPath(t *testing.T) {
	chdirTemp(t)
	var calls []call
	w := For(ClaudeCode, cliOpts(&calls, nil, "claude"))
	c, err := w.PlanAdd(ScopeProject, "agent", entry())
	if err != nil || c.Method != MethodCLI {
		t.Fatalf("change = %+v, %v", c, err)
	}
	if !strings.Contains(c.Preview, "claude mcp add-json -s project agent") {
		t.Errorf("preview must show the command: %s", c.Preview)
	}
	if _, err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].args[0] != "mcp" || calls[0].args[1] != "add-json" {
		t.Fatalf("calls = %+v (no remove when the entry is absent)", calls)
	}
	js := calls[0].args[len(calls[0].args)-1]
	for _, want := range []string{`"type":"stdio"`, `"command":"/abs/bin/agent"`, `"timeout":130000`} {
		if !strings.Contains(js, want) {
			t.Errorf("add-json payload missing %s: %s", want, js)
		}
	}
}

func TestClaudeReplacesExistingViaRemoveThenAdd(t *testing.T) {
	d := chdirTemp(t)
	os.WriteFile(d+"/.mcp.json", []byte(`{"mcpServers":{"agent":{"type":"stdio","command":"/old","args":[],"env":{"K":"v"}}}}`), 0o600)
	var calls []call
	c, _ := For(ClaudeCode, cliOpts(&calls, nil, "claude")).PlanAdd(ScopeProject, "agent", entry())
	if _, err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].args[1] != "remove" || calls[1].args[1] != "add-json" {
		t.Fatalf("calls = %+v", calls)
	}
	if !strings.Contains(calls[1].args[len(calls[1].args)-1], `"K":"v"`) {
		t.Error("existing env must be carried into the new entry")
	}
}

// Review Focus #4.
func TestClaudeCLIRestoresOnAddFailure(t *testing.T) {
	d := chdirTemp(t)
	os.WriteFile(d+"/.mcp.json", []byte(`{"mcpServers":{"agent":{"type":"stdio","command":"/old","args":[]}}}`), 0o600)
	var calls []call
	n := 0
	opts := cliOpts(&calls, nil, "claude")
	opts.Run = func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		calls = append(calls, call{name, args})
		if args[1] == "add-json" {
			n++
			if n == 1 {
				return nil, []byte("boom"), errors.New("exit status 1")
			}
		}
		return nil, nil, nil
	}
	c, _ := For(ClaudeCode, opts).PlanAdd(ScopeProject, "agent", entry())
	_, err := c.Apply()
	if err == nil || !strings.Contains(err.Error(), "restored") {
		t.Fatalf("err = %v, want failure that reports the restore", err)
	}
	last := calls[len(calls)-1]
	if last.args[1] != "add-json" || !strings.Contains(last.args[len(last.args)-1], `"/old"`) {
		t.Errorf("last call must re-add the old entry: %+v", last)
	}
}

func TestClaudeFallsBackToFileWhenUnownedKeysExist(t *testing.T) {
	d := chdirTemp(t)
	os.WriteFile(d+"/.mcp.json", []byte(`{"mcpServers":{"agent":{"command":"/old","args":[],"alwaysAllow":["x"]}}}`), 0o600)
	var calls []call
	c, _ := For(ClaudeCode, cliOpts(&calls, nil, "claude")).PlanAdd(ScopeProject, "agent", entry())
	if c.Method != MethodFile {
		t.Fatalf("method = %s; add-json would drop alwaysAllow", c.Method)
	}
}

func TestClaudeRemoveViaCLI(t *testing.T) {
	d := chdirTemp(t)
	os.WriteFile(d+"/.mcp.json", []byte(`{"mcpServers":{"agent":{"command":"/old","args":[]}}}`), 0o600)
	var calls []call
	c, err := For(ClaudeCode, cliOpts(&calls, nil, "claude")).PlanRemove(ScopeProject, "agent")
	if err != nil || c.Method != MethodCLI {
		t.Fatalf("%+v %v", c, err)
	}
	c.Apply()
	if len(calls) != 1 || strings.Join(calls[0].args, " ") != "mcp remove -s project agent" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestGeminiCLIOnlyForUserScopeWithoutCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	chdirTemp(t)
	var calls []call
	w := For(Gemini, cliOpts(&calls, nil, "gemini"))
	proj, _ := w.PlanAdd(ScopeProject, "agent", entry())
	if proj.Method != MethodFile {
		t.Errorf("project scope needs cwd → file, got %s", proj.Method)
	}
	e := entry()
	e.Cwd = ""
	e.Env = map[string]string{"A": "1"}
	user, _ := w.PlanAdd(ScopeUser, "agent", e)
	if user.Method != MethodCLI {
		t.Fatalf("user scope without cwd → cli, got %s", user.Method)
	}
	user.Apply()
	got := strings.Join(calls[0].args, " ")
	if got != "mcp add -s user -e A=1 --timeout 130000 agent /abs/bin/agent serve-mcp" {
		t.Errorf("gemini args = %q", got)
	}
}

func TestCodexNeverUsesCLI(t *testing.T) {
	chdirTemp(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	var calls []call
	c, _ := For(Codex, cliOpts(&calls, nil, "codex")).PlanAdd(ScopeUser, "agent", entry())
	if c.Method != MethodFile {
		t.Errorf("codex method = %s", c.Method)
	}
	forced := cliOpts(&calls, nil, "codex")
	forced.Method = MethodCLI
	if _, err := For(Codex, forced).PlanAdd(ScopeUser, "agent", entry()); err == nil || !strings.Contains(err.Error(), "codex") {
		t.Errorf("--config-method cli for codex must explain why it can't: %v", err)
	}
}

func TestForcedCLIWithoutBinaryErrors(t *testing.T) {
	chdirTemp(t)
	var calls []call
	opts := cliOpts(&calls, nil)
	opts.Method = MethodCLI
	if _, err := For(ClaudeCode, opts).PlanAdd(ScopeProject, "agent", entry()); err == nil || !strings.Contains(err.Error(), "not on PATH") {
		t.Errorf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/runtimecfg/ -run 'CLI|Gemini|Codex|Forced' -v`
Expected: FAIL. Every change is still `MethodFile`.

- [ ] **Step 3: Implement `choose` and the CLI changes**

In `filewriter.go`, replace `PlanAdd` and `PlanRemove` with:

```go
func (w *writer) PlanAdd(scope Scope, name string, e ServerEntry) (Change, error) {
	existing, err := w.existingJSON(scope, name)
	if err != nil {
		return Change{}, err
	}
	m, err := w.choose(scope, &e, existing)
	if err != nil {
		return Change{}, err
	}
	if m == MethodCLI {
		return w.cliAddChange(scope, name, e, existing)
	}
	return w.fileChange(scope, name, false, w.addEdit(name, e))
}

func (w *writer) PlanRemove(scope Scope, name string) (Change, error) {
	existing, err := w.existingJSON(scope, name)
	if err != nil {
		return Change{}, err
	}
	m, err := w.choose(scope, nil, existing)
	if err != nil {
		return Change{}, err
	}
	if m == MethodCLI {
		return w.cliRemoveChange(scope, name, existing)
	}
	return w.fileChange(scope, name, true, w.removeEdit(name))
}
```

`pkg/runtimecfg/cliwriter.go`:

```go
package runtimecfg

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teabranch/abbyfile/pkg/fsutil"
)

// cliTimeout bounds one runtime-CLI invocation.
const cliTimeout = 30 * time.Second

// existingJSON returns the current entry for JSON runtimes (nil when absent
// or for Codex). A parse failure is returned so nothing proceeds on a broken file.
func (w *writer) existingJSON(scope Scope, name string) (jsonObject, error) {
	if w.isTOML() {
		return nil, nil
	}
	path, err := w.ConfigPath(scope)
	if err != nil {
		return nil, err
	}
	snap, err := fsutil.ReadSnapshot(path)
	if err != nil {
		return nil, err
	}
	if !snap.Exists {
		return nil, nil
	}
	o, _, err := lookupJSONServer(snap.Data, name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w; abby did not modify it", path, err)
	}
	return o, nil
}

// cliReason returns "" when the runtime CLI can apply this change without
// losing anything, else why not. e == nil means a removal.
func (w *writer) cliReason(scope Scope, e *ServerEntry, existing jsonObject) string {
	switch w.r {
	case Codex:
		return "codex's CLI cannot set cwd or timeouts and has no project scope"
	case Gemini:
		if scope != ScopeUser {
			return "gemini's CLI cannot set cwd, which project-scope entries need"
		}
		if e != nil && e.Cwd != "" {
			return "gemini's CLI cannot set cwd"
		}
		if e != nil {
			for _, a := range e.Args {
				if strings.HasPrefix(a, "-") {
					return "an argument starts with '-', which gemini's CLI would parse as a flag"
				}
			}
		}
	}
	owned := ownedKeys(w.r)
	for _, f := range existing {
		if !owned[f.Key] {
			return fmt.Sprintf("the existing entry has %q, which %s's CLI would drop", f.Key, cliName(w.r))
		}
	}
	return ""
}

// choose picks MethodCLI or MethodFile for one change.
func (w *writer) choose(scope Scope, e *ServerEntry, existing jsonObject) (Method, error) {
	if w.opts.Method == MethodFile {
		return MethodFile, nil
	}
	_, pathErr := w.opts.LookPath(cliName(w.r))
	reason := w.cliReason(scope, e, existing)
	if w.opts.Method == MethodCLI {
		if pathErr != nil {
			return "", fmt.Errorf("--config-method cli: %s is not on PATH", cliName(w.r))
		}
		if reason != "" {
			return "", fmt.Errorf("--config-method cli: %s", reason)
		}
		return MethodCLI, nil
	}
	if pathErr == nil && reason == "" {
		return MethodCLI, nil
	}
	return MethodFile, nil
}

func (w *writer) run(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()
	_, stderr, err := w.opts.Run(ctx, cliName(w.r), args...)
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", cliName(w.r), strings.Join(args, " "), err, strings.TrimSpace(string(stderr)))
	}
	return nil
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"$`\\{}[]*?;&|<>()") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func commandLine(name string, args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = shellQuote(a)
	}
	return name + " " + strings.Join(q, " ")
}

// mergedJSON is existing overlaid with abby's owned keys.
func mergedJSON(existing, set jsonObject) jsonObject {
	out := append(jsonObject(nil), existing...)
	for _, f := range set {
		out = out.with(f.Key, f.Value)
	}
	return out
}

func (w *writer) cliAddChange(scope Scope, name string, e ServerEntry, existing jsonObject) (Change, error) {
	path, _ := w.ConfigPath(scope)
	after := mergedJSON(existing, ownedJSON(w.r, e))
	var cmds [][]string
	switch w.r {
	case ClaudeCode:
		payload, _ := after.compact()
		if existing != nil {
			cmds = append(cmds, []string{"mcp", "remove", "-s", string(scope), name})
		}
		cmds = append(cmds, []string{"mcp", "add-json", "-s", string(scope), name, string(payload)})
	case Gemini:
		args := []string{"mcp", "add", "-s", string(scope)}
		env := e.Env
		if env == nil {
			env = entryFromJSON(existing).Env
		}
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "-e", k+"="+env[k])
		}
		if e.Timeout >= time.Second {
			args = append(args, "--timeout", strconv.FormatInt(e.Timeout.Milliseconds(), 10))
		}
		args = append(args, name, e.Command)
		cmds = append(cmds, append(args, e.Args...))
	}
	var lines []string
	for _, c := range cmds {
		lines = append(lines, "$ "+commandLine(cliName(w.r), c))
	}
	c := Change{Runtime: w.r, Scope: scope, Method: MethodCLI, Target: path, Server: name,
		Preview: lineDiff(renderJSONPreview(existing), renderJSONPreview(after)) + strings.Join(lines, "\n") + "\n"}
	c.apply = func() (string, error) {
		for i, args := range cmds {
			if err := w.run(args...); err != nil {
				if w.r == ClaudeCode && existing != nil && i > 0 {
					old, _ := existing.compact()
					if rerr := w.run("mcp", "add-json", "-s", string(scope), name, string(old)); rerr != nil {
						return "", fmt.Errorf("%w; restoring the previous entry also failed (%v) — re-add it with: %s",
							err, rerr, commandLine("claude", []string{"mcp", "add-json", "-s", string(scope), name, string(old)}))
					}
					return "", fmt.Errorf("%w; the previous entry was restored", err)
				}
				return "", err
			}
		}
		return "", nil
	}
	return c, nil
}

func (w *writer) cliRemoveChange(scope Scope, name string, existing jsonObject) (Change, error) {
	path, _ := w.ConfigPath(scope)
	c := Change{Runtime: w.r, Scope: scope, Method: MethodCLI, Target: path, Server: name, Remove: true, Noop: existing == nil}
	args := []string{"mcp", "remove", "-s", string(scope), name}
	c.Preview = lineDiff(renderJSONPreview(existing), "") + "$ " + commandLine(cliName(w.r), args) + "\n"
	c.apply = func() (string, error) { return "", w.run(args...) }
	return c, nil
}

```


`existingJSON` works on an entry that exists but has no unowned keys, so `TestClaudeReplacesExistingViaRemoveThenAdd` passes (its existing entry uses only owned keys: `type`, `command`, `args`, `env`).

- [ ] **Step 4: Run the tests**

Run: `go test ./pkg/runtimecfg/ -cover -v`
Expected: PASS, coverage ≥ 80%. The Task 4 tests still pass because they use `fileOpts`.

- [ ] **Step 5: Commit**

```bash
git add pkg/runtimecfg/
```
```bash
git commit -m "feat: register via claude/gemini CLIs when they can express the entry; restore on failed replace (C1)

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 6: `cmd/abby` integration: options, dry-run, summary, env, timeouts (C4, C6)

**Files:**
- Create: `cmd/abby/options.go`, `cmd/abby/configchanges.go`, `cmd/abby/configchanges_test.go`
- Modify: `cmd/abby/describe.go`, `cmd/abby/install.go`, `cmd/abby/build.go`, `cmd/abby/uninstall.go`, `cmd/abby/update.go`
- Modify: `internal/integration/distribution_test.go`, adding `ABBY_CONFIG_METHOD=file` to every abby invocation
- Create: `internal/integration/install_config_test.go`

**Interfaces:**
- Consumes: the full `runtimecfg` API (Tasks 4–5).
- Produces:
  - `type installOptions struct { Global, DryRun, SkipChecksum bool; Writers []runtimecfg.ConfigWriter; Env map[string]string; Out, Err io.Writer }`. `SkipChecksum` is wired in Task 7.
  - `func scopeFor(global bool) runtimecfg.Scope`
  - `func projectRootFor(e registry.Entry) string`
  - `func parseEnvFlags(pairs []string) (map[string]string, error)`
  - `func configOptions(flag string) (runtimecfg.Options, error)`: the flag value, or `ABBY_CONFIG_METHOD`, or `auto`.
  - `type appliedChange struct { Change runtimecfg.Change; Backup string }`
  - `func applyEntries(opts installOptions, scope runtimecfg.Scope, entries map[string]runtimecfg.ServerEntry) ([]appliedChange, error)`
  - `func removeEntries(opts installOptions, scope runtimecfg.Scope, name string) ([]appliedChange, error)`
  - `func printSummary(w io.Writer, applied []appliedChange, dryRun bool)`
  - `agentManifest` gains `ToolTimeout string`, `Tools []struct{Name string}` and `Sandbox *manifestSandbox`.
  - `type manifestSandbox struct { AllowedDirs []string; Bash string; AllowCommands []string; MaxCommandTimeout string; Warnings []string }`, with the JSON names from the Phase B `--describe`: `allowedDirs`, `bash`, `allowCommands`, `maxCommandTimeout`, `warnings`.
  - `func runtimeTimeout(m *agentManifest) time.Duration`: 0 when m is nil.
  - `const runtimeTimeoutMargin = 10 * time.Second`

- [ ] **Step 1: Write the failing tests**

`cmd/abby/configchanges_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/registry"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

func chdir(t *testing.T) string {
	t.Helper()
	d, _ := filepath.EvalSymlinks(t.TempDir())
	old, _ := os.Getwd()
	os.Chdir(d)
	t.Cleanup(func() { os.Chdir(old) })
	return d
}

func fileWriters(rs ...runtimecfg.Runtime) []runtimecfg.ConfigWriter {
	opts := runtimecfg.Options{Method: runtimecfg.MethodFile, LookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	var ws []runtimecfg.ConfigWriter
	for _, r := range rs {
		ws = append(ws, runtimecfg.For(r, opts))
	}
	return ws
}

func TestRuntimeTimeout(t *testing.T) {
	if runtimeTimeout(nil) != 0 {
		t.Error("nil manifest → 0 (omit)")
	}
	m := &agentManifest{ToolTimeout: "45s"}
	if got := runtimeTimeout(m); got != 55*time.Second {
		t.Errorf("tool only = %s", got)
	}
	m.Tools = []manifestTool{{Name: "run_command"}}
	m.Sandbox = &manifestSandbox{MaxCommandTimeout: "2m0s"}
	if got := runtimeTimeout(m); got != 130*time.Second {
		t.Errorf("with run_command = %s", got)
	}
	if got := runtimeTimeout(&agentManifest{}); got != 40*time.Second {
		t.Errorf("default = %s", got)
	}
}

func TestParseEnvFlags(t *testing.T) {
	env, err := parseEnvFlags([]string{"A=1", "B=x=y"})
	if err != nil || env["A"] != "1" || env["B"] != "x=y" {
		t.Fatalf("%v %v", env, err)
	}
	for _, bad := range []string{"noequals", "=v", "1A=v", "A B=v"} {
		if _, err := parseEnvFlags([]string{bad}); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	d := chdir(t)
	var out bytes.Buffer
	opts := installOptions{DryRun: true, Writers: fileWriters(runtimecfg.ClaudeCode, runtimecfg.Gemini), Out: &out, Err: &out}
	applied, err := applyEntries(opts, runtimecfg.ScopeProject, map[string]runtimecfg.ServerEntry{"a": {Command: "/x", Args: []string{"serve-mcp"}}})
	if err != nil || len(applied) != 2 {
		t.Fatalf("%v %v", applied, err)
	}
	for _, p := range []string{".mcp.json", ".gemini/settings.json"} {
		if _, err := os.Stat(filepath.Join(d, p)); !os.IsNotExist(err) {
			t.Errorf("dry-run created %s", p)
		}
	}
	if !strings.Contains(out.String(), "+ ") {
		t.Errorf("dry-run must print the planned diff:\n%s", out.String())
	}
}

// Review Focus #1.
func TestInstallKeepsExistingEnv(t *testing.T) {
	d := chdir(t)
	os.WriteFile(filepath.Join(d, ".mcp.json"), []byte(`{"mcpServers":{"a":{"type":"stdio","command":"/old","args":[],"env":{"TOKEN":"keep"}}}}`), 0o600)
	var out bytes.Buffer
	opts := installOptions{Writers: fileWriters(runtimecfg.ClaudeCode), Out: &out, Err: &out}
	if _, err := applyEntries(opts, runtimecfg.ScopeProject, map[string]runtimecfg.ServerEntry{"a": {Command: "/new", Args: []string{"serve-mcp"}}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(d, ".mcp.json"))
	if !strings.Contains(string(b), `"TOKEN": "keep"`) || !strings.Contains(string(b), `"/new"`) {
		t.Errorf("reinstall without --env must keep env:\n%s", b)
	}
}

func TestProjectEnvWarning(t *testing.T) {
	chdir(t)
	var out, errb bytes.Buffer
	opts := installOptions{Writers: fileWriters(runtimecfg.ClaudeCode), Env: map[string]string{"K": "v"}, Out: &out, Err: &errb}
	applyEntries(opts, runtimecfg.ScopeProject, map[string]runtimecfg.ServerEntry{"a": {Command: "/x", Env: map[string]string{"K": "v"}}})
	if !strings.Contains(errb.String(), "committed") || !strings.Contains(errb.String(), "${") {
		t.Errorf("warning = %q", errb.String())
	}
}

func TestPrintSummary(t *testing.T) {
	var out bytes.Buffer
	printSummary(&out, []appliedChange{
		{Change: runtimecfg.Change{Runtime: runtimecfg.ClaudeCode, Scope: runtimecfg.ScopeProject, Method: runtimecfg.MethodCLI, Target: ".mcp.json", Server: "a"}},
		{Change: runtimecfg.Change{Runtime: runtimecfg.Codex, Scope: runtimecfg.ScopeProject, Method: runtimecfg.MethodFile, Target: ".codex/config.toml", Server: "a", Notes: []string{"Codex loads project .codex/config.toml only in trusted projects"}}, Backup: ".codex/config.toml.abbyfile.bak"},
	}, false)
	s := out.String()
	for _, want := range []string{"RUNTIME", "SCOPE", "METHOD", "TARGET", "BACKUP", "claude-code", "cli", "codex", "file", ".abbyfile.bak", "trusted"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary missing %q:\n%s", want, s)
		}
	}
}

func TestProjectRootFor(t *testing.T) {
	if got := projectRootFor(registry.Entry{Scope: "local", Path: "/p/proj/.abbyfile/bin/a"}); got != "/p/proj" {
		t.Errorf("local = %q", got)
	}
	if got := projectRootFor(registry.Entry{Scope: "global", Path: "/usr/local/bin/a"}); got != "" {
		t.Errorf("global = %q", got)
	}
	if got := projectRootFor(registry.Entry{Scope: "local", Path: "/odd/place/a"}); got != "" {
		t.Errorf("unknown layout = %q", got)
	}
}

func TestConfigOptionsFromEnv(t *testing.T) {
	t.Setenv("ABBY_CONFIG_METHOD", "file")
	o, err := configOptions("")
	if err != nil || o.Method != runtimecfg.MethodFile {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := configOptions("bogus"); err == nil {
		t.Error("bogus must fail")
	}
}
```

`internal/integration/install_config_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func abbyIn(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, abbyBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func stageBuild(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "build"), 0o755)
	if out, err := exec.Command("cp", binaryPath, filepath.Join(dir, "build", "test-agent")).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	return dir
}

func TestInstallDryRunWritesNothing(t *testing.T) {
	dir := stageBuild(t)
	home := t.TempDir()
	out, err := abbyIn(t, dir, []string{"HOME=" + home, "ABBY_CONFIG_METHOD=file"}, "install", "--dry-run", "--runtime", "all", "test-agent")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, p := range []string{".mcp.json", ".codex", ".gemini", ".abbyfile"} {
		if _, err := os.Stat(filepath.Join(dir, p)); !os.IsNotExist(err) {
			t.Errorf("dry-run created %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".abbyfile", "registry.json")); !os.IsNotExist(err) {
		t.Error("dry-run touched the registry")
	}
	for _, want := range []string{"RUNTIME", "claude-code", "codex", "gemini", "+ "} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestInstallWithFakeClaudeCLI(t *testing.T) {
	dir := stageBuild(t)
	home := t.TempDir()
	bin := t.TempDir()
	log := filepath.Join(bin, "calls.log")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\nexit 0\n"
	os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755)
	out, err := abbyIn(t, dir, []string{"HOME=" + home, "PATH=" + bin, "ABBY_CONFIG_METHOD=auto"}, "install", "--runtime", "claude-code", "test-agent")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "mcp add-json -s project test-agent") {
		t.Errorf("claude CLI not used: %q\n%s", calls, out)
	}
	if !strings.Contains(out, "cli") {
		t.Errorf("summary must show method cli:\n%s", out)
	}
}
```

`PATH=<bin>` holds only the fake `claude`. The staged test agent is a static Go binary, so `abby install` can still run it for `--describe`. If anything else in the install path needs a system tool, add `/usr/bin:/bin` **after** `bin` and make sure no `claude` exists there.

In `internal/integration/distribution_test.go`, add `"ABBY_CONFIG_METHOD=file"` to every `cmd.Env` for an abby invocation.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/abby/ -v`
Expected: FAIL with `undefined: installOptions`.

- [ ] **Step 3: Implement options, changes, manifest and wiring**

`cmd/abby/options.go`:

```go
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/teabranch/abbyfile/pkg/registry"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

// installOptions carries per-invocation settings through install/build/uninstall/update.
type installOptions struct {
	Global       bool
	DryRun       bool
	SkipChecksum bool // Task 7
	Writers      []runtimecfg.ConfigWriter
	Env          map[string]string
	Out, Err     io.Writer
}

// projectRootFor returns the project directory of a local install, whose
// binary lives at <root>/.abbyfile/bin/<name>; "" for global installs or
// unrecognised layouts (callers then fall back to the current directory).
func projectRootFor(e registry.Entry) string {
	if e.Scope == "global" {
		return ""
	}
	bin := filepath.Dir(e.Path)
	if filepath.Base(bin) != "bin" || filepath.Base(filepath.Dir(bin)) != ".abbyfile" {
		return ""
	}
	return filepath.Dir(filepath.Dir(bin))
}

func scopeFor(global bool) runtimecfg.Scope {
	if global {
		return runtimecfg.ScopeUser
	}
	return runtimecfg.ScopeProject
}

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// parseEnvFlags parses repeated --env KEY=VALUE flags.
func parseEnvFlags(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	env := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || !envKey.MatchString(k) {
			return nil, fmt.Errorf("--env %q: want KEY=VALUE with KEY matching [A-Za-z_][A-Za-z0-9_]*", p)
		}
		env[k] = v
	}
	return env, nil
}

// configOptions resolves --config-method (flag, else ABBY_CONFIG_METHOD, else auto).
func configOptions(flag string) (runtimecfg.Options, error) {
	v := flag
	if v == "" {
		v = os.Getenv("ABBY_CONFIG_METHOD")
	}
	if v == "" {
		v = string(runtimecfg.MethodAuto)
	}
	m, err := runtimecfg.ParseMethod(v)
	if err != nil {
		return runtimecfg.Options{}, err
	}
	return runtimecfg.Options{Method: m}, nil
}
```

`cmd/abby/configchanges.go`:

```go
package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

type appliedChange struct {
	Change runtimecfg.Change
	Backup string
}

// applyEntries plans (and unless DryRun, applies) one change per writer per
// entry, printing each preview. It stops at the first error; changes already
// applied stay applied and are returned.
func applyEntries(opts installOptions, scope runtimecfg.Scope, entries map[string]runtimecfg.ServerEntry) ([]appliedChange, error) {
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(opts.Env) > 0 && scope == runtimecfg.ScopeProject {
		fmt.Fprintln(opts.Err, "warning: --env values are written into project config files, which are often committed; prefer ${VAR} references (Claude Code and Gemini CLI expand them) for secrets")
	}
	var done []appliedChange
	for _, w := range opts.Writers {
		for _, n := range names {
			c, err := w.PlanAdd(scope, n, entries[n])
			if err != nil {
				return done, fmt.Errorf("%s: %w", w.Runtime(), err)
			}
			if a, err := commit(opts, c); err != nil {
				return done, err
			} else {
				done = append(done, a)
			}
		}
	}
	return done, nil
}

// removeEntries plans/applies removal of name from every writer.
func removeEntries(opts installOptions, scope runtimecfg.Scope, name string) ([]appliedChange, error) {
	var done []appliedChange
	for _, w := range opts.Writers {
		c, err := w.PlanRemove(scope, name)
		if err != nil {
			fmt.Fprintf(opts.Err, "warning: %s: %v\n", w.Runtime(), err)
			continue
		}
		if c.Noop {
			continue
		}
		a, err := commit(opts, c)
		if err != nil {
			fmt.Fprintf(opts.Err, "warning: %v\n", err)
			continue
		}
		done = append(done, a)
	}
	return done, nil
}

func commit(opts installOptions, c runtimecfg.Change) (appliedChange, error) {
	verb := "update"
	if c.Remove {
		verb = "remove"
	}
	fmt.Fprintf(opts.Out, "%s %s in %s (%s, %s scope, via %s):\n%s", verb, c.Server, c.Target, c.Runtime, c.Scope, c.Method, indent(c.Preview))
	if opts.DryRun || c.Noop {
		return appliedChange{Change: c}, nil
	}
	backup, err := c.Apply()
	if err != nil {
		return appliedChange{}, fmt.Errorf("%s (%s): %w", c.Target, c.Runtime, err)
	}
	return appliedChange{Change: c, Backup: backup}, nil
}

func indent(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("    " + l + "\n")
	}
	return b.String()
}

// printSummary prints one table of what was (or, with dryRun, would be) changed.
func printSummary(w io.Writer, applied []appliedChange, dryRun bool) {
	if len(applied) == 0 {
		return
	}
	title := "Runtime config changes:"
	if dryRun {
		title = "Planned runtime config changes (dry run — nothing written):"
	}
	fmt.Fprintln(w, "\n"+title)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RUNTIME\tSCOPE\tMETHOD\tTARGET\tSERVER\tBACKUP")
	var notes []string
	seen := map[string]bool{}
	for _, a := range applied {
		c := a.Change
		backup := a.Backup
		if backup == "" {
			backup = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", c.Runtime, c.Scope, c.Method, c.Target, c.Server, backup)
		for _, n := range c.Notes {
			if !seen[n] {
				seen[n] = true
				notes = append(notes, n)
			}
		}
	}
	tw.Flush()
	for _, n := range notes {
		fmt.Fprintln(w, "note: "+n)
	}
}
```

`cmd/abby/describe.go`: extend the manifest and add `runtimeTimeout`:

```go
type manifestTool struct {
	Name string `json:"name"`
}

type manifestSandbox struct {
	AllowedDirs       []string `json:"allowedDirs"`
	Bash              string   `json:"bash"`
	AllowCommands     []string `json:"allowCommands"`
	MaxCommandTimeout string   `json:"maxCommandTimeout"`
	Warnings          []string `json:"warnings"`
}

// agentManifest is the JSON output from --describe.
type agentManifest struct {
	Name        string           `json:"name"`
	Version     string           `json:"version"`
	Description string           `json:"description"`
	Memory      bool             `json:"memory"`
	ToolTimeout string           `json:"toolTimeout"`
	Tools       []manifestTool   `json:"tools"`
	Sandbox     *manifestSandbox `json:"sandbox"`
}

// runtimeTimeoutMargin is added to the agent's largest limit so the runtime
// never kills a call the server would allow (spec B3/C4).
const runtimeTimeoutMargin = 10 * time.Second

const defaultAgentToolTimeout = 30 * time.Second

// runtimeTimeout is the runtime-side timeout for an agent: its largest
// effective tool limit plus a margin; 0 (omit) when m is nil.
func runtimeTimeout(m *agentManifest) time.Duration {
	if m == nil {
		return 0
	}
	limit := defaultAgentToolTimeout
	if d, err := time.ParseDuration(m.ToolTimeout); err == nil && d > 0 {
		limit = d
	}
	if m.Sandbox != nil {
		for _, t := range m.Tools {
			if t.Name == "run_command" {
				if d, err := time.ParseDuration(m.Sandbox.MaxCommandTimeout); err == nil && d > limit {
					limit = d
				}
			}
		}
	}
	return limit + runtimeTimeoutMargin
}
```

Wire the commands. Keep the existing structure and replace the global/writers parameters with `installOptions`:

- **`install`:**
  - Add the flags `--dry-run`, `--config-method` (default `""`, help: "auto (default; env ABBY_CONFIG_METHOD), cli, or file"), `--env` (`StringArrayVar`, repeatable) and `--insecure-skip-checksum` (declared here; Task 7 enforces it).
  - Build `opts` in `RunE`: `configOptions` → `runtimecfg.Resolve(runtimeFlag, cfgOpts)` → `parseEnvFlags`.
  - `installOne`, `runBulkInstall`, `runBulkLocalInstall`, `runBulkRemoteInstall`, `runMultiInstall`, `installMany`, `runLocalInstall` and `runRemoteInstall` all take `(ref, opts installOptions)` and return `([]appliedChange, error)`. `installMany` accumulates the changes, and the command prints `printSummary(opts.Out, all, opts.DryRun)` once at the end.
  - **Local install:** compute `dst`/`absDst` as today.
    - On `DryRun`: print `would install build/<name> → <dst>`, run `describeAgent` on `build/<name>`, and skip the copy and `trackInstall`.
    - Build the entry: `{Command: absDst, Args: ["serve-mcp"], Env: opts.Env, Cwd: cwdIfProject, Timeout: runtimeTimeout(m)}`. `cwdIfProject` is `os.Getwd()` for project scope and `""` for user scope.
    - The manifest comes from `describeAgent`. When that fails, use `m = nil`, which omits the timeout.
    - Call `applyEntries(opts, scopeFor(opts.Global), …)`.
  - **Remote install:** the same, with `describeAgent` run on the verified temp file. Task 7 reorders the checksum. On `DryRun`, skip the copy and `trackInstall` after verification.
  - Remove `mergeRuntimeConfigs` (Task 4's shim).
- **`build`:**
  - Add `--dry-run` and `--config-method`.
  - On `DryRun`: parse the defs, print notes, **skip `BuildAll`**, plan the entries with `Timeout: 0` (no binary to describe), and print the summary.
  - Otherwise, after `BuildAll`, call `describeAgent` on each built binary (a failure means timeout 0), set `Cwd: os.Getwd()`, then `applyEntries(opts, runtimecfg.ScopeProject, entries)` and `printSummary`.
- **Project root for commands run from anywhere.** `uninstall`, `update` and `doctor` act on registry entries and may run from any directory. For each entry, build its writers with `cfgOpts.ProjectRoot = projectRootFor(entry)` (resolve again per entry), and for project scope set the entry's `Cwd` to that root, never `os.Getwd()`. `install` and `build` leave `ProjectRoot` empty, which means the current directory.
- **`uninstall`:**
  - Add `--dry-run` and `--config-method`.
  - On `DryRun`, print the binary removal and the planned changes, and touch nothing.
  - Otherwise remove the binary, then `removeEntries(opts, scopeFor(entry.Scope == "global"), name)`, the registry removal and `printSummary`.
- **`update`:** per entry, `cfgOpts, _ := configOptions("")`, `cfgOpts.ProjectRoot = projectRootFor(entry)`, `writers := runtimecfg.Detect(cfgOpts)`. Pass `installOptions{Global: …, Writers: writers, Out: os.Stdout, Err: os.Stderr}` to `runRemoteInstall`. The binary destination is also derived from the entry: its existing `Path`, not `installBinDir` relative to the cwd. `Env` is nil, so existing env entries are kept.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./cmd/abby/ ./pkg/runtimecfg/ -cover -v && go test -tags integration ./internal/integration/ -run 'TestInstall|TestUninstall|TestList|TestInstallAll' -timeout 900s`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/abby internal/integration
```
```bash
git commit -m "feat: abby install/build/uninstall --dry-run, --config-method, --env, runtime timeouts and a change summary (C4, C6)

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 7: Mandatory checksums, verified before execution (C5, D9)

**Files:**
- Create: `cmd/abby/checksum.go`, `cmd/abby/checksum_test.go`, `cmd/abby/install_remote_test.go`
- Modify: `cmd/abby/install.go` (`runRemoteInstall`, the `newGitHubClient` variable, the flag wiring)

**Interfaces:**
- Consumes: `installOptions.SkipChecksum` (Task 6), `github.Client`, `github.Release`, `github.Asset`, `github.ParseChecksumFile`, `github.VerifyChecksum`, `findChecksumAsset` (existing).
- Produces:
  - `var newGitHubClient = github.NewClient`
  - `func verifyReleaseAsset(ctx context.Context, client *github.Client, release *github.Release, agent string, asset github.Asset, binPath string, skip bool, errOut io.Writer) error`

- [ ] **Step 1: Write the failing tests**

`cmd/abby/checksum_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/github"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// fakeReleaseServer serves assets by name under /dl/<name>.
func fakeReleaseServer(t *testing.T, assets map[string][]byte) (*github.Client, func(names ...string) *github.Release) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/dl/")
		b, ok := assets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	client := &github.Client{HTTPClient: srv.Client(), BaseURL: srv.URL}
	return client, func(names ...string) *github.Release {
		rel := &github.Release{TagName: "agent/v1.0.0"}
		for _, n := range names {
			rel.Assets = append(rel.Assets, github.Asset{Name: n, BrowserDownloadURL: srv.URL + "/dl/" + n})
		}
		return rel
	}
}

// Review Focus #5.
func TestVerifyReleaseAsset(t *testing.T) {
	bin := []byte("#!/bin/sh\necho hi\n")
	binPath := filepath.Join(t.TempDir(), "agent")
	os.WriteFile(binPath, bin, 0o644)
	asset := github.Asset{Name: "agent-darwin-arm64"}
	good := []byte(sum(bin) + "  agent-darwin-arm64\n")
	client, release := fakeReleaseServer(t, map[string][]byte{"agent-sha256sums.txt": good, "SHA256SUMS": []byte(strings.Repeat("0", 64) + "  agent-darwin-arm64\n"), "checksums.txt": []byte(sum(bin) + "  other\n")})
	ctx := context.Background()
	var errOut bytes.Buffer

	if err := verifyReleaseAsset(ctx, client, release("agent-sha256sums.txt"), "agent", asset, binPath, false, &errOut); err != nil {
		t.Errorf("good checksum: %v", err)
	}
	if err := verifyReleaseAsset(ctx, client, release(), "agent", asset, binPath, false, &errOut); err == nil || !strings.Contains(err.Error(), "--insecure-skip-checksum") {
		t.Errorf("no checksum asset: %v", err)
	}
	if err := verifyReleaseAsset(ctx, client, release("SHA256SUMS"), "agent", asset, binPath, false, &errOut); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("mismatch: %v", err)
	}
	if err := verifyReleaseAsset(ctx, client, release("checksums.txt"), "agent", asset, binPath, false, &errOut); err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Errorf("no entry: %v", err)
	}
	broken := &github.Release{TagName: "x", Assets: []github.Asset{{Name: "agent-sha256sums.txt", BrowserDownloadURL: "http://127.0.0.1:1/nope"}}}
	if err := verifyReleaseAsset(ctx, client, broken, "agent", asset, binPath, false, &errOut); err == nil {
		t.Error("download failure must fail")
	}
	errOut.Reset()
	if err := verifyReleaseAsset(ctx, client, release(), "agent", asset, binPath, true, &errOut); err != nil || !strings.Contains(errOut.String(), "WARNING") {
		t.Errorf("skip: err=%v warning=%q", err, errOut.String())
	}
}
```

`cmd/abby/install_remote_test.go` is the ordering test. It uses a fake binary that writes a marker file when it runs:

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/github"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

// Review Focus #5: a tampered binary must never run.
func TestRemoteInstallVerifiesBeforeExecuting(t *testing.T) {
	dir := chdir(t)
	t.Setenv("HOME", t.TempDir())
	marker := filepath.Join(dir, "executed")
	bin := []byte("#!/bin/sh\ntouch " + marker + "\necho '{\"name\":\"agent\",\"version\":\"1.0.0\"}'\n")
	assetName := fmt.Sprintf("agent-%s-%s", runtime.GOOS, runtime.GOARCH)
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases"):
			json.NewEncoder(w).Encode([]github.Release{{TagName: "agent/v1.0.0", Assets: []github.Asset{
				{Name: assetName, BrowserDownloadURL: srvURL + "/dl/bin"},
				{Name: "agent-sha256sums.txt", BrowserDownloadURL: srvURL + "/dl/sums"},
			}}})
		case strings.HasSuffix(r.URL.Path, "/dl/bin"):
			w.Write(bin)
		case strings.HasSuffix(r.URL.Path, "/dl/sums"):
			fmt.Fprintf(w, "%s  %s\n", strings.Repeat("0", 64), assetName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL
	old := newGitHubClient
	newGitHubClient = func() *github.Client { return &github.Client{HTTPClient: srv.Client(), BaseURL: srv.URL} }
	defer func() { newGitHubClient = old }()

	opts := installOptions{Writers: fileWriters(runtimecfg.ClaudeCode), Out: os.Stdout, Err: os.Stderr}
	_, err := runRemoteInstall("github.com/o/r/agent", opts)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("the downloaded binary was executed before its checksum was verified")
	}
}
```

If `LatestRelease` reaches a different endpoint than `/releases`, check `pkg/github/github.go` `LatestRelease` and serve that path, keeping the assertion. The fake binary is a `#!/bin/sh` script, so skip this test with `t.Skip` on Windows.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/abby/ -run 'Verify|RemoteInstall' -v`
Expected: FAIL with `undefined: verifyReleaseAsset` / `newGitHubClient`.

- [ ] **Step 3: Implement**

`cmd/abby/checksum.go`:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/teabranch/abbyfile/pkg/github"
)

// newGitHubClient is replaced in tests. No environment variable may redirect
// the API base URL: the client sends the user's GitHub token there.
var newGitHubClient = github.NewClient

// verifyReleaseAsset checks binPath against the release's checksum asset.
// Every failure is fatal unless skip is set, which prints a warning instead.
func verifyReleaseAsset(ctx context.Context, client *github.Client, release *github.Release, agent string, asset github.Asset, binPath string, skip bool, errOut io.Writer) error {
	if skip {
		fmt.Fprintf(errOut, "WARNING: --insecure-skip-checksum: installing %s from %s WITHOUT verifying its checksum\n", asset.Name, release.TagName)
		return nil
	}
	sumsAsset := findChecksumAsset(release, agent)
	if sumsAsset == nil {
		return fmt.Errorf("release %s has no checksum asset (looked for %s-sha256sums.txt, SHA256SUMS, checksums.txt); refusing to install — republish with `abby publish`, or pass --insecure-skip-checksum", release.TagName, agent)
	}
	f, err := os.CreateTemp("", "abbyfile-sums-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(f.Name())
	err = client.DownloadAsset(ctx, *sumsAsset, f)
	f.Close()
	if err != nil {
		return fmt.Errorf("downloading %s: %w; refusing to install (pass --insecure-skip-checksum to bypass)", sumsAsset.Name, err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		return err
	}
	expected, ok := github.ParseChecksumFile(string(data))[asset.Name]
	if !ok {
		return fmt.Errorf("checksum file %s has no entry for %s; refusing to install (pass --insecure-skip-checksum to bypass)", sumsAsset.Name, asset.Name)
	}
	if err := github.VerifyChecksum(binPath, expected); err != nil {
		return fmt.Errorf("checksum verification failed for %s: %w", asset.Name, err)
	}
	return nil
}
```

In `runRemoteInstall`:
- `client := newGitHubClient()`.
- After the download and `tmpFile.Close()`, **before** `os.Chmod` and `describeAgent`, call `verifyReleaseAsset(ctx, client, release, parsed.Agent, *asset, tmpPath, opts.SkipChecksum, opts.Err)` and return its error. Print `Checksum verified ✓` on success when not skipped.
- Delete the old optional checksum block.
- Pass `newGitHubClient()` in `runBulkRemoteInstall` too.
- Wire `--insecure-skip-checksum` into `opts.SkipChecksum`.
- In `update.go`, use `newGitHubClient()` as well. `update` doesn't expose the flag, so an update of a release without checksums fails with the same actionable message.

- [ ] **Step 4: Run the tests**

Run: `go test ./cmd/abby/ -cover -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/abby
```
```bash
git commit -m "feat: mandatory checksum verification before a downloaded agent runs (C5, D9)

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 8: `abby doctor`

**Files:**
- Create: `cmd/abby/doctor.go`, `cmd/abby/doctor_test.go`
- Modify: `cmd/abby/main.go` (register the command)
- Create: `internal/integration/doctor_test.go`

**Interfaces:**
- Consumes:
  - `registry.DefaultPath`, `registry.Load`, `(*Registry).Get`, and `(*Registry).List()`.
  - `describeAgent`, `runtimeTimeout`, `agentManifest`, `manifestSandbox`, `scopeFor`, `configOptions` (Task 6).
  - `runtimecfg.Detect`, `ConfigWriter.Lookup`, `ConfigWriter.ConfigPath`, `LegacyClaudePath`, `lookupJSONServer` (unexported; use a small local JSON parse in doctor).
  - go-sdk `gomcp.NewClient`, `gomcp.CommandTransport`, `ClientSessionOptions{ProtocolVersion}`, `(*ClientSession).InitializeResult()`.
- Produces:
  - `type checkStatus int` with `statusOK`, `statusWarn`, `statusFail`
  - `type check struct { Status checkStatus; Text string }`
  - `type doctorDeps struct { describe func(string) (*agentManifest, error); handshake func(ctx context.Context, bin, protocolVersion string) (string, error); writers []runtimecfg.ConfigWriter; legacyPath func() (string, error) }`
  - `func diagnoseAgent(e registry.Entry, d doctorDeps) []check`
  - `func legacyChecks(d doctorDeps) []check`
  - `func printChecks(w io.Writer, title string, cs []check) (failed bool)`
  - `const legacyEraVersion = "2025-11-25"`

- [ ] **Step 1: Write the failing tests**

`cmd/abby/doctor_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/registry"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

func textOf(cs []check) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.Text + "\n")
	}
	return b.String()
}

func TestDiagnoseAgentHealthy(t *testing.T) {
	d := chdir(t)
	bin := filepath.Join(d, "agent")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	w := fileWriters(runtimecfg.ClaudeCode)[0]
	c, _ := w.PlanAdd(runtimecfg.ScopeProject, "agent", runtimecfg.ServerEntry{Command: bin, Args: []string{"serve-mcp"}, Timeout: 40 * time.Second})
	c.Apply()
	deps := doctorDeps{
		describe: func(string) (*agentManifest, error) {
			return &agentManifest{Name: "agent", Version: "1.0.0", Sandbox: &manifestSandbox{Bash: "restricted", AllowCommands: []string{"go test ./..."}, AllowedDirs: []string{d}}}, nil
		},
		handshake: func(_ context.Context, _, pv string) (string, error) {
			if pv == "" {
				return "2026-07-28", nil
			}
			return pv, nil
		},
		writers: []runtimecfg.ConfigWriter{w},
	}
	cs := diagnoseAgent(registry.Entry{Name: "agent", Path: bin, Scope: "local", Version: "1.0.0"}, deps)
	for _, c := range cs {
		if c.Status == statusFail {
			t.Errorf("unexpected failure: %s", c.Text)
		}
	}
	s := textOf(cs)
	for _, want := range []string{"2026-07-28", "2025-11-25", "restricted", "go test ./...", ".mcp.json"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
}

func TestDiagnoseAgentProblems(t *testing.T) {
	d := chdir(t)
	bin := filepath.Join(d, "agent")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	w := fileWriters(runtimecfg.ClaudeCode)[0]
	c, _ := w.PlanAdd(runtimecfg.ScopeProject, "agent", runtimecfg.ServerEntry{Command: "/elsewhere/agent", Args: []string{"serve-mcp"}, Timeout: 20 * time.Second})
	c.Apply()
	deps := doctorDeps{
		describe: func(string) (*agentManifest, error) {
			return &agentManifest{Name: "agent", ToolTimeout: "30s", Sandbox: &manifestSandbox{Bash: "unrestricted", Warnings: []string{"sandbox.bash is unrestricted"}}}, nil
		},
		handshake: func(context.Context, string, string) (string, error) { return "", errors.New("connection closed") },
		writers:   []runtimecfg.ConfigWriter{w},
	}
	cs := diagnoseAgent(registry.Entry{Name: "agent", Path: bin, Scope: "local"}, deps)
	s := textOf(cs)
	var fails, warns int
	for _, c := range cs {
		switch c.Status {
		case statusFail:
			fails++
		case statusWarn:
			warns++
		}
	}
	if fails < 2 || warns < 2 {
		t.Errorf("fails=%d warns=%d:\n%s", fails, warns, s)
	}
	for _, want := range []string{"/elsewhere/agent", "connection closed", "unrestricted", "40s"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
}

func TestDiagnoseMissingBinary(t *testing.T) {
	chdir(t)
	cs := diagnoseAgent(registry.Entry{Name: "agent", Path: "/nope/agent", Scope: "local"}, doctorDeps{})
	if len(cs) == 0 || cs[0].Status != statusFail {
		t.Fatalf("checks = %+v", cs)
	}
}

func TestLegacyChecks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mcp.json")
	os.WriteFile(p, []byte(`{"mcpServers":{"old-agent":{"command":"/x"}}}`), 0o644)
	cs := legacyChecks(doctorDeps{legacyPath: func() (string, error) { return p, nil }})
	if len(cs) != 1 || cs[0].Status != statusWarn || !strings.Contains(cs[0].Text, "old-agent") || !strings.Contains(cs[0].Text, p) {
		t.Errorf("legacy = %+v", cs)
	}
	if cs := legacyChecks(doctorDeps{legacyPath: func() (string, error) { return filepath.Join(t.TempDir(), "none"), nil }}); len(cs) != 0 {
		t.Errorf("no legacy file → no checks, got %+v", cs)
	}
}

func TestPrintChecks(t *testing.T) {
	var out bytes.Buffer
	failed := printChecks(&out, "agent", []check{{statusOK, "a"}, {statusWarn, "b"}, {statusFail, "c"}})
	if !failed || !strings.Contains(out.String(), "✓ a") || !strings.Contains(out.String(), "! b") || !strings.Contains(out.String(), "✗ c") {
		t.Errorf("failed=%v out=%q", failed, out.String())
	}
}
```

`internal/integration/doctor_test.go`:

```go
//go:build integration

package integration

import (
	"path/filepath"
	"strings"
	"testing"
)

// Doctor run from an unrelated directory must still find a local install's entries.
func TestDoctorFromElsewhere(t *testing.T) {
	dir := stageBuild(t)
	home := t.TempDir()
	env := []string{"HOME=" + home, "ABBY_CONFIG_METHOD=file"}
	if out, err := abbyIn(t, dir, env, "install", "--runtime", "claude-code", "test-agent"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	out, err := abbyIn(t, t.TempDir(), env, "doctor", "--runtime", "claude-code", "test-agent")
	if err != nil || !strings.Contains(out, filepath.Join(dir, ".mcp.json")) {
		t.Fatalf("doctor from elsewhere: %v\n%s", err, out)
	}
}

func TestDoctorAfterInstall(t *testing.T) {
	dir := stageBuild(t)
	home := t.TempDir()
	env := []string{"HOME=" + home, "ABBY_CONFIG_METHOD=file"}
	if out, err := abbyIn(t, dir, env, "install", "--runtime", "claude-code", "test-agent"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	out, err := abbyIn(t, dir, env, "doctor", "--runtime", "claude-code", "test-agent")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	for _, want := range []string{"test-agent", "2026-07-28", "2025-11-25", ".mcp.json", "✓"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/abby/ -run 'Diagnose|Legacy|PrintChecks' -v`
Expected: FAIL with `undefined: diagnoseAgent`.

- [ ] **Step 3: Implement**

`cmd/abby/doctor.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/teabranch/abbyfile/pkg/registry"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

type checkStatus int

const (
	statusOK checkStatus = iota
	statusWarn
	statusFail
)

type check struct {
	Status checkStatus
	Text   string
}

// legacyEraVersion is the pre-2026-07-28 protocol, negotiated via initialize.
const legacyEraVersion = "2025-11-25"

const doctorHandshakeTimeout = 10 * time.Second

type doctorDeps struct {
	describe   func(bin string) (*agentManifest, error)
	handshake  func(ctx context.Context, bin, protocolVersion string) (string, error)
	writers    []runtimecfg.ConfigWriter
	legacyPath func() (string, error)
}

func newDoctorCommand() *cobra.Command {
	var runtimeFlag, methodFlag string
	cmd := &cobra.Command{
		Use:   "doctor [agent]...",
		Short: "Diagnose installed agents: binary, runtime config entries, MCP handshake, sandbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgOpts, err := configOptions(methodFlag)
			if err != nil {
				return err
			}
			writers, err := runtimecfg.Resolve(runtimeFlag, cfgOpts)
			if err != nil {
				return err
			}
			regPath, err := registry.DefaultPath()
			if err != nil {
				return err
			}
			reg, err := registry.Load(regPath)
			if err != nil {
				return err
			}
			entries, err := doctorTargets(reg, args)
			if err != nil {
				return err
			}
			deps := doctorDeps{describe: describeAgent, handshake: mcpHandshake, legacyPath: runtimecfg.LegacyClaudePath}
			_ = writers // validates --runtime early
			failed := false
			for _, e := range entries {
				perEntry := cfgOpts
				perEntry.ProjectRoot = projectRootFor(e)
				if deps.writers, err = runtimecfg.Resolve(runtimeFlag, perEntry); err != nil {
					return err
				}
				if printChecks(cmd.OutOrStdout(), fmt.Sprintf("%s (v%s, %s, %s)", e.Name, e.Version, e.Scope, e.Path), diagnoseAgent(e, deps)) {
					failed = true
				}
			}
			if cs := legacyChecks(deps); len(cs) > 0 {
				printChecks(cmd.OutOrStdout(), "legacy config", cs)
			}
			if failed {
				return fmt.Errorf("doctor found problems")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&runtimeFlag, "runtime", "auto", "Runtimes to check: auto, all, claude-code, codex, gemini")
	cmd.Flags().StringVar(&methodFlag, "config-method", "", "Accepted for symmetry; doctor only reads config")
	return cmd
}

// doctorTargets returns the named registry entries, or all of them.
func doctorTargets(reg *registry.Registry, names []string) ([]registry.Entry, error) {
	if len(names) == 0 {
		all := reg.List()
		sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
		if len(all) == 0 {
			return nil, fmt.Errorf("no agents installed (see `abby install`)")
		}
		return all, nil
	}
	var out []registry.Entry
	for _, n := range names {
		e, ok := reg.Get(n)
		if !ok {
			return nil, fmt.Errorf("agent %q is not installed", n)
		}
		out = append(out, e)
	}
	return out, nil
}

func diagnoseAgent(e registry.Entry, d doctorDeps) []check {
	var cs []check
	fi, err := os.Stat(e.Path)
	if err != nil || fi.Mode()&0o111 == 0 {
		return []check{{statusFail, fmt.Sprintf("binary %s is missing or not executable — reinstall with `abby install`", e.Path)}}
	}
	cs = append(cs, check{statusOK, "binary " + e.Path})

	var m *agentManifest
	if d.describe != nil {
		if m, err = d.describe(e.Path); err != nil {
			cs = append(cs, check{statusFail, fmt.Sprintf("--describe failed: %v", err)})
		} else if m.Sandbox != nil {
			sb := m.Sandbox
			cmds, _ := json.Marshal(sb.AllowCommands)
			cs = append(cs, check{statusOK, fmt.Sprintf("sandbox: bash=%s allow_commands=%s allowed_dirs=%s", sb.Bash, cmds, strings.Join(sb.AllowedDirs, ", "))})
			for _, w := range sb.Warnings {
				cs = append(cs, check{statusWarn, "sandbox: " + w})
			}
			if sb.Bash == "unrestricted" && len(sb.Warnings) == 0 {
				cs = append(cs, check{statusWarn, "sandbox: bash is unrestricted"})
			}
		}
	}

	if d.handshake != nil {
		for _, pv := range []string{"", legacyEraVersion} {
			ctx, cancel := context.WithTimeout(context.Background(), doctorHandshakeTimeout)
			got, err := d.handshake(ctx, e.Path, pv)
			cancel()
			label := "server/discover"
			if pv != "" {
				label = "initialize"
			}
			if err != nil {
				cs = append(cs, check{statusFail, fmt.Sprintf("serve-mcp handshake (%s) failed: %v", label, err)})
			} else {
				cs = append(cs, check{statusOK, fmt.Sprintf("serve-mcp speaks %s (%s)", got, label)})
			}
		}
	}

	scope := scopeFor(e.Scope == "global")
	want := runtimeTimeout(m)
	for _, w := range d.writers {
		path, _ := w.ConfigPath(scope)
		entry, ok, err := w.Lookup(scope, e.Name)
		switch {
		case err != nil:
			cs = append(cs, check{statusFail, fmt.Sprintf("%s %s: %v", w.Runtime(), path, err)})
		case !ok:
			cs = append(cs, check{statusWarn, fmt.Sprintf("%s %s: no entry for %s — run `abby install --runtime %s %s`", w.Runtime(), path, e.Name, w.Runtime(), e.Name)})
		case entry.Command != e.Path:
			cs = append(cs, check{statusFail, fmt.Sprintf("%s %s: entry points at %s, not %s — reinstall", w.Runtime(), path, entry.Command, e.Path)})
		default:
			cs = append(cs, check{statusOK, fmt.Sprintf("%s %s → %s", w.Runtime(), path, entry.Command)})
		}
		if err != nil || !ok {
			continue
		}
		if want > 0 && entry.Timeout > 0 && entry.Timeout < want {
			cs = append(cs, check{statusWarn, fmt.Sprintf("%s timeout is %s but the agent may run for up to %s — reinstall to refresh it", w.Runtime(), entry.Timeout, want)})
		}
		if w.Runtime() == runtimecfg.Codex && scope == runtimecfg.ScopeProject {
			cs = append(cs, check{statusWarn, "Codex loads project .codex/config.toml only in trusted projects"})
		}
	}
	return cs
}

// legacyChecks reports entries in ~/.claude/mcp.json (written by abby <= v0.11; never read by Claude Code).
func legacyChecks(d doctorDeps) []check {
	if d.legacyPath == nil {
		return nil
	}
	p, err := d.legacyPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(data, &doc) != nil || len(doc.MCPServers) == 0 {
		return nil
	}
	names := make([]string, 0, len(doc.MCPServers))
	for n := range doc.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)
	return []check{{statusWarn, fmt.Sprintf("%s has entries %s written by abby < v0.12; Claude Code never reads this file — reinstall them with `abby install --global`, then delete %s", p, strings.Join(names, ", "), p)}}
}

func printChecks(w io.Writer, title string, cs []check) (failed bool) {
	fmt.Fprintln(w, title)
	for _, c := range cs {
		mark := "✓"
		switch c.Status {
		case statusWarn:
			mark = "!"
		case statusFail:
			mark = "✗"
			failed = true
		}
		fmt.Fprintf(w, "  %s %s\n", mark, c.Text)
	}
	return failed
}

// mcpHandshake connects to `bin serve-mcp` and returns the negotiated protocol version.
func mcpHandshake(ctx context.Context, bin, protocolVersion string) (string, error) {
	client := gomcp.NewClient(&gomcp.Implementation{Name: "abby-doctor", Version: cliVersion}, nil)
	cmd := exec.Command(bin, "serve-mcp")
	cmd.Stderr = io.Discard // the agent's own startup logs would clutter doctor output
	sess, err := client.Connect(ctx, &gomcp.CommandTransport{Command: cmd}, &gomcp.ClientSessionOptions{ProtocolVersion: protocolVersion})
	if err != nil {
		return "", err
	}
	defer sess.Close()
	return sess.InitializeResult().ProtocolVersion, nil
}
```

Register it in `cmd/abby/main.go` with `root.AddCommand(newDoctorCommand())`, following how the other commands are added.

`TestDiagnoseAgentProblems` expects "40s": the manifest has `ToolTimeout: "30s"`, so the wanted timeout is 40s, and the entry's 20s is below that.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go test ./cmd/abby/ -cover -v && go test -tags integration ./internal/integration/ -run TestDoctor -timeout 600s`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/abby internal/integration
```
```bash
git commit -m "feat: abby doctor — binary, runtime entries, both-era MCP handshake, sandbox and legacy config checks (C6)

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 9: Docs, migration notes and full verification

**Files:**
- Modify: `docs/guides/distribution.md`, `docs/reference.md`, `README.md` (install section only)
- Confirm `.gitignore` has `*.abbyfile.bak` (added in Task 4)

**Interfaces:**
- Consumes: everything above.
- Produces: nothing new.

- [ ] **Step 1: Write the docs**

In `docs/guides/distribution.md`, add or refresh these sections:

- **`## Where abby registers agents`.** A table per runtime and scope, with the paths from Global Constraints, including `CLAUDE_CONFIG_DIR` and `CODEX_HOME`. Explain the method rule: CLI when it can express the entry, otherwise a surgical file edit; Codex always uses the file method. Explain `--config-method` and `ABBY_CONFIG_METHOD`.
- **`## What abby changes, and how to preview it`.** Cover `--dry-run`, the summary table, backups (`*.abbyfile.bak`, once per file, with the same mode; suggest adding them to `.gitignore`), keys abby owns versus keys it preserves, and the refusal on an unparsable file (for example a Gemini `settings.json` with comments).
- **`## Entry fields`.** Cover `cwd` (project scope), `env` (`--env`, the project-scope warning, `${VAR}`), and timeouts (largest agent limit plus 10s; refreshed on reinstall; `doctor` warns when stale).
- **`## Checksums`.** Checksums are mandatory. Explain `--insecure-skip-checksum`, and that `abby publish` emits `<agent>-sha256sums.txt`.
- **`## abby doctor`.** Show a sample output block.
- **`### Migrating from v0.11`.** The numbered list:
  1. `abby install` requires a checksum asset. Pass `--insecure-skip-checksum` to bypass.
  2. Claude Code user-scope entries now go into `~/.claude.json` (or `$CLAUDE_CONFIG_DIR/.claude.json`), or through `claude mcp add-json`. Old `~/.claude/mcp.json` entries are reported by `abby doctor` with a removal hint.
  3. Runtimes are detected by CLI on PATH or by config dir, not by `$HOME` existing.
  4. Config edits refuse unparsable files, keep unknown keys and comments outside abby's block, write atomically, and back up once.
  5. Entries now carry `cwd`, `env` and a timeout. Codex project config loads only in trusted projects.
  6. `--dry-run`, `--config-method`, `--env` and `abby doctor` are new.

In `docs/reference.md`, document the new flags for `abby install|build|uninstall`, the `abby doctor` command, and the `ABBY_CONFIG_METHOD` environment variable.

In `README.md`, update the install snippet if it mentions `~/.claude/mcp.json`.

Run: `grep -rn "claude/mcp.json\|GlobalPath\|LocalPath" docs README.md cmd pkg | grep -v docs/superpowers`
Expected: only the legacy mentions in the doctor and migration docs.

- [ ] **Step 2: Full verification (what CI runs)**

Run: `make fmtcheck && go vet ./... && go test -race ./... && make integration && git status --short`
Expected: all PASS, and `git status --short` shows only this task's docs changes. No `.mcp.json`, `.gemini/`, `.codex/` or `*.abbyfile.bak` changes are left in the repository. Check that coverage is ≥ 80% on `pkg/runtimecfg` and `pkg/fsutil`: `go test -cover ./pkg/runtimecfg/ ./pkg/fsutil/`.

- [ ] **Step 3: Commit**

```bash
git add docs README.md
```
```bash
git commit -m "docs: runtime registration, dry-run, checksums, doctor and v0.12 migration notes

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Self-review record

- **Spec coverage:**
  - C1: Task 5 (CLI methods) plus Task 4 (file fallback, D7 path).
  - C2: Tasks 1–4.
  - C3: Task 4, `Detect`.
  - C4: Tasks 4 (rendering) and 6 (`env` flag, `cwd`, timeout from `--describe`).
  - C5: Task 7.
  - C6:
    - summary table and `--dry-run`: Task 6;
    - `doctor`: Task 8 (binary, entries per runtime, discover/initialize, sandbox, legacy path).
  - Migration items 5 and 6: Task 9.
  - The spec's Testing bullets:
    - golden round trips: Task 4;
    - parse-failure refusal: Task 4, Review Focus #3;
    - backup creation: Tasks 1 and 4;
    - fake CLIs on PATH: Tasks 5 and 6;
    - fake GitHub server and verify-before-execute: Task 7.
- **Type consistency:**
  - `Scope`/`Method`/`ServerEntry`/`Change`/`Options` are defined in Task 4, and Tasks 5–8 use them.
  - `installOptions`, `applyEntries`, `removeEntries`, `printSummary`, `runtimeTimeout`, `manifestTool` and `manifestSandbox` are defined in Task 6, and Tasks 7–8 use them.
  - `newGitHubClient` is defined in Task 7.
- **Deviations from the spec text, with reasons:**
  - Codex always uses the file method: its CLI can't set C4's fields.
  - Gemini uses its CLI only for user scope: its CLI has no `cwd`.
  - Claude has no `cwd` field: this was verified.
