package builtins

import (
	"context"
	"fmt"
	"os"

	"github.com/teabranch/abbyfile/pkg/sandbox"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// ReadFileTool returns a tool definition for reading file contents.
func ReadFileTool() *tools.Definition {
	return tools.BuiltinToolCtx(
		"read_file",
		"Read the contents of a file. Paths may be relative to the working directory; paths outside the allowed directories are refused.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the file to read",
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

func handleReadFile(ctx context.Context, input map[string]any) (string, error) {
	path, ok := input["path"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: path")
	}
	resolved, err := sandbox.FromContext(ctx).Resolve(path, sandbox.Read)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	return string(data), nil
}
