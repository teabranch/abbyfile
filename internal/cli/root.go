// Package cli provides the Cobra command structure for agentfile binaries.
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/teabranch/agentfile/pkg/memory"
	"github.com/teabranch/agentfile/pkg/prompt"
	"github.com/teabranch/agentfile/pkg/tools"
)

// AgentManifest is the JSON description of an agent, used for discovery.
type AgentManifest struct {
	SchemaVersion  string              `json:"schemaVersion"`
	Name           string              `json:"name"`
	Version        string              `json:"version"`
	Description    string              `json:"description"`
	PromptChecksum string              `json:"promptChecksum,omitempty"`
	Tools          []ToolManifestEntry `json:"tools,omitempty"`
	Memory         bool                `json:"memory"`
	MemoryLimits   *memory.Limits      `json:"memoryLimits,omitempty"`
}

// ToolManifestEntry describes a single tool in the manifest.
type ToolManifestEntry struct {
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	Builtin      bool               `json:"builtin"`
	InputSchema  any                `json:"inputSchema,omitempty"`
	OutputSchema any                `json:"outputSchema,omitempty"`
	Annotations  *tools.Annotations `json:"annotations,omitempty"`
}

// Options configures the root command.
type Options struct {
	Name         string
	Version      string
	Description  string
	Loader       *prompt.Loader
	Registry     *tools.Registry
	Memory       bool
	MemoryLimits *memory.Limits
	Logger       *slog.Logger
}

// NewRootCommand creates the root Cobra command for an agent binary.
func NewRootCommand(opts Options) *cobra.Command {
	var showInstructions bool
	var showDescribe bool

	cmd := &cobra.Command{
		Use:   opts.Name,
		Short: opts.Description,
		// Show help when run with no subcommand or flags
		RunE: func(cmd *cobra.Command, args []string) error {
			if showInstructions {
				p, err := opts.Loader.Load()
				if err != nil {
					return fmt.Errorf("loading instructions: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), p)
				return nil
			}

			if showDescribe {
				return printManifest(cmd, opts)
			}

			return cmd.Help()
		},
	}

	cmd.Version = opts.Version
	cmd.Flags().BoolVar(&showInstructions, "custom-instructions", false, "Print the agent's system prompt and exit")
	cmd.Flags().BoolVar(&showDescribe, "describe", false, "Print the agent manifest as JSON and exit")

	// Customize version template: "name vX.Y.Z"
	cmd.SetVersionTemplate(fmt.Sprintf("%s v{{.Version}}\n", opts.Name))

	return cmd
}

func printManifest(cmd *cobra.Command, opts Options) error {
	manifest := AgentManifest{
		SchemaVersion: "v1",
		Name:          opts.Name,
		Version:       opts.Version,
		Description:   opts.Description,
		Memory:        opts.Memory,
		MemoryLimits:  opts.MemoryLimits,
	}

	// Compute prompt checksum.
	if p, err := opts.Loader.Load(); err == nil {
		h := sha256.Sum256([]byte(p))
		manifest.PromptChecksum = hex.EncodeToString(h[:])
	}

	for _, def := range opts.Registry.All() {
		manifest.Tools = append(manifest.Tools, ToolManifestEntry{
			Name:         def.Name,
			Description:  def.Description,
			Builtin:      def.Builtin,
			InputSchema:  def.InputSchema,
			OutputSchema: def.OutputSchema,
			Annotations:  def.Annotations,
		})
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}
