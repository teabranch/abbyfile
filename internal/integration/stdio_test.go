//go:build integration

package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

// sendRawJSONRPC spawns `serve-mcp` with HOME isolated to a fresh temp dir,
// writes each message in msgs to stdin (one JSON-RPC message per line), and
// scans stdout until a response has arrived for every id in wantIDs. Every
// line read from stdout must be a valid JSON-RPC 2.0 message — per spec A6,
// a stdio server must never write logs or anything else to stdout — so a
// non-JSON-RPC line fails the test immediately rather than being skipped.
// It returns the parsed response object for each id in wantIDs, keyed by id.
func sendRawJSONRPC(t *testing.T, msgs []string, wantIDs []float64) map[float64]map[string]any {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "serve-mcp")
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = io.Discard
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
				return responses
			}
		}
	}
	t.Fatalf("stream ended before all responses seen; got=%v scan err=%v", responses, sc.Err())
	return nil
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
	responses := sendRawJSONRPC(t, msgs, []float64{1, 2, 3})

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
	responses := sendRawJSONRPC(t, msgs, []float64{1, 2})

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
