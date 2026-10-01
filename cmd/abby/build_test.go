package main

import (
	"slices"
	"testing"
)

func TestAgentOwnTools_DropsBuiltins(t *testing.T) {
	m := &agentManifest{Tools: []manifestTool{
		{Name: "read_file"}, {Name: "run_command"}, {Name: "lint"}, {Name: "memory_read"},
	}}
	got := agentOwnTools(m)
	want := []string{"lint", "memory_read"}
	if !slices.Equal(got, want) {
		t.Fatalf("agentOwnTools = %v, want %v", got, want)
	}
}

func TestAgentOwnTools_NilManifest(t *testing.T) {
	if got := agentOwnTools(nil); got != nil {
		t.Fatalf("agentOwnTools(nil) = %v, want nil", got)
	}
}
