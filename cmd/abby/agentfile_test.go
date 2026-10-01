package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/registry"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

const builtHelperMD = "---\nname: helper\n---\n\nYou help.\n\n<!-- abbyfile: helper v0.3.0 -->\n"

// agentFileProject chdirs into a fresh project with HOME isolated and
// build/.claude/agents/helper.md in place, as abby build leaves it.
func agentFileProject(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d := chdir(t)
	writeBuilt(t, d, builtHelperMD)
	return d
}

func writeBuilt(t *testing.T, d, content string) {
	t.Helper()
	dir := filepath.Join(d, "build", ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testOpts() installOptions {
	var out bytes.Buffer
	return installOptions{Writers: fileWriters(runtimecfg.ClaudeCode), Out: &out, Err: &out}
}

func loadEntry(t *testing.T, name string) (registry.Entry, bool) {
	t.Helper()
	p, err := registry.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return reg.Get(name)
}

func TestRunLocalInstall_FileOnlyAgent(t *testing.T) {
	d := agentFileProject(t)
	if _, err := runLocalInstall("helper", testOpts()); err != nil {
		t.Fatalf("runLocalInstall: %v", err)
	}
	dst := filepath.Join(d, ".claude", "agents", "helper.md")
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != builtHelperMD {
		t.Fatalf("installed file = %q, %v; want the built file", got, err)
	}
	if _, err := os.Stat(filepath.Join(d, ".mcp.json")); !os.IsNotExist(err) {
		t.Error("a file-only agent must not get an MCP config entry")
	}
	e, ok := loadEntry(t, "helper")
	if !ok {
		t.Fatal("no registry entry")
	}
	if e.Path != "" || e.AgentFile != dst || e.AgentFileSHA256 == "" || e.Version != "0.3.0" {
		t.Fatalf("entry = %+v", e)
	}
}

func TestRunLocalInstall_RefusesHandWrittenAgentFile(t *testing.T) {
	d := agentFileProject(t)
	dst := filepath.Join(d, ".claude", "agents", "helper.md")
	os.MkdirAll(filepath.Dir(dst), 0o755)
	os.WriteFile(dst, []byte("hand written\n"), 0o644)

	_, err := runLocalInstall("helper", testOpts())
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want a refusal naming --force", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "hand written\n" {
		t.Fatalf("hand-written file was changed: %q", got)
	}

	opts := testOpts()
	opts.Force = true
	if _, err := runLocalInstall("helper", opts); err != nil {
		t.Fatalf("--force install: %v", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != builtHelperMD {
		t.Fatalf("--force did not replace the file: %q", got)
	}
}

func TestRunLocalInstall_ReplacesOwnUnchangedFileButNotEditedOne(t *testing.T) {
	d := agentFileProject(t)
	if _, err := runLocalInstall("helper", testOpts()); err != nil {
		t.Fatal(err)
	}
	// A rebuilt file replaces the one abby installed and nobody touched.
	newer := strings.Replace(builtHelperMD, "v0.3.0", "v0.4.0", 1)
	writeBuilt(t, d, newer)
	if _, err := runLocalInstall("helper", testOpts()); err != nil {
		t.Fatalf("reinstall over own file: %v", err)
	}
	dst := filepath.Join(d, ".claude", "agents", "helper.md")
	if got, _ := os.ReadFile(dst); string(got) != newer {
		t.Fatalf("file not updated: %q", got)
	}

	// Once edited by hand, it is refused like any other file.
	os.WriteFile(dst, []byte(newer+"local edit\n"), 0o644)
	writeBuilt(t, d, builtHelperMD)
	if _, err := runLocalInstall("helper", testOpts()); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want a refusal for an edited file", err)
	}
}

func TestRunLocalInstall_RejectsSourceWithoutMarker(t *testing.T) {
	d := agentFileProject(t)
	writeBuilt(t, d, "---\nname: helper\n---\n\nNo marker.\n")
	if _, err := runLocalInstall("helper", testOpts()); err == nil || !strings.Contains(err.Error(), "abby build") {
		t.Fatalf("err = %v, want a not-generated-by-abby-build error", err)
	}
}

func TestRunLocalInstall_BinaryAlsoInstallsItsAgentFile(t *testing.T) {
	d := agentFileProject(t)
	os.WriteFile(filepath.Join(d, "build", "helper"), []byte("#!/bin/sh\nexit 1\n"), 0o755)
	if _, err := runLocalInstall("helper", testOpts()); err != nil {
		t.Fatalf("runLocalInstall: %v", err)
	}
	e, _ := loadEntry(t, "helper")
	if e.Path == "" || e.AgentFile == "" {
		t.Fatalf("want both binary and agent file tracked, got %+v", e)
	}
}

func TestRunBulkLocalInstall_FindsFileOnlyAgents(t *testing.T) {
	agentFileProject(t)
	if _, err := runBulkLocalInstall(testOpts()); err != nil {
		t.Fatalf("runBulkLocalInstall: %v", err)
	}
	if _, ok := loadEntry(t, "helper"); !ok {
		t.Fatal("file-only agent not installed by --all")
	}
}

// installedHelper installs the built helper file and returns its entry.
func installedHelper(t *testing.T) registry.Entry {
	t.Helper()
	agentFileProject(t)
	if _, err := runLocalInstall("helper", testOpts()); err != nil {
		t.Fatal(err)
	}
	e, _ := loadEntry(t, "helper")
	return e
}

func statusFor(cs []check, sub string) (checkStatus, bool) {
	for _, c := range cs {
		if strings.Contains(c.Text, sub) {
			return c.Status, true
		}
	}
	return 0, false
}

func TestAgentFileChecks(t *testing.T) {
	e := installedHelper(t)
	if st, ok := statusFor(agentFileChecks(e), "agent file "); !ok || st != statusOK {
		t.Fatalf("unchanged file: %s", textOf(agentFileChecks(e)))
	}

	os.WriteFile(e.AgentFile, []byte(builtHelperMD+"edit\n"), 0o644)
	if st, ok := statusFor(agentFileChecks(e), "edited since install"); !ok || st != statusWarn {
		t.Fatalf("edited file: %s", textOf(agentFileChecks(e)))
	}

	stale := e
	stale.Version = "0.9.0" // the binary moved on; the file did not
	os.WriteFile(e.AgentFile, []byte(builtHelperMD), 0o644)
	if st, ok := statusFor(agentFileChecks(stale), "v0.3.0"); !ok || st != statusWarn {
		t.Fatalf("version drift: %s", textOf(agentFileChecks(stale)))
	}

	os.Remove(e.AgentFile)
	if st, ok := statusFor(agentFileChecks(e), "missing"); !ok || st != statusFail {
		t.Fatalf("missing file: %s", textOf(agentFileChecks(e)))
	}
}

func TestDiagnoseAgent_FileOnlySkipsBinaryChecks(t *testing.T) {
	e := installedHelper(t)
	cs := diagnoseAgent(e, doctorDeps{writers: fileWriters(runtimecfg.ClaudeCode)})
	for _, c := range cs {
		if c.Status != statusOK {
			t.Fatalf("unexpected problem for a healthy file-only agent: %s", textOf(cs))
		}
	}
}

func TestRunUninstall_FileOnly(t *testing.T) {
	e := installedHelper(t)
	if err := runUninstall("helper", "claude-code", runtimecfg.Options{Method: runtimecfg.MethodFile}, false); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	if _, err := os.Stat(e.AgentFile); !os.IsNotExist(err) {
		t.Error("agent file not removed")
	}
	if _, ok := loadEntry(t, "helper"); ok {
		t.Error("registry entry not removed")
	}
}

func TestRunUninstall_KeepsEditedAgentFile(t *testing.T) {
	e := installedHelper(t)
	os.WriteFile(e.AgentFile, []byte("my edits\n"), 0o644)
	if err := runUninstall("helper", "claude-code", runtimecfg.Options{Method: runtimecfg.MethodFile}, false); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	if got, _ := os.ReadFile(e.AgentFile); string(got) != "my edits\n" {
		t.Fatalf("edited agent file was removed or changed: %q", got)
	}
}

func TestRunLocalInstall_SkipsAgentFileFromAnotherVersion(t *testing.T) {
	d := agentFileProject(t) // built file is v0.3.0
	bin := "#!/bin/sh\necho '{\"name\":\"helper\",\"version\":\"0.4.0\"}'\n"
	os.WriteFile(filepath.Join(d, "build", "helper"), []byte(bin), 0o755)
	opts := testOpts()
	if _, err := runLocalInstall("helper", opts); err != nil {
		t.Fatalf("runLocalInstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d, ".claude", "agents", "helper.md")); !os.IsNotExist(err) {
		t.Fatal("a stale agent file was installed next to a newer binary")
	}
	if !strings.Contains(opts.Err.(*bytes.Buffer).String(), "v0.3.0") {
		t.Fatalf("missing skip note: %s", opts.Err)
	}
	if e, _ := loadEntry(t, "helper"); e.AgentFile != "" {
		t.Fatalf("stale file tracked: %+v", e)
	}
}
