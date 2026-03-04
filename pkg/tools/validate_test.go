package tools

import (
	"strings"
	"testing"
)

func TestValidateInput(t *testing.T) {
	tests := []struct {
		name      string
		schema    any
		input     map[string]any
		wantErr   bool
		errSubstr []string // all must appear in the error message
	}{
		{
			name:   "nil schema passes",
			schema: nil,
			input:  map[string]any{"key": "val"},
		},
		{
			name:   "non-map schema passes",
			schema: "not a map",
			input:  map[string]any{"key": "val"},
		},
		{
			name: "no required no properties passes",
			schema: map[string]any{
				"type": "object",
			},
			input: map[string]any{"anything": 42.0},
		},
		{
			name: "required field present passes",
			schema: map[string]any{
				"type":     "object",
				"required": []any{"key"},
				"properties": map[string]any{
					"key": map[string]any{"type": "string"},
				},
			},
			input: map[string]any{"key": "hello"},
		},
		{
			name: "missing required field errors",
			schema: map[string]any{
				"type":     "object",
				"required": []any{"key"},
				"properties": map[string]any{
					"key": map[string]any{"type": "string"},
				},
			},
			input:     map[string]any{},
			wantErr:   true,
			errSubstr: []string{`missing required field "key"`},
		},
		{
			name: "nil input with required field errors",
			schema: map[string]any{
				"type":     "object",
				"required": []any{"key"},
				"properties": map[string]any{
					"key": map[string]any{"type": "string"},
				},
			},
			input:     nil,
			wantErr:   true,
			errSubstr: []string{`missing required field "key"`},
		},
		{
			name: "correct string type passes",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			input: map[string]any{"name": "alice"},
		},
		{
			name: "wrong type string expected got number",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			},
			input:     map[string]any{"path": 123.0},
			wantErr:   true,
			errSubstr: []string{`field "path"`, "expected string", "got number"},
		},
		{
			name: "correct number type passes",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"count": map[string]any{"type": "number"},
				},
			},
			input: map[string]any{"count": 42.0},
		},
		{
			name: "correct integer type passes as float64",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"count": map[string]any{"type": "integer"},
				},
			},
			input: map[string]any{"count": 5.0},
		},
		{
			name: "correct boolean type passes",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"flag": map[string]any{"type": "boolean"},
				},
			},
			input: map[string]any{"flag": true},
		},
		{
			name: "correct array type passes",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items": map[string]any{"type": "array"},
				},
			},
			input: map[string]any{"items": []any{1.0, 2.0}},
		},
		{
			name: "correct object type passes",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"meta": map[string]any{"type": "object"},
				},
			},
			input: map[string]any{"meta": map[string]any{"k": "v"}},
		},
		{
			name: "wrong type boolean expected got string",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"flag": map[string]any{"type": "boolean"},
				},
			},
			input:     map[string]any{"flag": "true"},
			wantErr:   true,
			errSubstr: []string{`field "flag"`, "expected boolean", "got string"},
		},
		{
			name: "multiple errors combined",
			schema: map[string]any{
				"type":     "object",
				"required": []any{"a", "b"},
				"properties": map[string]any{
					"a": map[string]any{"type": "string"},
					"b": map[string]any{"type": "string"},
					"c": map[string]any{"type": "number"},
				},
			},
			input:   map[string]any{"c": "not a number"},
			wantErr: true,
			errSubstr: []string{
				`missing required field "a"`,
				`missing required field "b"`,
				`field "c"`,
			},
		},
		{
			name: "required as []string (Go-constructed schema)",
			schema: map[string]any{
				"type":     "object",
				"required": []string{"key"},
				"properties": map[string]any{
					"key": map[string]any{"type": "string"},
				},
			},
			input:     map[string]any{},
			wantErr:   true,
			errSubstr: []string{`missing required field "key"`},
		},
		{
			name: "unknown type in schema skips check",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x": map[string]any{"type": "custom"},
				},
			},
			input: map[string]any{"x": "anything"},
		},
		{
			name: "extra fields not in schema pass",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"a": map[string]any{"type": "string"},
				},
			},
			input: map[string]any{"a": "ok", "extra": 99.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := &Definition{
				Name:        "test_tool",
				InputSchema: tt.schema,
			}

			err := def.ValidateInput(tt.input)

			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if err != nil {
				for _, sub := range tt.errSubstr {
					if !strings.Contains(err.Error(), sub) {
						t.Errorf("error %q does not contain %q", err.Error(), sub)
					}
				}
			}
		})
	}
}
