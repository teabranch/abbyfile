package builtins

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/teabranch/abbyfile/pkg/tools"
)

// WriteFileTool returns a tool definition for writing file contents.
func WriteFileTool() *tools.Definition {
	return tools.BuiltinTool(
		"write_file",
		"Write content to a file at the given absolute path. Creates parent directories if needed.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Absolute path to the file to write",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "The content to write to the file",
				},
			},
			"required": []string{"path", "content"},
		},
		handleWriteFile,
	).WithAnnotations(&tools.Annotations{
		DestructiveHint: tools.BoolPtr(false),
		Title:           "Write File",
	})
}

func handleWriteFile(input map[string]any) (string, error) {
	path, ok := input["path"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: path")
	}
	content, ok := input["content"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: content")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating directories: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(content), path), nil
}
