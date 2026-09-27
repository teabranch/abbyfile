package mcp_test

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/teabranch/abbyfile/pkg/tools"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

// wireRegistry is a fixed registry covering annotations, output schema, and
// a schema without explicit type, so SDK serialization changes show up in the diff.
func wireRegistry() *tools.Registry {
	r := tools.NewRegistry()
	_ = r.Register(tools.BuiltinTool("alpha", "Alpha tool",
		map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
		func(map[string]any) (string, error) { return "ok", nil },
	).WithAnnotations(&tools.Annotations{ReadOnlyHint: true, IdempotentHint: true, Title: "Alpha"}))
	beta := tools.BuiltinTool("beta", "Beta tool",
		map[string]any{"type": "object"},
		func(map[string]any) (string, error) { return `{"n":1}`, nil },
	)
	beta.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{"n": map[string]any{"type": "integer"}}}
	_ = r.Register(beta)
	return r
}

func TestWireToolsListGolden(t *testing.T) {
	session, _ := startBridge(t, wireRegistry())
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got, err := json.MarshalIndent(res.Tools, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join("testdata", "tools_list.golden.json")
	if *updateGolden {
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(append(got, '\n')) != string(want) {
		t.Errorf("tools/list wire changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestInputSchemaWithoutTypeDoesNotPanic covers input schemas whose top-level
// "type" is missing, null, a non-"object" scalar, or a union — go-sdk v1.8.0's
// AddTool panics unless the decoded top-level "type" is exactly "object". Each
// case runs in its own subtest with its own registry/bridge, since a panic in
// AddTool kills the whole server goroutine (and would otherwise take down
// unrelated subtests sharing the same registry).
func TestInputSchemaWithoutTypeDoesNotPanic(t *testing.T) {
	cases := []struct {
		name   string
		schema map[string]any
	}{
		{
			name:   "type absent",
			schema: map[string]any{"properties": map[string]any{"args": map[string]any{"type": "string"}}},
		},
		{
			name:   "type null",
			schema: map[string]any{"type": nil, "properties": map[string]any{"args": map[string]any{"type": "string"}}},
		},
		{
			name:   "type string",
			schema: map[string]any{"type": "string", "properties": map[string]any{"args": map[string]any{"type": "string"}}},
		},
		{
			name:   "type union",
			schema: map[string]any{"type": []any{"object", "null"}, "properties": map[string]any{"args": map[string]any{"type": "string"}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tools.NewRegistry()
			_ = r.Register(tools.CLI("notype", "echo", "schema missing type"))
			def := r.Get("notype")
			def.InputSchema = tc.schema

			session, _ := startBridge(t, r)
			res, err := session.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatalf("list tools: %v", err)
			}
			for _, tool := range res.Tools {
				if tool.Name != "notype" {
					continue
				}
				raw, _ := json.Marshal(tool.InputSchema)
				var m map[string]any
				_ = json.Unmarshal(raw, &m)
				if m["type"] != "object" {
					t.Fatalf("inputSchema.type = %v, want object", m["type"])
				}
				if _, ok := m["properties"]; !ok {
					t.Fatal("properties dropped")
				}
				return
			}
			t.Fatal("notype tool not listed")
		})
	}
}
