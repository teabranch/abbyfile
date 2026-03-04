package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestRegistry_Register(t *testing.T) {
	reg := NewRegistry()
	def := CLI("date", "date", "Get current date")

	if err := reg.Register(def); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Duplicate registration should fail
	if err := reg.Register(def); err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegistry_Get(t *testing.T) {
	reg := NewRegistry()
	def := CLI("date", "date", "Get current date")
	reg.Register(def)

	got := reg.Get("date")
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if got.Name != "date" {
		t.Errorf("Name = %q, want %q", got.Name, "date")
	}

	if reg.Get("nonexistent") != nil {
		t.Error("Get() should return nil for unknown tool")
	}
}

func TestRegistry_All(t *testing.T) {
	reg := NewRegistry()
	reg.Register(CLI("date", "date", "Get current date"))
	reg.Register(CLI("echo", "echo", "Echo text"))

	all := reg.All()
	if len(all) != 2 {
		t.Errorf("All() count = %d, want 2", len(all))
	}
}

func TestRegistry_EmptyName(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(&Definition{})
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestExecutor_Run_CLI(t *testing.T) {
	exec := NewExecutor(5*time.Second, nil)
	def := CLI("echo", "echo", "Echo text")
	def.Args = []string{"hello"}

	result, err := exec.Run(context.Background(), def, nil)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result != "hello" {
		t.Errorf("Run() = %q, want %q", result, "hello")
	}
}

func TestExecutor_Run_WithArgs(t *testing.T) {
	exec := NewExecutor(5*time.Second, nil)
	def := CLI("echo", "echo", "Echo text")

	result, err := exec.Run(context.Background(), def, map[string]any{
		"args": "hello world",
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("Run() = %q, want %q", result, "hello world")
	}
}

func TestExecutor_Run_Builtin(t *testing.T) {
	exec := NewExecutor(5*time.Second, nil)
	def := BuiltinTool("test", "A test tool", nil, func(input map[string]any) (string, error) {
		return "builtin result", nil
	})

	result, err := exec.Run(context.Background(), def, nil)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result != "builtin result" {
		t.Errorf("Run() = %q, want %q", result, "builtin result")
	}
}

func TestExecutor_Run_Timeout(t *testing.T) {
	exec := NewExecutor(100*time.Millisecond, nil)
	def := CLI("sleep", "sleep", "Sleep")
	def.Args = []string{"10"}

	_, err := exec.Run(context.Background(), def, nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestExecutor_Run_StdinInput(t *testing.T) {
	exec := NewExecutor(5*time.Second, nil)
	def := &Definition{
		Name:       "stdin-cat",
		Command:    "cat",
		StdinInput: true,
	}

	input := map[string]any{
		"environment": "production",
		"replicas":    float64(3),
	}

	result, err := exec.Run(context.Background(), def, input)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// cat should echo back the JSON we piped in
	var got map[string]any
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatalf("parsing output as JSON: %v\noutput: %q", err, result)
	}
	if got["environment"] != "production" {
		t.Errorf("environment = %v, want %q", got["environment"], "production")
	}
	if got["replicas"] != float64(3) {
		t.Errorf("replicas = %v, want 3", got["replicas"])
	}
}

func TestExecutor_Run_StdinInput_WithArgs(t *testing.T) {
	exec := NewExecutor(5*time.Second, nil)
	def := &Definition{
		Name:       "stdin-cat",
		Command:    "cat",
		Args:       []string{},
		StdinInput: true,
	}

	// When StdinInput is true, the "args" key in input should NOT be
	// appended to the command line — it should be passed through stdin.
	input := map[string]any{
		"args": "should-not-be-cli-arg",
		"key":  "value",
	}

	result, err := exec.Run(context.Background(), def, input)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatalf("parsing output as JSON: %v\noutput: %q", err, result)
	}
	if got["args"] != "should-not-be-cli-arg" {
		t.Errorf("args = %v, want %q", got["args"], "should-not-be-cli-arg")
	}
	if got["key"] != "value" {
		t.Errorf("key = %v, want %q", got["key"], "value")
	}
}

func TestExecutor_Run_NoCommand(t *testing.T) {
	exec := NewExecutor(5*time.Second, nil)
	def := &Definition{Name: "bad"}

	_, err := exec.Run(context.Background(), def, nil)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}
