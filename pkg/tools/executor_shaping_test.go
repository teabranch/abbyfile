package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecutor_ShapesBuiltinOutput(t *testing.T) {
	// Builtin tool that returns 50 lines.
	var big strings.Builder
	for i := 0; i < 50; i++ {
		big.WriteString("line\n")
	}
	def := BuiltinTool("bigcat", "returns many lines", map[string]any{"type": "object"},
		func(input map[string]any) (string, error) { return big.String(), nil })

	budget := ContextBudget{MaxOutputLines: 10, MaxOutputBytes: 100000, OnOverflow: OverflowHeadTail, HeadLines: 2, TailLines: 2}
	e := NewExecutor(time.Second, nil, WithContextBudget(budget, nil))

	out, err := e.Run(context.Background(), def, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "elided") {
		t.Fatalf("expected shaped output with elision marker, got:\n%s", out)
	}
	if got := strings.Count(out, "\n"); got > 10 {
		t.Fatalf("shaped output has %d newlines, expected <= 10", got)
	}
}

func TestExecutor_ShapesCLIErrorOutput(t *testing.T) {
	def := &Definition{
		Name:    "failbig",
		Command: "sh",
		Args:    []string{"-c", "for i in $(seq 1 50); do echo err line $i >&2; done; exit 1"},
	}

	budget := ContextBudget{MaxOutputLines: 5, MaxOutputBytes: 100000, OnOverflow: OverflowHeadTail, HeadLines: 2, TailLines: 2}
	e := NewExecutor(time.Second, nil, WithContextBudget(budget, nil))

	_, err := e.Run(context.Background(), def, nil)
	if err == nil {
		t.Fatalf("expected error from failing CLI tool")
	}
	if !strings.Contains(err.Error(), "elided") {
		t.Fatalf("expected shaped error with elision marker, got: %v", err)
	}
}

func TestExecutor_NoBudget_LeavesOutputUnchanged(t *testing.T) {
	def := BuiltinTool("echo3", "3 lines", map[string]any{"type": "object"},
		func(input map[string]any) (string, error) { return "a\nb\nc", nil })
	e := NewExecutor(time.Second, nil) // no WithContextBudget
	out, err := e.Run(context.Background(), def, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "a\nb\nc" {
		t.Fatalf("output changed without budget: %q", out)
	}
}
