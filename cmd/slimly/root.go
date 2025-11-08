package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fchimpan/gh-slimify/internal/scan"
	"github.com/fchimpan/gh-slimify/internal/workflow"
	"github.com/spf13/cobra"
)

var (
	workflowFiles []string
	skipDuration  bool
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

	rootCmd.PersistentFlags().StringArrayVarP(&workflowFiles, "file", "f", []string{}, "Specify workflow file(s) to process. If not specified, all files in .github/workflows/*.yml are processed. Can be specified multiple times (e.g., -f .github/workflows/ci.yml -f .github/workflows/test.yml)")
	rootCmd.PersistentFlags().BoolVar(&skipDuration, "skip-duration", false, "Skip fetching job execution durations from GitHub API to avoid unnecessary API calls")

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
	candidates, err := scan.Scan(skipDuration, workflowFiles...)
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
	candidates, err := scan.Scan(skipDuration, workflowFiles...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(candidates) == 0 {
		fmt.Println("No jobs found that can be safely migrated to ubuntu-slim.")
		return
	}

	fmt.Println("Updating workflows to use ubuntu-slim...")
	fmt.Println()

	// Group candidates by workflow file
	workflowMap := make(map[string][]*scan.Candidate)
	for _, c := range candidates {
		workflowMap[c.WorkflowPath] = append(workflowMap[c.WorkflowPath], c)
	}

	updatedCount := 0
	errorCount := 0

	// Update each workflow file
	for workflowPath, jobs := range workflowMap {
		fmt.Printf("Updating %s\n", workflowPath)
		for _, job := range jobs {
			// Reload workflow to get current state
			wf, err := workflow.LoadWorkflow(workflowPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Error loading workflow %s: %v\n", workflowPath, err)
				errorCount++
				continue
			}

			// Verify job still exists and is eligible
			if _, ok := wf.Jobs[job.JobName]; !ok {
				fmt.Fprintf(os.Stderr, "  Warning: job %s not found in %s\n", job.JobName, workflowPath)
				continue
			}

			// Update runs-on value
			if err := workflow.UpdateRunsOn(workflowPath, job.JobName, "ubuntu-slim"); err != nil {
				fmt.Fprintf(os.Stderr, "  Error updating job %s in %s: %v\n", job.JobName, workflowPath, err)
				errorCount++
				continue
			}

			fmt.Printf("  ✓ Updated job \"%s\" (L%d) → ubuntu-slim\n", job.JobName, job.LineNumber)
			updatedCount++
		}
		fmt.Println()
	}

	// Summary
	fmt.Printf("Successfully updated %d job(s) to use ubuntu-slim.\n", updatedCount)
	if errorCount > 0 {
		fmt.Fprintf(os.Stderr, "Encountered %d error(s) during update.\n", errorCount)
		os.Exit(1)
	}
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
