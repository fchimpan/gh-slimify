package scan

import (
	"fmt"
	"os"

	"github.com/fchimpan/gh-slimify/internal/workflow"
)

// Candidate represents a job that is eligible for migration
type Candidate struct {
	WorkflowPath string
	JobName      string
	LineNumber   int
	Duration     string // Will be populated from GitHub API later
}

// Scan scans workflows and returns migration candidates
func Scan() ([]*Candidate, error) {
	workflows, err := workflow.LoadWorkflows()
	if err != nil {
		return nil, fmt.Errorf("failed to load workflows: %w", err)
	}

	if len(workflows) == 0 {
		fmt.Fprintf(os.Stderr, "No workflow files found in .github/workflows\n")
		return nil, nil
	}

	var candidates []*Candidate

	for _, wf := range workflows {
		for jobName, job := range wf.Jobs {
			// Check migration criteria
			if isEligible(job) {
				candidates = append(candidates, &Candidate{
					WorkflowPath: wf.Path,
					JobName:      jobName,
					LineNumber:   job.LineStart,
				})
			}
		}
	}

	return candidates, nil
}

// isEligible checks if a job meets all migration criteria
// Criteria:
// 1. Runs on ubuntu-latest
// 2. Does not use Docker commands
// 3. Does not use Docker-based GitHub Actions
// 4. Does not use services
// 5. Duration check will be added later via GitHub API
func isEligible(job *workflow.Job) bool {
	// Criterion 1: Must run on ubuntu-latest
	if !job.IsUbuntuLatest() {
		return false
	}

	// Criterion 2: Must not use Docker commands
	if job.HasDockerCommands() {
		return false
	}

	// Criterion 3: Must not use Docker-based GitHub Actions
	if job.HasDockerActions() {
		return false
	}

	// Criterion 4: Must not use services
	if job.HasServices() {
		return false
	}

	// Criterion 5: Duration check will be done via GitHub API
	// TODO: Implement duration check via GitHub API

	return true
}
