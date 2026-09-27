package mcp_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	agentmcp "github.com/teabranch/abbyfile/pkg/mcp"
	"github.com/teabranch/abbyfile/pkg/tools"
)

const fullPrompt = "You are a test agent for MCP bridge testing."

var backtickName = regexp.MustCompile("`([A-Za-z0-9_.-]+)`")

// TestInstructionsModes pins D-2 for both protocol eras: get_instructions
// exists iff the handshake carries the stub, and the stub names only tools
// that exist.
func TestInstructionsModes(t *testing.T) {
	for _, era := range []string{"", "2025-11-25"} {
		for _, eager := range []bool{false, true} {
			t.Run(fmt.Sprintf("era=%q/eager=%t", era, eager), func(t *testing.T) {
				sess := startBridgeEra(t, agentmcp.BridgeConfig{
					Name:              "test-agent",
					Version:           "v0.1.0",
					Description:       "A test agent",
					Model:             "claude-opus-4-6",
					Registry:          tools.NewRegistry(),
					Executor:          tools.NewExecutor(30*time.Second, nil),
					Loader:            newTestLoader(t),
					EagerInstructions: eager,
				}, era)

				list, err := sess.ListTools(context.Background(), nil)
				if err != nil {
					t.Fatalf("list tools: %v", err)
				}
				listed := map[string]string{}
				for _, tool := range list.Tools {
					listed[tool.Name] = tool.Description
				}
				instr := sess.InitializeResult().Instructions

				if eager {
					if _, ok := listed["get_instructions"]; ok {
						t.Error("eager mode must not register get_instructions")
					}
					if !strings.Contains(instr, fullPrompt) {
						t.Errorf("eager instructions missing full prompt: %q", instr)
					}
					if !strings.Contains(instr, "claude-opus-4-6") { // Review Focus #1
						t.Errorf("eager instructions missing model hint: %q", instr)
					}
					if strings.Contains(instr, "get_instructions") {
						t.Errorf("eager instructions must not mention get_instructions: %q", instr)
					}
					return
				}

				desc, ok := listed["get_instructions"]
				if !ok {
					t.Fatal("non-eager mode must register get_instructions")
				}
				if strings.Contains(desc, "Deprecated") {
					t.Errorf("get_instructions description still says Deprecated: %q", desc)
				}
				want := "A test agent\n\nCall the `get_instructions` tool to load your full instructions before acting."
				if instr != want {
					t.Errorf("stub = %q, want %q", instr, want)
				}
				for _, m := range backtickName.FindAllStringSubmatch(instr, -1) {
					if _, ok := listed[m[1]]; !ok {
						t.Errorf("stub names %q, which is not in tools/list", m[1])
					}
				}
			})
		}
	}
}
