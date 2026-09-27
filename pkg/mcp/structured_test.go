package mcp

import (
	"strings"
	"testing"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func textOf(t *testing.T, r *gomcp.CallToolResult) string {
	t.Helper()
	if len(r.Content) != 1 {
		t.Fatalf("content len = %d, want 1", len(r.Content))
	}
	tc, ok := r.Content[0].(*gomcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want *TextContent", r.Content[0])
	}
	return tc.Text
}

func TestStructuredOutputObject(t *testing.T) {
	r := structuredResult("t", `{"n":1}`, 0)
	if r.IsError {
		t.Fatalf("unexpected error: %s", textOf(t, r))
	}
	m, ok := r.StructuredContent.(map[string]any)
	if !ok || m["n"] != float64(1) {
		t.Fatalf("structured = %#v", r.StructuredContent)
	}
	if textOf(t, r) != `{"n":1}` {
		t.Fatalf("text = %q", textOf(t, r))
	}
}

func TestStructuredOutputNonObjectJSON(t *testing.T) { // Review Focus #2
	for _, raw := range []string{`[1,2]`, `"hi"`, `42`, `true`} {
		r := structuredResult("t", raw, 0)
		if r.IsError {
			t.Errorf("%s: unexpected error %s", raw, textOf(t, r))
		}
		if r.StructuredContent == nil {
			t.Errorf("%s: structured content nil", raw)
		}
	}
}

func TestStructuredOutputTrailingNewline(t *testing.T) { // Review Focus #3
	r := structuredResult("t", "{\"n\":1}\n\n", 0)
	if r.IsError {
		t.Fatalf("unexpected error: %s", textOf(t, r))
	}
	if textOf(t, r) != `{"n":1}` {
		t.Fatalf("text = %q, want trimmed", textOf(t, r))
	}
}

func TestStructuredOutputNonJSON(t *testing.T) {
	r := structuredResult("t", "not json", 0)
	if !r.IsError || !strings.Contains(textOf(t, r), "non-JSON") {
		t.Fatalf("want isError non-JSON, got %+v", r)
	}
	if !strings.Contains(textOf(t, r), "if stdout was empty, stderr was used as output") {
		t.Fatalf("want stderr-fallback hint, got %+v", r)
	}
	if r.StructuredContent != nil {
		t.Fatal("structured content must be nil on error")
	}
}

func TestStructuredOutputNull(t *testing.T) { // F6: JSON null is not usable structured content
	r := structuredResult("t", "null", 0)
	if !r.IsError {
		t.Fatalf("want isError for JSON null, got %+v", r)
	}
	if !strings.Contains(textOf(t, r), `tool "t" declared outputSchema but produced JSON null`) {
		t.Fatalf("error text = %q", textOf(t, r))
	}
	if r.StructuredContent != nil {
		t.Fatal("structured content must be nil on error")
	}
}

func TestStructuredOutputOverCap(t *testing.T) {
	r := structuredResult("t", `{"s":"`+strings.Repeat("x", 100)+`"}`, 50)
	if !r.IsError || !strings.Contains(textOf(t, r), "50-byte cap") {
		t.Fatalf("want isError over cap, got %+v", r)
	}
	if !strings.Contains(textOf(t, r), "tools.ContextBudget.PerTool") {
		t.Fatalf("want ContextBudget hint, got %+v", r)
	}
}
