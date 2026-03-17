package builtins

import (
	"fmt"
	"os"

	"github.com/teabranch/abbyfile/pkg/tools"
)

// ReadFileTool returns a tool definition for reading file contents.
func ReadFileTool() *tools.Definition {
	return tools.BuiltinTool(
		"read_file",
		"Read the contents of a file at the given absolute path",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Absolute path to the file to read",
				},
			},
			"required": []string{"path"},
		},
		handleReadFile,
	).WithAnnotations(&tools.Annotations{
		ReadOnlyHint:   true,
		IdempotentHint: true,
		Title:          "Read File",
	})
}

func handleReadFile(input map[string]any) (string, error) {
	path, ok := input["path"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	return string(data), nil
}
