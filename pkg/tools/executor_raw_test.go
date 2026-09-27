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

func TestResultSizeHint(t *testing.T) { // Review Focus #4 (runtime side)
	if n, req := NewExecutor(0, nil).ResultSizeHint("x"); n != 0 || req {
		t.Fatalf("no budget: got (%d,%v), want (0,false)", n, req)
	}
	var nilExec *Executor
	if n, req := nilExec.ResultSizeHint("x"); n != 0 || req {
		t.Fatalf("nil executor: got (%d,%v), want (0,false)", n, req)
	}
	b := DefaultContextBudget()
	b.PerTool = map[string]ContextBudget{
		"big":     {InlineLarge: true, MaxOutputBytes: 300000},
		"inherit": {InlineLarge: true},
		"huge":    {InlineLarge: true, MaxOutputBytes: 900000},
		"plain":   {MaxOutputBytes: 300000},
	}
	e := NewExecutor(0, nil, WithContextBudget(b, nil))
	cases := []struct {
		tool    string
		want    int
		wantReq bool
	}{
		{"big", 300000, true},
		{"inherit", 262144, true},
		{"huge", MaxResultSizeCharsCeiling, true}, // clamped
		{"plain", 0, false},
		{"absent", 0, false},
	}
	for _, c := range cases {
		if n, req := e.ResultSizeHint(c.tool); n != c.want || req != c.wantReq {
			t.Errorf("%s: got (%d,%v), want (%d,%v)", c.tool, n, req, c.want, c.wantReq)
		}
	}

	unl := DefaultContextBudget()
	unl.MaxOutputBytes = 0 // e.g. a runtime `config set context_budget.max_output_bytes 0`
	unl.PerTool = map[string]ContextBudget{"t": {InlineLarge: true}}
	if n, req := NewExecutor(0, nil, WithContextBudget(unl, nil)).ResultSizeHint("t"); n != 0 || !req {
		t.Fatalf("unlimited cap: got (%d,%v), want (0,true)", n, req)
	}
}
