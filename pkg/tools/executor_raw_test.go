package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func bigBuiltin(n int) *Definition {
	return BuiltinTool("big", "big", map[string]any{"type": "object"},
		func(map[string]any) (string, error) { return strings.Repeat("x", n), nil })
}

func TestRunRawSkipsShaping(t *testing.T) {
	b := DefaultContextBudget()
	b.MaxOutputBytes = 10
	e := NewExecutor(time.Second, nil, WithContextBudget(b, nil))
	raw, err := e.RunRaw(context.Background(), bigBuiltin(100), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 100 {
		t.Fatalf("RunRaw len = %d, want 100 (unshaped)", len(raw))
	}
	shaped, _ := e.Run(context.Background(), bigBuiltin(100), nil)
	if len(shaped) > 10 {
		t.Fatalf("Run len = %d, want <= 10 (shaped)", len(shaped))
	}
	if e.Shape("big", raw) != shaped {
		t.Fatal("Run must equal Shape(RunRaw)")
	}
}

func TestRunRawTrimsCLIOutput(t *testing.T) {
	e := NewExecutor(5*time.Second, nil)
	def := CLI("echo", "echo", "echo")
	def.Args = []string{"hello"}
	raw, err := e.RunRaw(context.Background(), def, nil)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "hello" {
		t.Fatalf("raw = %q, want %q", raw, "hello")
	}
}

func TestMaxOutputBytes(t *testing.T) {
	if got := NewExecutor(0, nil).MaxOutputBytes("x"); got != 0 {
		t.Fatalf("no budget: got %d, want 0", got)
	}
	b := DefaultContextBudget()
	b.PerTool = map[string]ContextBudget{"small": {MaxOutputBytes: 512}}
	e := NewExecutor(0, nil, WithContextBudget(b, nil))
	if got := e.MaxOutputBytes("small"); got != 512 {
		t.Fatalf("per-tool: got %d, want 512", got)
	}
	if got := e.MaxOutputBytes("other"); got != 262144 {
		t.Fatalf("base: got %d, want 262144", got)
	}
}
