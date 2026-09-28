package builtins

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/sandbox"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// manyFiles creates n small .go files across 10 subdirectories under dir,
// each containing the string "needle", so a walk that ignored ctx would
// have many iterations to get through before finishing.
func manyFiles(t *testing.T, dir string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		sub := filepath.Join(dir, "d"+strconv.Itoa(i%10))
		writeFile(t, filepath.Join(sub, "f"+strconv.Itoa(i)+".go"), "package d\n// needle\n")
	}
}

// expiredCtx returns a context carrying root's sandbox whose deadline has
// already passed.
func expiredCtx(t *testing.T, root string, cfg sandbox.Config) context.Context {
	t.Helper()
	base := sandboxCtx(t, root, cfg)
	ctx, cancel := context.WithDeadline(base, time.Now().Add(-time.Second))
	t.Cleanup(cancel)
	return ctx
}

// cancelledCtx returns a context carrying root's sandbox that is already
// cancelled.
func cancelledCtx(t *testing.T, root string, cfg sandbox.Config) context.Context {
	t.Helper()
	base := sandboxCtx(t, root, cfg)
	ctx, cancel := context.WithCancel(base)
	cancel()
	return ctx
}

// Finding 2: glob_files' ** walk must stop on an expired ctx instead of
// returning results from a tree of a few hundred files.
func TestGlobFiles_DoubleStar_RespectsExpiredCtx(t *testing.T) {
	root := realTempDir(t)
	manyFiles(t, root, 300)
	_, err := handleGlobFiles(expiredCtx(t, root, sandbox.Config{}), map[string]any{"pattern": "**/*.go"})
	if err == nil {
		t.Fatal("expired ctx must stop the ** walk with an error, not results")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want to wrap context.DeadlineExceeded", err)
	}
}

// Finding 2: glob_files' non-** hit loop must also check ctx.
func TestGlobFiles_NonDoubleStar_RespectsCancelledCtx(t *testing.T) {
	root := realTempDir(t)
	manyFiles(t, root, 300)
	ctx := cancelledCtx(t, root, sandbox.Config{})
	_, err := handleGlobFiles(ctx, map[string]any{"pattern": "d0/*.go"})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want to wrap context.Canceled", err)
	}
}

// Finding 2: grep_search's directory walk must stop on an expired ctx
// instead of returning results from a tree of a few hundred files.
func TestGrepSearch_Dir_RespectsExpiredCtx(t *testing.T) {
	root := realTempDir(t)
	manyFiles(t, root, 300)
	_, err := handleGrepSearch(expiredCtx(t, root, sandbox.Config{}), map[string]any{"pattern": "needle"})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want to wrap context.DeadlineExceeded", err)
	}
}

// Finding 2: grep_search's single-file scan loop (searchFile) must also
// check ctx periodically, not just once per WalkDir entry.
func TestGrepSearch_SingleFile_RespectsExpiredCtx(t *testing.T) {
	root := realTempDir(t)
	big := strings.Repeat("no match here\n", 5000) + "needle\n"
	writeFile(t, filepath.Join(root, "big.txt"), big)
	_, err := handleGrepSearch(expiredCtx(t, root, sandbox.Config{}), map[string]any{"pattern": "needle", "path": "big.txt"})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want to wrap context.DeadlineExceeded", err)
	}
}

// Finding 2, executor level: grep run under an Executor with a 1ns timeout
// must report the standard "timed out" message, proving handleGrepSearch's
// ctx checks feed back into runBuiltin's DeadlineExceeded mapping.
func TestGrepSearch_ExecutorTimeoutReportsTimedOut(t *testing.T) {
	root := realTempDir(t)
	manyFiles(t, root, 300)
	sb, err := sandbox.New(sandbox.Config{}, root)
	if err != nil {
		t.Fatal(err)
	}
	ex := tools.NewExecutor(time.Nanosecond, nil, tools.WithSandbox(sb))
	_, err = ex.RunRaw(context.Background(), GrepSearchTool(), map[string]any{"pattern": "needle"})
	if err == nil || !strings.Contains(err.Error(), `tool "grep_search" timed out after 1ns`) {
		t.Fatalf("err = %v, want a timed-out message", err)
	}
}
