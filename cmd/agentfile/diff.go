package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func newDiffCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <binary1> <binary2>",
		Short: "Compare two agent binary versions",
		Long: `Compares the system prompts and manifests of two agent binaries by running
--custom-instructions and --describe on each, then showing a unified diff.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(args[0], args[1])
		},
	}
}

func runDiff(bin1, bin2 string) error {
	// Validate binaries exist and are executable.
	for _, bin := range []string{bin1, bin2} {
		info, err := os.Stat(bin)
		if err != nil {
			return fmt.Errorf("cannot access %s: %w", bin, err)
		}
		if info.IsDir() {
			return fmt.Errorf("%s is a directory, expected executable", bin)
		}
	}

	// Compare --custom-instructions output.
	prompt1, err := runBinary(bin1, "--custom-instructions")
	if err != nil {
		return fmt.Errorf("running %s --custom-instructions: %w", bin1, err)
	}
	prompt2, err := runBinary(bin2, "--custom-instructions")
	if err != nil {
		return fmt.Errorf("running %s --custom-instructions: %w", bin2, err)
	}

	// Compare --describe output.
	desc1, err := runBinary(bin1, "--describe")
	if err != nil {
		return fmt.Errorf("running %s --describe: %w", bin1, err)
	}
	desc2, err := runBinary(bin2, "--describe")
	if err != nil {
		return fmt.Errorf("running %s --describe: %w", bin2, err)
	}

	hasDiff := false

	if prompt1 != prompt2 {
		fmt.Println("=== System Prompt Diff ===")
		printLineDiff(bin1, bin2, prompt1, prompt2)
		hasDiff = true
	}

	if desc1 != desc2 {
		if hasDiff {
			fmt.Println()
		}
		fmt.Println("=== Manifest Diff ===")
		printLineDiff(bin1, bin2, desc1, desc2)
		hasDiff = true
	}

	if !hasDiff {
		fmt.Println("No differences found.")
	}

	return nil
}

// runBinary executes a binary with the given flag and returns stdout.
func runBinary(path, flag string) (string, error) {
	cmd := exec.Command(path, flag)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// printLineDiff prints a simple unified-style diff showing additions and removals.
func printLineDiff(label1, label2, text1, text2 string) {
	lines1 := strings.Split(text1, "\n")
	lines2 := strings.Split(text2, "\n")

	fmt.Printf("--- %s\n", label1)
	fmt.Printf("+++ %s\n", label2)

	// Build a set of lines in each for simple comparison.
	set1 := make(map[string]bool, len(lines1))
	for _, l := range lines1 {
		set1[l] = true
	}
	set2 := make(map[string]bool, len(lines2))
	for _, l := range lines2 {
		set2[l] = true
	}

	// Show lines only in text1 (removals).
	for _, l := range lines1 {
		if !set2[l] {
			fmt.Printf("- %s\n", l)
		}
	}

	// Show lines only in text2 (additions).
	for _, l := range lines2 {
		if !set1[l] {
			fmt.Printf("+ %s\n", l)
		}
	}
}
