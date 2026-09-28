package builtins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/teabranch/abbyfile/pkg/sandbox"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// WriteFileTool returns a tool definition for writing file contents.
func WriteFileTool() *tools.Definition {
	return tools.BuiltinToolCtx(
		"write_file",
		"Write content to a file, creating parent directories if needed. Overwrites existing files. Paths outside the allowed directories are refused.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the file to write",
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
		DestructiveHint: tools.BoolPtr(true),
		Title:           "Write File",
	})
}

func handleWriteFile(ctx context.Context, input map[string]any) (string, error) {
	path, ok := input["path"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: path")
	}
	content, ok := input["content"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: content")
	}
	// Resolve before MkdirAll: a denied write must not create directories.
	resolved, err := sandbox.FromContext(ctx).Resolve(path, sandbox.Write)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return "", fmt.Errorf("creating directories: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(content), path), nil
}
