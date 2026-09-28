//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer safe for one writer (os/exec's stderr copy
// goroutine) and a concurrent reader (the test, while the process runs).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// sendRawJSONRPC spawns bin's `serve-mcp` with HOME isolated to a fresh temp
// dir, writes each message in msgs to stdin (one JSON-RPC message per
// line), and scans stdout until a response has arrived for every id in
// wantIDs. Every line read from stdout must be a valid JSON-RPC 2.0 message
// — per spec A6, a stdio server must never write logs or anything else to
// stdout — so a non-JSON-RPC line fails the test immediately rather than
// being skipped. It returns the parsed response object for each id in
// wantIDs (keyed by id) and everything the process wrote to stderr, so
// callers can assert on both.
func sendRawJSONRPC(t *testing.T, bin string, msgs []string, wantIDs []float64) (map[float64]map[string]any, string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "serve-mcp")
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stderr syncBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve-mcp: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	for _, m := range msgs {
		if _, err := io.WriteString(stdin, m+"\n"); err != nil {
			t.Fatalf("write stdin: %v", err)
		}
	}

	want := make(map[float64]bool, len(wantIDs))
	for _, id := range wantIDs {
		want[id] = true
	}
	responses := make(map[float64]map[string]any, len(wantIDs))

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			t.Fatalf("non-JSON on stdout: %q", sc.Text())
		}
		if msg["jsonrpc"] != "2.0" {
			t.Fatalf("stdout message without jsonrpc 2.0: %q", sc.Text())
		}
		if id, ok := msg["id"].(float64); ok && want[id] {
			responses[id] = msg
			if len(responses) == len(want) {
				return responses, stderr.String()
			}
		}
	}
	t.Fatalf("stream ended before all responses seen; got=%v scan err=%v stderr=%s", responses, sc.Err(), stderr.String())
	return nil, ""
}

// TestStdoutIsOnlyJSONRPC drives serve-mcp with raw legacy-era (2025-11-25)
// JSON-RPC — an initialize/initialized handshake, then tools/list and a
// call to a nonexistent tool — and asserts every stdout line is valid
// JSON-RPC 2.0.
func TestStdoutIsOnlyJSONRPC(t *testing.T) {
	msgs := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"raw","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"does_not_exist","arguments":{}}}`,
	}
	responses, _ := sendRawJSONRPC(t, binaryPath, msgs, []float64{1, 2, 3})

	if _, ok := responses[1]["result"]; !ok {
		t.Errorf("id 1 (initialize) has no result: %v", responses[1])
	}
	if _, ok := responses[2]["result"]; !ok {
		t.Errorf("id 2 (tools/list) has no result: %v", responses[2])
	}
}

// TestStdoutIsOnlyJSONRPCModernEra drives serve-mcp with raw 2026-07-28
// JSON-RPC and NO initialize/initialized handshake at all: per SEP-2575,
// the modern protocol carries protocol version, client info, and client
// capabilities in each call's `_meta`, and server/discover replaces
// initialize. It asserts every stdout line is valid JSON-RPC 2.0, both
// responses arrive, and tools/list succeeds with a non-empty tool list.
func TestStdoutIsOnlyJSONRPCModernEra(t *testing.T) {
	const meta = `"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
		`"io.modelcontextprotocol/clientInfo":{"name":"raw","version":"1"},` +
		`"io.modelcontextprotocol/clientCapabilities":{}}`
	msgs := []string{
		`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{` + meta + `}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{` + meta + `}}`,
	}
	responses, _ := sendRawJSONRPC(t, binaryPath, msgs, []float64{1, 2})

	if errObj, ok := responses[1]["error"]; ok {
		t.Fatalf("id 1 (server/discover) returned error: %v", errObj)
	}
	if _, ok := responses[1]["result"]; !ok {
		t.Fatalf("id 1 (server/discover) has no result: %v", responses[1])
	}

	if errObj, ok := responses[2]["error"]; ok {
		t.Fatalf("id 2 (tools/list) returned error: %v", errObj)
	}
	result, ok := responses[2]["result"].(map[string]any)
	if !ok {
		t.Fatalf("id 2 (tools/list) has no result: %v", responses[2])
	}
	toolsList, ok := result["tools"].([]any)
	if !ok || len(toolsList) == 0 {
		t.Fatalf("id 2 result.tools is empty or missing: %v", result)
	}
}

// TestStdoutIsOnlyJSONRPCWithSandboxWarnings drives serve-mcp for an agent
// whose sandbox: block (bash: unrestricted) triggers a startup warning
// (sandbox.New's Warnings(), logged via slog to stderr in buildSandbox).
// Finding 8: that warning must still go to stderr, never stdout — a stdio
// server's stdout is JSON-RPC only, per spec A6 — and the test asserts the
// warning actually reached stderr, so it cannot pass vacuously.
func TestStdoutIsOnlyJSONRPCWithSandboxWarnings(t *testing.T) {
	bin := buildAgentWithSandbox(t, "warn-sandbox-agent", "  bash: unrestricted")
	msgs := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"raw","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}
	responses, stderr := sendRawJSONRPC(t, bin, msgs, []float64{1, 2})

	if _, ok := responses[1]["result"]; !ok {
		t.Errorf("id 1 (initialize) has no result: %v", responses[1])
	}
	if _, ok := responses[2]["result"]; !ok {
		t.Errorf("id 2 (tools/list) has no result: %v", responses[2])
	}
	if !strings.Contains(stderr, "unrestricted") {
		t.Errorf("expected the sandbox.bash: unrestricted warning on stderr, got: %q", stderr)
	}
}
