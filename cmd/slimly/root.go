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
	verbose       bool
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
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output including debug warnings")

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
	candidates, err := scan.Scan(skipDuration, verbose, workflowFiles...)
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

	// Display results grouped by workflow file
	for workflowPath, jobs := range workflowMap {
		fmt.Printf("\n📄 %s\n", workflowPath)

		// Separate safe jobs and jobs with warnings
		// Safe jobs: no missing commands AND execution time is known
		// Warning jobs: missing commands OR execution time is unknown
		var safeJobs []*scan.Candidate
		var warningJobs []*scan.Candidate
		for _, job := range jobs {
			duration := job.Duration
			if duration == "" {
				duration = "unknown"
			}
			hasMissingCommands := len(job.MissingCommands) > 0
			hasUnknownDuration := duration == "unknown"

			if hasMissingCommands || hasUnknownDuration {
				warningJobs = append(warningJobs, job)
			} else {
				safeJobs = append(safeJobs, job)
			}
		}

		// Display safe jobs first
		if len(safeJobs) > 0 {
			fmt.Printf("  ✅ Safe to migrate (%d job(s)):\n", len(safeJobs))
			for _, job := range safeJobs {
				jobLink := formatLocalLink(workflowPath, job.LineNumber)
				fmt.Printf("     • \"%s\" (L%d) - Last execution time: %s\n", job.JobName, job.LineNumber, job.Duration)
				fmt.Printf("       %s\n", jobLink)
			}
		}

		// Display jobs with warnings
		if len(warningJobs) > 0 {
			fmt.Printf("  ⚠️  Can migrate but requires attention (%d job(s)):\n", len(warningJobs))
			for _, job := range warningJobs {
				duration := job.Duration
				if duration == "" {
					duration = "unknown"
				}
				jobLink := formatLocalLink(workflowPath, job.LineNumber)

				// Build warning reasons in a single line
				var reasons []string
				if len(job.MissingCommands) > 0 {
					commandsStr := ""
					for i, cmd := range job.MissingCommands {
						if i > 0 {
							commandsStr += ", "
						}
						commandsStr += cmd
					}
					reasons = append(reasons, fmt.Sprintf("Setup may be required (%s)", commandsStr))
				}
				if duration == "unknown" {
					reasons = append(reasons, "Last execution time: unknown")
				}

				warningMsg := ""
				if len(reasons) > 0 {
					warningMsg = reasons[0]
					for i := 1; i < len(reasons); i++ {
						warningMsg += ", " + reasons[i]
					}
				}

				fmt.Printf("     • \"%s\" (L%d)\n", job.JobName, job.LineNumber)
				if warningMsg != "" {
					fmt.Printf("       ⚠️  %s\n", warningMsg)
				}
				if duration != "unknown" {
					fmt.Printf("       Last execution time: %s\n", duration)
				}
				fmt.Printf("       %s\n", jobLink)
			}
		}
	}

	// Summary
	safeCount := 0
	warningCount := 0
	for _, jobs := range workflowMap {
		for _, job := range jobs {
			duration := job.Duration
			if duration == "" {
				duration = "unknown"
			}
			hasMissingCommands := len(job.MissingCommands) > 0
			hasUnknownDuration := duration == "unknown"

			if hasMissingCommands || hasUnknownDuration {
				warningCount++
			} else {
				safeCount++
			}
		}
	}

	fmt.Println()
	if safeCount > 0 {
		fmt.Printf("✅ %d job(s) can be safely migrated\n", safeCount)
	}
	if warningCount > 0 {
		fmt.Printf("⚠️  %d job(s) can be migrated but require attention\n", warningCount)
	}
	fmt.Printf("📊 Total: %d job(s) eligible for migration\n", len(candidates))
}

func runFix(cmd *cobra.Command, args []string) {
	candidates, err := scan.Scan(skipDuration, verbose, workflowFiles...)
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
// Returns a relative path from the current working directory
func formatLocalLink(filePath string, lineNumber int) string {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		// If we can't get CWD, return the original path
		return fmt.Sprintf("%s:%d", filePath, lineNumber)
	}

	// Get absolute path of the file
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		// If we can't get absolute path, return the original path
		return fmt.Sprintf("%s:%d", filePath, lineNumber)
	}

	// Convert to relative path
	relPath, err := filepath.Rel(cwd, absPath)
	if err != nil {
		// If we can't get relative path, return absolute path
		return fmt.Sprintf("%s:%d", absPath, lineNumber)
	}

	return fmt.Sprintf("%s:%d", relPath, lineNumber)
}
