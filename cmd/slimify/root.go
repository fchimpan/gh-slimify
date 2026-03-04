package main

import (
	"fmt"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fchimpan/gh-slimify/internal/scan"
	"github.com/fchimpan/gh-slimify/internal/workflow"
	"github.com/spf13/cobra"
)

var (
	workflowFiles []string
	scanAll       bool
	skipDuration  bool
	verbose       bool
	force         bool
	jsonOutput    bool
)

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "slimify [flags] [workflow-file...]",
		Short: "Scan GitHub Actions workflows for ubuntu-slim migration candidates",
		Long: `slimify is a GitHub CLI extension that automatically detects and safely migrates
eligible ubuntu-latest jobs to ubuntu-slim.

By default, you must specify workflow file(s) to process. Use --all to scan all
workflows in .github/workflows/*.yml.`,
		Run:  runScan,
		Args: cobra.ArbitraryArgs,
	}

	rootCmd.PersistentFlags().StringArrayVarP(&workflowFiles, "file", "f", []string{}, "Specify workflow file(s) to process. Can be specified multiple times (e.g., -f .github/workflows/ci.yml -f .github/workflows/test.yml)")
	rootCmd.PersistentFlags().BoolVar(&scanAll, "all", false, "Scan all workflow files in .github/workflows/*.yml")
	rootCmd.PersistentFlags().BoolVar(&skipDuration, "skip-duration", false, "Skip fetching job execution durations from GitHub API to avoid unnecessary API calls")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output including debug warnings")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")

	fixCmd := &cobra.Command{
		Use:   "fix [flags] [workflow-file...]",
		Short: "Automatically update workflows to use ubuntu-slim",
		Long: `Replace runs-on: ubuntu-latest with ubuntu-slim for safe jobs that meet
all migration criteria. By default, only safe jobs (no missing commands and known execution time)
are updated. Use --force to also update jobs with warnings.

By default, you must specify workflow file(s) to process. Use --all to scan all
workflows in .github/workflows/*.yml.`,
		Run:  runFix,
		Args: cobra.ArbitraryArgs,
	}
	fixCmd.Flags().BoolVar(&force, "force", false, "Also update jobs with warnings (missing commands or unknown execution time)")

	rootCmd.AddCommand(fixCmd)
	return rootCmd
}

// resolveFiles collects workflow files from args and flags, validates input,
// and returns the list of files to scan.
// subcommand should be "" for the root command or the subcommand name (e.g. "fix").
func resolveFiles(args []string, subcommand string) []string {
	var files []string
	files = append(files, args...)
	files = append(files, workflowFiles...)

	if !scanAll && len(files) == 0 {
		prefix := ""
		if subcommand != "" {
			prefix = subcommand + " "
		}
		fmt.Fprintf(os.Stderr, "Error: no workflow files specified. Use --all to scan all workflows, or specify workflow file(s) as arguments or with --file flag.\n")
		fmt.Fprintf(os.Stderr, "Example: gh slimify %s.github/workflows/ci.yml\n", prefix)
		fmt.Fprintf(os.Stderr, "Example: gh slimify %s--all\n", prefix)
		os.Exit(1)
	}

	if scanAll {
		return []string{}
	}
	return files
}

func runScan(cmd *cobra.Command, args []string) {
	filesToScan := resolveFiles(args, "")

	if !jsonOutput {
		sp := spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithWriter(os.Stderr))
		sp.Suffix = " Scanning workflows..."
		sp.Start()

		result, err := scan.Scan(skipDuration, verbose, filesToScan...)
		sp.Stop()

		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ Scan failed\n")
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Fprintf(os.Stderr, "✓ Scan complete\n")
		printScanText(result)
		return
	}

	// JSON output path
	result, err := scan.Scan(skipDuration, verbose, filesToScan...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	printScanJSON(result)
}

func runFix(cmd *cobra.Command, args []string) {
	filesToScan := resolveFiles(args, "fix")

	// Scan phase
	if !jsonOutput {
		sp := spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithWriter(os.Stderr))
		sp.Suffix = " Scanning workflows..."
		sp.Start()
		result, err := scan.Scan(skipDuration, verbose, filesToScan...)
		sp.Stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ Scan failed\n")
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "✓ Scan complete\n")
		runFixWithResult(result, false)
		return
	}

	// JSON output path
	result, err := scan.Scan(skipDuration, verbose, filesToScan...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	runFixWithResult(result, true)
}

