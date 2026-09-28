package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

func testSandbox(t *testing.T, cfg sandbox.Config) *sandbox.Sandbox {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := sandbox.New(cfg, root)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestExecutorPrefersHandlerCtx(t *testing.T) {
	def := &Definition{
		Name: "t", Builtin: true,
		Handler:    func(map[string]any) (string, error) { return "legacy", nil },
		HandlerCtx: func(context.Context, map[string]any) (string, error) { return "ctx", nil },
	}
	got, err := NewExecutor(time.Second, nil).RunRaw(context.Background(), def, nil)
	if err != nil || got != "ctx" {
		t.Fatalf("RunRaw = %q, %v; want ctx", got, err)
	}
}

func TestExecutorLegacyHandlerStillRuns(t *testing.T) {
	def := BuiltinTool("t", "d", nil, func(map[string]any) (string, error) { return "legacy", nil })
	got, err := NewExecutor(time.Second, nil).RunRaw(context.Background(), def, nil)
	if err != nil || got != "legacy" {
		t.Fatalf("RunRaw = %q, %v", got, err)
	}
}

func TestExecutorInjectsSandbox(t *testing.T) {
	sb := testSandbox(t, sandbox.Config{MaxCommandTimeout: 7 * time.Second})
	def := BuiltinToolCtx("t", "d", nil, func(ctx context.Context, _ map[string]any) (string, error) {
		return sandbox.FromContext(ctx).Config().MaxCommandTimeout.String(), nil
	})
	got, err := NewExecutor(time.Second, nil, WithSandbox(sb)).RunRaw(context.Background(), def, nil)
	if err != nil || got != "7s" {
		t.Fatalf("handler saw %q, %v; want 7s", got, err)
	}
}

func TestExecutorTimeoutAppliesToHandlerCtx(t *testing.T) {
	def := BuiltinToolCtx("slow", "d", nil, func(ctx context.Context, _ map[string]any) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	_, err := NewExecutor(50*time.Millisecond, nil).RunRaw(context.Background(), def, nil)
	if err == nil || !strings.Contains(err.Error(), `tool "slow" timed out after 50ms`) {
		t.Fatalf("err = %v", err)
	}
}

func TestExecutorCommandTimeoutExtendsOuterLimit(t *testing.T) {
	sb := testSandbox(t, sandbox.Config{MaxCommandTimeout: 2 * time.Second})
	def := BuiltinToolCtx("run_command", "d", nil, func(ctx context.Context, _ map[string]any) (string, error) {
		dl, ok := ctx.Deadline()
		if !ok {
			return "", fmt.Errorf("no deadline")
		}
		return fmt.Sprint(time.Until(dl) > time.Second), nil
	})
	def.UsesCommandTimeout = true
	got, err := NewExecutor(50*time.Millisecond, nil, WithSandbox(sb)).RunRaw(context.Background(), def, nil)
	if err != nil || got != "true" {
		t.Fatalf("outer limit should be max(50ms, 2s); got %q, %v", got, err)
	}
}

func TestExecutorInjectsOutputLimit(t *testing.T) {
	probe := func(ctx context.Context, _ map[string]any) (string, error) { return fmt.Sprint(OutputLimit(ctx)), nil }
	def := BuiltinToolCtx("t", "d", nil, probe)
	def.Policy = &CommandPolicy{MaxOutputBytes: 5}
	if got, _ := NewExecutor(time.Second, nil).RunRaw(context.Background(), def, nil); got != "5" {
		t.Errorf("per-tool policy limit = %s, want 5", got)
	}
	plain := BuiltinToolCtx("t", "d", nil, probe)
	if got, _ := NewExecutor(time.Second, nil).RunRaw(context.Background(), plain, nil); got != fmt.Sprint(DefaultMaxOutputBytes) {
		t.Errorf("no policy limit = %s, want default", got)
	}
	zero := BuiltinToolCtx("t", "d", nil, probe)
	ex := NewExecutor(time.Second, nil, WithDefaultPolicy(&CommandPolicy{AllowedPrefixes: []string{"go "}}))
	if got, _ := ex.RunRaw(context.Background(), zero, nil); got != fmt.Sprint(DefaultMaxOutputBytes) {
		t.Errorf("policy with MaxOutputBytes 0 must use default, got %s", got)
	}
}

func TestExecutorCLIBoundedCapture(t *testing.T) {
	def := &Definition{
		Name: "big", Command: "sh",
		Args:   []string{"-c", "head -c 100000 /dev/zero | tr '\\0' x"},
		Policy: &CommandPolicy{MaxOutputBytes: 1000},
	}
	got, err := NewExecutor(5*time.Second, nil).RunRaw(context.Background(), def, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "[output truncated at 1000 bytes]") || len(got) > 1100 {
		t.Errorf("len=%d tail=%q", len(got), got[max(0, len(got)-40):])
	}
}

func TestBuiltinToolCtxSetsLegacyHandler(t *testing.T) {
	def := BuiltinToolCtx("t", "d", nil, func(ctx context.Context, _ map[string]any) (string, error) {
		if sandbox.FromContext(ctx) == nil {
			return "", fmt.Errorf("no sandbox")
		}
		return "ok", nil
	})
	if !def.Builtin || def.Handler == nil {
		t.Fatal("BuiltinToolCtx must set Builtin and a Handler wrapper")
	}
	if got, err := def.Handler(nil); err != nil || got != "ok" {
		t.Fatalf("Handler wrapper = %q, %v", got, err)
	}
}
