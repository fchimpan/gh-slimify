package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fchimpan/gh-slimify/internal/scan"
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "slimfy",
		Short: "Scan GitHub Actions workflows for ubuntu-slim migration candidates",
		Long: `slimfy is a GitHub CLI extension that automatically detects and safely migrates
eligible ubuntu-latest jobs to ubuntu-slim.

It analyzes .github/workflows/*.yml files and identifies jobs that can be safely
migrated based on migration criteria.`,
		Run: runScan,
	}

	fixCmd := &cobra.Command{
		Use:   "fix",
		Short: "Automatically update workflows to use ubuntu-slim",
		Long: `Replace runs-on: ubuntu-latest with ubuntu-slim for safe jobs that meet
all migration criteria.`,
		Run: runFix,
	}

	rootCmd.AddCommand(fixCmd)
	return rootCmd
}

func runScan(cmd *cobra.Command, args []string) {
	candidates, err := scan.Scan()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(candidates) == 0 {
		fmt.Println("No jobs found that can be safely migrated to ubuntu-slim.")
		return
	}

	// Group candidates by workflow file
	workflowMap := make(map[string][]*scan.Candidate)
	for _, c := range candidates {
		workflowMap[c.WorkflowPath] = append(workflowMap[c.WorkflowPath], c)
	}

	// Display results
	for workflowPath, jobs := range workflowMap {
		fmt.Printf("%s\n", workflowPath)
		for _, job := range jobs {
			duration := job.Duration
			if duration == "" {
				duration = "unknown"
			}
			// Generate local file link with line number
			jobLink := formatLocalLink(workflowPath, job.LineNumber)
			fmt.Printf("  - job \"%s\" (L%d) → ubuntu-slim compatible (last run: %s) %s\n",
				job.JobName, job.LineNumber, duration, jobLink)
		}
		fmt.Println()
	}

	fmt.Printf("Total: %d job(s) can be safely migrated.\n", len(candidates))
}

func runFix(cmd *cobra.Command, args []string) {
	fmt.Println("Updating workflows to use ubuntu-slim...")
	// TODO: Implement workflow fixing logic
	fmt.Println("(Fix functionality will be implemented)")
}

// formatLocalLink formats a local file link with line number
// This format is recognized by many terminal emulators (VS Code, iTerm2, etc.)
func formatLocalLink(filePath string, lineNumber int) string {
	// Get absolute path
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}
	return fmt.Sprintf("%s:%d", absPath, lineNumber)
}
