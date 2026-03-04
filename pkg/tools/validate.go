package tools

import (
	"fmt"
	"strings"
)

// ValidateInput checks that input satisfies the tool's InputSchema.
// It verifies required fields are present and property types match.
// Returns nil if the schema is nil or input is valid.
func (d *Definition) ValidateInput(input map[string]any) error {
	if d.InputSchema == nil {
		return nil
	}

	schema, ok := d.InputSchema.(map[string]any)
	if !ok {
		return nil
	}

	if input == nil {
		input = map[string]any{}
	}

	var errs []string

	// Check required fields.
	if req, ok := schema["required"]; ok {
		for _, key := range toStringSlice(req) {
			if _, exists := input[key]; !exists {
				errs = append(errs, fmt.Sprintf("missing required field %q", key))
			}
		}
	}

	// Check property types.
	if props, ok := schema["properties"]; ok {
		if propsMap, ok := props.(map[string]any); ok {
			for key, propDef := range propsMap {
				val, exists := input[key]
				if !exists {
					continue
				}

				propSchema, ok := propDef.(map[string]any)
				if !ok {
					continue
				}

				declaredType, ok := propSchema["type"].(string)
				if !ok {
					continue
				}

				if err := checkType(key, declaredType, val); err != "" {
					errs = append(errs, err)
				}
			}
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(errs, "; "))
}

// checkType returns an error string if val doesn't match the declared JSON Schema type.
func checkType(field, declaredType string, val any) string {
	var ok bool
	var expected string

	switch declaredType {
	case "string":
		_, ok = val.(string)
		expected = "string"
	case "number", "integer":
		_, ok = val.(float64)
		expected = "number"
	case "boolean":
		_, ok = val.(bool)
		expected = "boolean"
	case "array":
		_, ok = val.([]any)
		expected = "array"
	case "object":
		_, ok = val.(map[string]any)
		expected = "object"
	default:
		return ""
	}

	if !ok {
		return fmt.Sprintf("field %q: expected %s, got %s", field, expected, goTypeLabel(val))
	}
	return ""
}

// toStringSlice converts a required field value to []string.
// Handles both []string (Go-constructed schemas) and []any (JSON-unmarshalled schemas).
func toStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

// goTypeLabel returns a human-readable label for val's Go type.
func goTypeLabel(val any) string {
	switch val.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", val)
	}
}
