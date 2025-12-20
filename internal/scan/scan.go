package scan

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/fchimpan/gh-slimify/internal/api"
	"github.com/fchimpan/gh-slimify/internal/workflow"
)

// Candidate represents a job that is eligible for migration
type Candidate struct {
	WorkflowPath    string
	JobID           string // Job ID (the key in the jobs map)
	JobName         string // Job display name (name: field in YAML, or job ID if not specified)
	LineNumber      int
	Duration        string   // Will be populated from GitHub API later
	MissingCommands []string // Commands that exist in ubuntu-latest but need to be installed in ubuntu-slim
}

// IneligibleJob represents a job that is not eligible for migration
type IneligibleJob struct {
	WorkflowPath string
	JobID        string // Job ID (the key in the jobs map)
	JobName      string // Job display name (name: field in YAML, or job ID if not specified)
	LineNumber   int
	Reasons      []string // Reasons why the job cannot be migrated
}

// AlreadySlimJob represents a job that is already using ubuntu-slim
type AlreadySlimJob struct {
	WorkflowPath string
	JobID        string // Job ID (the key in the jobs map)
	JobName      string // Job display name (name: field in YAML, or job ID if not specified)
	LineNumber   int
}

// ScanResult contains both eligible candidates and ineligible jobs
type ScanResult struct {
	Candidates      []*Candidate
	IneligibleJobs  []*IneligibleJob
	AlreadySlimJobs []*AlreadySlimJob
}

// Scan scans workflows and returns migration candidates and ineligible jobs
// If paths are provided, only those files are scanned. Otherwise, all workflow files
// in .github/workflows are scanned.
// skipDuration, if true, skips fetching job execution durations from GitHub API.
// verbose, if true, enables verbose output including debug warnings.
func Scan(skipDuration bool, verbose bool, paths ...string) (*ScanResult, error) {
	var workflows []*workflow.Workflow
	var err error

	if len(paths) > 0 {
		// Load only specified files
		workflows = make([]*workflow.Workflow, 0, len(paths))
		for _, path := range paths {
			wf, err := workflow.LoadWorkflow(path)
			if err != nil {
				return nil, fmt.Errorf("failed to load workflow %s: %w", path, err)
			}
			workflows = append(workflows, wf)
		}
	} else {
		// Load all workflows
		workflows, err = workflow.LoadWorkflows()
		if err != nil {
			return nil, fmt.Errorf("failed to load workflows: %w", err)
		}

		if len(workflows) == 0 {
			fmt.Fprintf(os.Stderr, "No workflow files found in .github/workflows\n")
			return &ScanResult{
				Candidates:      []*Candidate{},
				IneligibleJobs:  []*IneligibleJob{},
				AlreadySlimJobs: []*AlreadySlimJob{},
			}, nil
		}
	}

	var candidates []*Candidate
	var ineligibleJobs []*IneligibleJob
	var alreadySlimJobs []*AlreadySlimJob

	for _, wf := range workflows {
		for jobID, job := range wf.Jobs {
			// Check if job is already using ubuntu-slim
			if job.IsUbuntuSlim() {
				alreadySlimJobs = append(alreadySlimJobs, &AlreadySlimJob{
					WorkflowPath: wf.Path,
					JobID:        jobID,
					JobName:      job.Name,
					LineNumber:   job.LineStart,
				})
				continue
			}

			// Check migration criteria
			isEligible, reasons := checkEligibility(job)
			if isEligible {
				// Check for missing commands and include in candidate
				missingCommands := job.GetMissingCommands()
				candidates = append(candidates, &Candidate{
					WorkflowPath:    wf.Path,
					JobID:           jobID,
					JobName:         job.Name,
					LineNumber:      job.LineStart,
					MissingCommands: missingCommands,
				})
			} else {
				// Record ineligible job with reasons
				ineligibleJobs = append(ineligibleJobs, &IneligibleJob{
					WorkflowPath: wf.Path,
					JobID:        jobID,
					JobName:      job.Name,
					LineNumber:   job.LineStart,
					Reasons:      reasons,
				})
			}
		}
	}

	// Fetch duration from GitHub API for each candidate (unless skipped)
	if !skipDuration {
		if err := fetchDurations(candidates, verbose); err != nil {
			// Log error but don't fail the scan
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: failed to fetch job durations from GitHub API: %v\n", err)
			}
		}
	}

	return &ScanResult{
		Candidates:      candidates,
		IneligibleJobs:  ineligibleJobs,
		AlreadySlimJobs: alreadySlimJobs,
	}, nil
}

// checkEligibility checks if a job meets all migration criteria and returns
// eligibility status along with reasons if not eligible.
// Criteria:
// 1. Runs on ubuntu-latest
// 2. Does not use Docker commands
// 3. Does not use container-based GitHub Actions
// 4. Does not use services containers (e.g. services:)
// 5. Does not run steps inside a Docker container. (e.g. container:)
// 6. Duration check will be added later via GitHub API
// Returns (isEligible, reasons) where reasons is empty if eligible.
func checkEligibility(job *workflow.Job) (bool, []string) {
	var reasons []string

	// Criterion 1: Must run on ubuntu-latest
	if !job.IsUbuntuLatest() {
		reasons = append(reasons, "does not run on ubuntu-latest")
		return false, reasons
	}

	// Criterion 2: Must not use Docker commands
	if job.HasDockerCommands() {
		reasons = append(reasons, "uses Docker commands")
	}

	// Criterion 3: Must not use container-based GitHub Actions
	if job.HasContainerActions() {
		reasons = append(reasons, "uses container-based GitHub Actions")
	}

	// Criterion 4: Must not use services
	if job.HasServices() {
		reasons = append(reasons, "uses service containers")
	}

	// Criterion 5: Must not use container: syntax
	if job.HasContainer() {
		reasons = append(reasons, "uses container syntax")
	}

	// Criterion 6: Duration check will be done via GitHub API
	// Duration is fetched after eligibility check to avoid blocking on API calls

	if len(reasons) > 0 {
		return false, reasons
	}

	return true, nil
}

// isEligible checks if a job meets all migration criteria (kept for backward compatibility with tests)
func isEligible(job *workflow.Job) bool {
	isEligible, _ := checkEligibility(job)
	return isEligible
}

// fetchDurations fetches job execution durations from GitHub API
// verbose, if true, enables verbose output including debug warnings.
func fetchDurations(candidates []*Candidate, verbose bool) error {
	if len(candidates) == 0 {
		return nil
	}

	// Get repository info from git remote
	host, owner, repo, err := api.GetRepoInfo()
	if err != nil {
		return fmt.Errorf("failed to get repository info: %w", err)
	}

	// Create API client
	client, err := api.NewClient(host, owner, repo)
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	ctx := context.Background()

	// Fetch duration for each candidate
	for _, candidate := range candidates {
		duration, err := client.GetJobDuration(ctx, candidate.WorkflowPath, candidate.JobID, candidate.JobName)
		if err != nil {
			// Log error for debugging but continue to next candidate
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: failed to get duration for job %s (ID: %s) in %s: %v\n", candidate.JobName, candidate.JobID, candidate.WorkflowPath, err)
			}
			continue
		}

		// Format duration as human-readable string
		candidate.Duration = formatDuration(duration.Duration)
	}

	return nil
}

// formatDuration formats a duration as a human-readable string
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		if seconds == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, minutes)
}
