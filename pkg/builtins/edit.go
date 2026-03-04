package builtins

import (
	"fmt"
	"os"
	"strings"

	"github.com/teabranch/agentfile/pkg/tools"
)

// EditFileTool returns a tool definition for find-and-replace editing.
func EditFileTool() *tools.Definition {
	return tools.BuiltinTool(
		"edit_file",
		"Edit a file by replacing an exact string match with new content",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Absolute path to the file to edit",
				},
				"old_string": map[string]any{
					"type":        "string",
					"description": "The exact string to find and replace",
				},
				"new_string": map[string]any{
					"type":        "string",
					"description": "The replacement string",
				},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
		handleEditFile,
	).WithAnnotations(&tools.Annotations{
		DestructiveHint: tools.BoolPtr(false),
		Title:           "Edit File",
	})
}

func handleEditFile(input map[string]any) (string, error) {
	path, ok := input["path"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: path")
	}
	oldStr, ok := input["old_string"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: old_string")
	}
	newStr, ok := input["new_string"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: new_string")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}

	content := string(data)
	count := strings.Count(content, oldStr)
	if count == 0 {
		return "", fmt.Errorf("old_string not found in file")
	}
	if count > 1 {
		return "", fmt.Errorf("old_string found %d times, must be unique", count)
	}

	content = strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}
	return fmt.Sprintf("Edited %s", path), nil
}
