package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fchimpan/gh-slimify/internal/scan"
)

// JSON output types for scan command
type scanJobJSON struct {
	WorkflowPath      string   `json:"workflow_path"`
	JobID             string   `json:"job_id"`
	JobName           string   `json:"job_name"`
	LineNumber        int      `json:"line_number"`
	Status            string   `json:"status"`
	StatusDescription string   `json:"status_description"`
	RecommendedAction string   `json:"recommended_action"`
	DurationSeconds   *float64 `json:"duration_seconds,omitempty"`
	MissingCommands   []string `json:"missing_commands,omitempty"`
	Reasons           []string `json:"reasons,omitempty"`
}

type scanSummaryJSON struct {
	Safe        int `json:"safe"`
	Warning     int `json:"warning"`
	Ineligible  int `json:"ineligible"`
	AlreadySlim int `json:"already_slim"`
	Total       int `json:"total"`
}

type scanOutputJSON struct {
	Jobs    []scanJobJSON   `json:"jobs"`
	Summary scanSummaryJSON `json:"summary"`
}

// JSON output types for fix command
type fixJobJSON struct {
	WorkflowPath      string `json:"workflow_path"`
	JobID             string `json:"job_id"`
	JobName           string `json:"job_name"`
	LineNumber        int    `json:"line_number"`
	Status            string `json:"status"`
	StatusDescription string `json:"status_description"`
	RecommendedAction string `json:"recommended_action"`
	HasWarnings       bool   `json:"has_warnings"`
	Error             string `json:"error,omitempty"`
}

