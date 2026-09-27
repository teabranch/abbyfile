//go:build integration

package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"testing"
	"time"
)

// TestStdoutIsOnlyJSONRPC drives serve-mcp with raw legacy-era JSON-RPC and
// asserts every stdout line is a JSON-RPC 2.0 message (logs must go to stderr).
func TestStdoutIsOnlyJSONRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath, "serve-mcp")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	msgs := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"raw","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"does_not_exist","arguments":{}}}`,
	}
	for _, m := range msgs {
		if _, err := io.WriteString(stdin, m+"\n"); err != nil {
			t.Fatal(err)
		}
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	seen := map[float64]bool{}
	for sc.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			t.Fatalf("non-JSON on stdout: %q", sc.Text())
		}
		if msg["jsonrpc"] != "2.0" {
			t.Fatalf("stdout message without jsonrpc 2.0: %q", sc.Text())
		}
		if id, ok := msg["id"].(float64); ok {
			seen[id] = true
		}
		if seen[1] && seen[2] && seen[3] {
			return
		}
	}
	t.Fatalf("stream ended before responses 1-3; seen=%v err=%v", seen, sc.Err())
}