func runFixWithResult(result *scan.ScanResult, asJSON bool) {
	candidates := result.Candidates

	safeJobs, warningJobs := classifyCandidates(candidates)

	var jobsToUpdate []*scan.Candidate
	var skippedJobs []*scan.Candidate

	jobsToUpdate = append(jobsToUpdate, safeJobs...)
	if force {
		jobsToUpdate = append(jobsToUpdate, warningJobs...)
	} else {
		skippedJobs = append(skippedJobs, warningJobs...)
	}

	if len(jobsToUpdate) == 0 {
		if asJSON {
			printFixJSON(nil, skippedJobs, false)
		} else if len(skippedJobs) > 0 {
			fmt.Printf("No safe jobs to update. %d job(s) have warnings and were skipped.\n", len(skippedJobs))
			fmt.Println("Use --force to update jobs with warnings.")
		} else {
			fmt.Println("No jobs found that can be safely migrated to ubuntu-slim.")
		}
		return
	}

	if !asJSON {
		if force {
			fmt.Println("Updating workflows to use ubuntu-slim (including jobs with warnings)...")
		} else {
			fmt.Println("Updating workflows to use ubuntu-slim (safe jobs only)...")
			if len(skippedJobs) > 0 {
				fmt.Printf("Skipping %d job(s) with warnings. Use --force to update them.\n", len(skippedJobs))
			}
		}
		fmt.Println()
	}

	// Group jobs by workflow file
	workflowMap := make(map[string][]*scan.Candidate)
	for _, c := range jobsToUpdate {
		workflowMap[c.WorkflowPath] = append(workflowMap[c.WorkflowPath], c)
	}

	updatedCount := 0
	errorCount := 0

	var updateSpinner *spinner.Spinner
	if !asJSON {
		updateSpinner = spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithWriter(os.Stderr))
		updateSpinner.Suffix = " Updating workflows..."
		updateSpinner.Start()
	}

	var results []updateResult

	// Update each workflow file
	for workflowPath, jobs := range workflowMap {
		for _, job := range jobs {
			wf, err := workflow.LoadWorkflow(workflowPath)
			if err != nil {
				results = append(results, updateResult{
					workflowPath: workflowPath,
					jobID:        job.JobID,
					jobName:      job.JobName,
					lineNumber:   job.LineNumber,
					isError:      true,
					errorMsg:     fmt.Sprintf("Error loading workflow %s: %v", workflowPath, err),
				})
				errorCount++
				continue
			}

			if _, ok := wf.Jobs[job.JobID]; !ok {
				results = append(results, updateResult{
					workflowPath: workflowPath,
					jobID:        job.JobID,
					jobName:      job.JobName,
					lineNumber:   job.LineNumber,
					isNotFound:   true,
					errorMsg:     fmt.Sprintf("job %s (ID: %s) not found in %s", job.JobName, job.JobID, workflowPath),
				})
				continue
			}

			if err := workflow.UpdateRunsOn(workflowPath, job.JobID, "ubuntu-slim"); err != nil {
				results = append(results, updateResult{
					workflowPath: workflowPath,
					jobID:        job.JobID,
					jobName:      job.JobName,
					lineNumber:   job.LineNumber,
					isError:      true,
					errorMsg:     fmt.Sprintf("Error updating job %s (ID: %s) in %s: %v", job.JobName, job.JobID, workflowPath, err),
				})
				errorCount++
				continue
			}

			duration := job.Duration
			if duration == "" {
				duration = "unknown"
			}
			hasMissingCommands := len(job.MissingCommands) > 0
			hasUnknownDuration := duration == "unknown"

			results = append(results, updateResult{
				workflowPath: workflowPath,
				jobID:        job.JobID,
				jobName:      job.JobName,
				lineNumber:   job.LineNumber,
				hasWarnings:  hasMissingCommands || hasUnknownDuration,
			})
			updatedCount++
		}
	}

	if updateSpinner != nil {
		updateSpinner.Stop()
	}

	if asJSON {
		printFixJSON(results, skippedJobs, errorCount > 0)
		return
	}

	printFixText(results, updatedCount, errorCount)
}