type fixSummaryJSON struct {
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

type fixOutputJSON struct {
	Jobs    []fixJobJSON   `json:"jobs"`
	Summary fixSummaryJSON `json:"summary"`
}

// updateResult holds the result of updating a single job in a workflow.
type updateResult struct {
	workflowPath string
	jobID        string
	jobName      string
	lineNumber   int
	hasWarnings  bool
	isError      bool
	errorMsg     string
	isNotFound   bool
}

// parseDurationSeconds parses a human-readable duration string (e.g. "2m30s")
// and returns a pointer to the total seconds. Returns nil for empty or unparseable strings.
func parseDurationSeconds(s string) *float64 {
	if s == "" {
		return nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil
	}
	secs := d.Seconds()
	return &secs
}

// classifyCandidates splits candidates into safe and warning groups.
func classifyCandidates(candidates []*scan.Candidate) (safe, warning []*scan.Candidate) {
	for _, job := range candidates {
		duration := job.Duration
		if duration == "" {
			duration = "unknown"
		}
		if len(job.MissingCommands) > 0 || duration == "unknown" {
			warning = append(warning, job)
		} else {
			safe = append(safe, job)
		}
	}
	return
}

func printScanJSON(result *scan.ScanResult) {
	candidates := result.Candidates
	ineligibleJobs := result.IneligibleJobs
	alreadySlimJobs := result.AlreadySlimJobs

	safeJobs, warningJobs := classifyCandidates(candidates)

	var jobs []scanJobJSON

	for _, job := range safeJobs {
		jobs = append(jobs, scanJobJSON{
			WorkflowPath:      job.WorkflowPath,
			JobID:             job.JobID,
			JobName:           job.JobName,
			LineNumber:        job.LineNumber,
			Status:            "safe",
			StatusDescription: "Safe to migrate to ubuntu-slim. No missing commands and execution time is known.",
			RecommendedAction: "migrate",
			DurationSeconds:   parseDurationSeconds(job.Duration),
		})
	}

	for _, job := range warningJobs {
		duration := job.Duration
		if duration == "" {
			duration = "unknown"
		}

		var details []string
		if len(job.MissingCommands) > 0 {
			details = append(details, fmt.Sprintf("Setup may be required for: %s.", strings.Join(job.MissingCommands, ", ")))
		}
		if duration == "unknown" {
			details = append(details, "Last execution time is unknown.")
		}

		jobs = append(jobs, scanJobJSON{
			WorkflowPath:      job.WorkflowPath,
			JobID:             job.JobID,
			JobName:           job.JobName,
			LineNumber:        job.LineNumber,
			Status:            "warning",
			StatusDescription: "Can migrate but requires attention. " + strings.Join(details, " "),
			RecommendedAction: "review_before_migrate",
			DurationSeconds:   parseDurationSeconds(job.Duration),
			MissingCommands:   job.MissingCommands,
		})
	}

	for _, job := range ineligibleJobs {
		reasonsStr := strings.Join(job.Reasons, ", ")
		jobs = append(jobs, scanJobJSON{
			WorkflowPath:      job.WorkflowPath,
			JobID:             job.JobID,
			JobName:           job.JobName,
			LineNumber:        job.LineNumber,
			Status:            "ineligible",
			StatusDescription: "Cannot migrate to ubuntu-slim. " + reasonsStr,
			RecommendedAction: "do_not_migrate",
			Reasons:           job.Reasons,
		})
	}

	for _, job := range alreadySlimJobs {
		jobs = append(jobs, scanJobJSON{
			WorkflowPath:      job.WorkflowPath,
			JobID:             job.JobID,
			JobName:           job.JobName,
			LineNumber:        job.LineNumber,
			Status:            "already_slim",
			StatusDescription: "Already using ubuntu-slim. No action needed.",
			RecommendedAction: "no_action_needed",
		})
	}

	if jobs == nil {
		jobs = []scanJobJSON{}
	}

	output := scanOutputJSON{
		Jobs: jobs,
		Summary: scanSummaryJSON{
			Safe:        len(safeJobs),
			Warning:     len(warningJobs),
			Ineligible:  len(ineligibleJobs),
			AlreadySlim: len(alreadySlimJobs),
			Total:       len(safeJobs) + len(warningJobs) + len(ineligibleJobs) + len(alreadySlimJobs),
		},
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(output)
}

func printScanText(result *scan.ScanResult) {
	candidates := result.Candidates
	ineligibleJobs := result.IneligibleJobs
	alreadySlimJobs := result.AlreadySlimJobs

	// Group candidates by workflow file
	workflowMap := make(map[string][]*scan.Candidate)
	for _, c := range candidates {
		workflowMap[c.WorkflowPath] = append(workflowMap[c.WorkflowPath], c)
	}

	// Group ineligible jobs by workflow file
	ineligibleMap := make(map[string][]*scan.IneligibleJob)
	for _, job := range ineligibleJobs {
		ineligibleMap[job.WorkflowPath] = append(ineligibleMap[job.WorkflowPath], job)
	}

	// Group already slim jobs by workflow file
	alreadySlimMap := make(map[string][]*scan.AlreadySlimJob)
	for _, job := range alreadySlimJobs {
		alreadySlimMap[job.WorkflowPath] = append(alreadySlimMap[job.WorkflowPath], job)
	}

	// Display results grouped by workflow file
	allWorkflowPaths := make(map[string]bool)
	for path := range workflowMap {
		allWorkflowPaths[path] = true
	}
	for path := range ineligibleMap {
		allWorkflowPaths[path] = true
	}
	for path := range alreadySlimMap {
		allWorkflowPaths[path] = true
	}

	for workflowPath := range allWorkflowPaths {
		fmt.Printf("\n📄 %s\n", workflowPath)
		jobs := workflowMap[workflowPath]

		safeJobs, warningJobs := classifyCandidates(jobs)

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

		// Display ineligible jobs
		ineligibleJobsForWorkflow := ineligibleMap[workflowPath]
		if len(ineligibleJobsForWorkflow) > 0 {
			fmt.Printf("  ❌ Cannot migrate (%d job(s)):\n", len(ineligibleJobsForWorkflow))
			for _, job := range ineligibleJobsForWorkflow {
				jobLink := formatLocalLink(workflowPath, job.LineNumber)
				reasonsStr := ""
				if len(job.Reasons) > 0 {
					reasonsStr = job.Reasons[0]
					for i := 1; i < len(job.Reasons); i++ {
						reasonsStr += ", " + job.Reasons[i]
					}
				}
				fmt.Printf("     • \"%s\" (L%d)\n", job.JobName, job.LineNumber)
				if reasonsStr != "" {
					fmt.Printf("       ❌ %s\n", reasonsStr)
				}
				fmt.Printf("       %s\n", jobLink)
			}
		}

		// Display already slim jobs
		alreadySlimJobsForWorkflow := alreadySlimMap[workflowPath]
		if len(alreadySlimJobsForWorkflow) > 0 {
			fmt.Printf("  ✨ Already using ubuntu-slim (%d job(s)):\n", len(alreadySlimJobsForWorkflow))
			for _, job := range alreadySlimJobsForWorkflow {
				jobLink := formatLocalLink(workflowPath, job.LineNumber)
				fmt.Printf("     • \"%s\" (L%d)\n", job.JobName, job.LineNumber)
				fmt.Printf("       %s\n", jobLink)
			}
		}
	}

	// Summary
	safeCount := 0
	warningCount := 0
	for _, jobs := range workflowMap {
		safe, warning := classifyCandidates(jobs)
		safeCount += len(safe)
		warningCount += len(warning)
	}

	fmt.Println()
	if safeCount > 0 {
		fmt.Printf("✅ %d job(s) can be safely migrated\n", safeCount)
	}
	if warningCount > 0 {
		fmt.Printf("⚠️  %d job(s) can be migrated but require attention\n", warningCount)
	}
	if len(ineligibleJobs) > 0 {
		fmt.Printf("❌ %d job(s) cannot be migrated\n", len(ineligibleJobs))
	}
	if len(alreadySlimJobs) > 0 {
		fmt.Printf("✨ %d job(s) already using ubuntu-slim\n", len(alreadySlimJobs))
	}
	if len(candidates) > 0 {
		fmt.Printf("📊 Total: %d job(s) eligible for migration\n", len(candidates))
	}
	if len(candidates) == 0 && len(ineligibleJobs) == 0 && len(alreadySlimJobs) == 0 {
		fmt.Println("No jobs found that can be safely migrated to ubuntu-slim.")
	}
}

func printFixJSON(results []updateResult, skippedJobs []*scan.Candidate, hasErrors bool) {
	var jobs []fixJobJSON
	updatedCount := 0
	skippedCount := 0
	errorCount := 0

	for _, r := range results {
		if r.isError {
			jobs = append(jobs, fixJobJSON{
				WorkflowPath:      r.workflowPath,
				JobID:             r.jobID,
				JobName:           r.jobName,
				LineNumber:        r.lineNumber,
				Status:            "error",
				StatusDescription: fmt.Sprintf("Failed to update: %s", r.errorMsg),
				RecommendedAction: "investigate_error",
				Error:             r.errorMsg,
			})
			errorCount++
		} else if r.isNotFound {
			jobs = append(jobs, fixJobJSON{
				WorkflowPath:      r.workflowPath,
				JobID:             r.jobID,
				JobName:           r.jobName,
				LineNumber:        r.lineNumber,
				Status:            "not_found",
				StatusDescription: "Job not found in workflow file.",
				RecommendedAction: "investigate_error",
				Error:             r.errorMsg,
			})
			errorCount++
		} else if r.hasWarnings {
			jobs = append(jobs, fixJobJSON{
				WorkflowPath:      r.workflowPath,
				JobID:             r.jobID,
				JobName:           r.jobName,
				LineNumber:        r.lineNumber,
				Status:            "updated",
				StatusDescription: "Updated to ubuntu-slim but has warnings. Review job configuration.",
				RecommendedAction: "verify_workflow_carefully",
				HasWarnings:       true,
			})
			updatedCount++
		} else {
			jobs = append(jobs, fixJobJSON{
				WorkflowPath:      r.workflowPath,
				JobID:             r.jobID,
				JobName:           r.jobName,
				LineNumber:        r.lineNumber,
				Status:            "updated",
				StatusDescription: "Successfully updated to ubuntu-slim.",
				RecommendedAction: "verify_workflow",
			})
			updatedCount++
		}
	}

	for _, job := range skippedJobs {
		jobs = append(jobs, fixJobJSON{
			WorkflowPath:      job.WorkflowPath,
			JobID:             job.JobID,
			JobName:           job.JobName,
			LineNumber:        job.LineNumber,
			Status:            "skipped",
			StatusDescription: "Skipped due to warnings. Use --force to update.",
			RecommendedAction: "review_then_force",
			HasWarnings:       true,
		})
		skippedCount++
	}

	if jobs == nil {
		jobs = []fixJobJSON{}
	}

	output := fixOutputJSON{
		Jobs: jobs,
		Summary: fixSummaryJSON{
			Updated: updatedCount,
			Skipped: skippedCount,
			Errors:  errorCount,
		},
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(output)

	if hasErrors {
		os.Exit(1)
	}
}

func printFixText(results []updateResult, updatedCount, errorCount int) {
	if errorCount > 0 {
		fmt.Fprintf(os.Stderr, "✗ Update completed with errors\n")
	} else {
		fmt.Fprintf(os.Stderr, "✓ Update complete\n")
	}
	fmt.Println()

	currentWorkflow := ""
	for _, r := range results {
		if r.workflowPath != currentWorkflow {
			if currentWorkflow != "" {
				fmt.Println()
			}
			fmt.Printf("Updated %s\n", r.workflowPath)
			currentWorkflow = r.workflowPath
		}

		if r.isError {
			fmt.Fprintf(os.Stderr, "  ✗ %s\n", r.errorMsg)
		} else if r.isNotFound {
			fmt.Fprintf(os.Stderr, "  ⚠️  Warning: %s\n", r.errorMsg)
		} else if r.hasWarnings {
			fmt.Printf("  ⚠️  Updated job \"%s\" (L%d) → ubuntu-slim (with warnings)\n", r.jobName, r.lineNumber)
		} else {
			fmt.Printf("  ✓ Updated job \"%s\" (L%d) → ubuntu-slim\n", r.jobName, r.lineNumber)
		}
	}
	fmt.Println()

	fmt.Printf("Successfully updated %d job(s) to use ubuntu-slim.\n", updatedCount)
	if errorCount > 0 {
		fmt.Fprintf(os.Stderr, "Encountered %d error(s) during update.\n", errorCount)
		os.Exit(1)
	}
}

// formatLocalLink formats a local file link with line number.
// This format is recognized by many terminal emulators (VS Code, iTerm2, etc.)
// Returns a relative path from the current working directory.
func formatLocalLink(filePath string, lineNumber int) string {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Sprintf("%s:%d", filePath, lineNumber)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Sprintf("%s:%d", filePath, lineNumber)
	}

	relPath, err := filepath.Rel(cwd, absPath)
	if err != nil {
		return fmt.Sprintf("%s:%d", absPath, lineNumber)
	}

	return fmt.Sprintf("%s:%d", relPath, lineNumber)
}
