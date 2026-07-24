package scan

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/fchimpan/gh-slimify/internal/api"
	"github.com/fchimpan/gh-slimify/internal/workflow"
)

const (
	// SlimMaxDuration is ubuntu-slim's hard job time limit. Jobs whose last
	// successful run exceeds it will be killed after migration, so they are
	// reported as ineligible.
	SlimMaxDuration = 15 * time.Minute

	// SlimDurationWarnThreshold flags jobs whose last run came close to the
	// limit. ubuntu-slim has a single vCPU and is often slower than
	// ubuntu-latest, so a job near the limit may exceed it after migration.
	SlimDurationWarnThreshold = 10 * time.Minute
)

// Candidate represents a job that is eligible for migration
type Candidate struct {
	WorkflowPath    string
	JobID           string // Job ID (the key in the jobs map)
	JobName         string // Job display name (name: field in YAML, or job ID if not specified)
	LineNumber      int
	RawDuration     time.Duration // Last execution time from GitHub API; 0 means unknown
	MissingCommands []string      // Commands that exist in ubuntu-latest but need to be installed in ubuntu-slim

	usesRefs []string // uses: references of the job's steps, for Docker-action verification
}

// NearDurationLimit reports whether the job's last run came close enough to
// ubuntu-slim's 15-minute limit that the migration deserves attention.
func (c *Candidate) NearDurationLimit() bool {
	return c.RawDuration > SlimDurationWarnThreshold
}

// DurationText returns the human-readable last execution time, or "unknown".
func (c *Candidate) DurationText() string {
	if c.RawDuration == 0 {
		return "unknown"
	}
	return formatDuration(c.RawDuration)
}

// HasWarnings reports whether migrating this job needs attention: missing
// commands, unknown execution time, or a last run close to the 15-minute
// limit. Classification, output rendering, and the fix flow all share this
// predicate.
func (c *Candidate) HasWarnings() bool {
	return len(c.MissingCommands) > 0 || c.RawDuration == 0 || c.NearDurationLimit()
}

// toIneligible converts a demoted candidate into an ineligible record.
func (c *Candidate) toIneligible(reasons ...string) *IneligibleJob {
	return &IneligibleJob{
		WorkflowPath: c.WorkflowPath,
		JobID:        c.JobID,
		JobName:      c.JobName,
		LineNumber:   c.LineNumber,
		Reasons:      reasons,
	}
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

// Options controls how a scan runs.
type Options struct {
	// SkipDuration skips fetching job execution durations from the GitHub API.
	SkipDuration bool
	// Offline skips all GitHub API access: durations and remote action
	// metadata. Detection falls back to offline heuristics.
	Offline bool
	// Verbose enables debug warnings on stderr.
	Verbose bool
}

// Scan scans workflows and returns migration candidates and ineligible jobs.
// If paths are provided, only those files are scanned. Otherwise, all workflow
// files in .github/workflows are scanned.
func Scan(opts Options, paths ...string) (*ScanResult, error) {
	skipDuration := opts.SkipDuration || opts.Offline
	verbose := opts.Verbose
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
		// Iterate jobs in a stable order so output and API calls are
		// deterministic across runs.
		for _, jobID := range slices.Sorted(maps.Keys(wf.Jobs)) {
			job := wf.Jobs[jobID]
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
				var usesRefs []string
				for _, step := range job.Steps {
					if step.Uses != "" {
						usesRefs = append(usesRefs, step.Uses)
					}
				}
				candidates = append(candidates, &Candidate{
					WorkflowPath:    wf.Path,
					JobID:           jobID,
					JobName:         job.Name,
					LineNumber:      job.LineStart,
					MissingCommands: missingCommands,
					usesRefs:        usesRefs,
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

	// Build the API client once; the action-metadata resolver and the
	// duration fetch share it. Offline mode leaves it nil.
	var client *api.Client
	var repoErr error
	if !opts.Offline {
		var host, owner, repo string
		host, owner, repo, repoErr = api.GetRepoInfo()
		if repoErr != nil {
			// Action metadata still resolves against the default host;
			// only the duration fetch needs the repository.
			host, owner, repo = "", "", ""
		}
		var err error
		if client, err = api.NewClient(host, owner, repo); err != nil {
			client = nil
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: failed to create GitHub API client: %v\n", err)
			}
		}
	}

	// Verify that no remaining candidate uses a Dockerfile-based action from
	// a publisher the prefix heuristic doesn't know. Local actions are read
	// from disk; remote metadata is fetched unless running offline.
	if len(candidates) > 0 {
		candidates, ineligibleJobs = verifyContainerActions(candidates, ineligibleJobs, newActionResolver(client), verbose)
	}

	// Fetch duration from GitHub API for each candidate (unless skipped)
	if !skipDuration {
		if client != nil && repoErr == nil {
			fetchDurations(client, candidates, verbose)
		} else if verbose {
			fmt.Fprintf(os.Stderr, "Warning: failed to fetch job durations from GitHub API: %v\n", repoErr)
		}

		candidates, ineligibleJobs = enforceDurationLimit(candidates, ineligibleJobs)
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

	// Criterion 0: Reusable workflow calls define their runner elsewhere.
	if job.Uses != "" {
		reasons = append(reasons, "calls a reusable workflow (its runner is defined in the called workflow)")
		return false, reasons
	}

	// Criterion 1: Must run on a label ubuntu-slim can replace.
	if job.HasExpressionRunsOn() {
		reasons = append(reasons, "runs-on is set by an expression (e.g. matrix) and was not analyzed")
		return false, reasons
	}
	if !job.IsMigratableRunner() {
		reasons = append(reasons, "does not run on ubuntu-latest or ubuntu-24.04")
		return false, reasons
	}

	// Criterion 1b: Multi-label runs-on targets self-hosted runners and must
	// not be collapsed into a single GitHub-hosted label.
	if job.HasMultipleRunnerLabels() {
		reasons = append(reasons, "uses multiple runner labels (self-hosted runner)")
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

	// Criterion 6: Must not use privileged operations
	if hasPrivOps, privCmds := job.HasPrivilegedOperations(); hasPrivOps {
		reasons = append(reasons, fmt.Sprintf("uses privileged operations (%s)", strings.Join(privCmds, ", ")))
	}

	// Criterion 7: Duration check will be done via GitHub API
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

// enforceDurationLimit moves candidates whose last successful run exceeds
// ubuntu-slim's 15-minute limit into the ineligible list: such jobs would be
// killed after migration.
func enforceDurationLimit(candidates []*Candidate, ineligibleJobs []*IneligibleJob) ([]*Candidate, []*IneligibleJob) {
	remaining := candidates[:0]
	for _, c := range candidates {
		if c.RawDuration > SlimMaxDuration {
			ineligibleJobs = append(ineligibleJobs, c.toIneligible(fmt.Sprintf(
				"last execution time (%s) exceeds ubuntu-slim's 15-minute limit", c.DurationText())))
			continue
		}
		remaining = append(remaining, c)
	}
	return remaining, ineligibleJobs
}

// fetchDurations fetches job execution durations from the GitHub API.
// Per-candidate failures are logged in verbose mode and leave the duration
// unknown.
func fetchDurations(client *api.Client, candidates []*Candidate, verbose bool) {
	for _, candidate := range candidates {
		duration, err := client.GetJobDuration(candidate.WorkflowPath, candidate.JobID, candidate.JobName)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: failed to get duration for job %s (ID: %s) in %s: %v\n", candidate.JobName, candidate.JobID, candidate.WorkflowPath, err)
			}
			continue
		}

		candidate.RawDuration = duration.Duration
	}
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
