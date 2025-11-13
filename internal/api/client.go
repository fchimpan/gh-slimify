package api

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
)


// Client wraps GitHub API client for Actions API
type Client struct {
	restClient *api.RESTClient
	host       string
	owner      string
	repo       string
}

// NewClient creates a new GitHub API client
// If host is empty, it defaults to github.com
func NewClient(host, owner, repo string) (*Client, error) {
	if host == "" {
		host = "github.com"
	}

	// Create REST client with automatic authentication from gh CLI
	restClient, err := api.DefaultRESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create REST client: %w", err)
	}

	return &Client{
		restClient: restClient,
		host:       host,
		owner:      owner,
		repo:       repo,
	}, nil
}


// JobDuration represents job execution duration information
type JobDuration struct {
	JobName  string
	Duration time.Duration
}

// GetJobDuration gets the latest execution duration for a specific job in a workflow
func (c *Client) GetJobDuration(ctx context.Context, workflowPath, jobName string) (*JobDuration, error) {
	// Get workflow runs
	runs, err := c.getWorkflowRuns(ctx, workflowPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow runs: %w", err)
	}

	if len(runs) == 0 {
		return nil, fmt.Errorf("no workflow runs found")
	}

	// Try to find the job in the latest successful run
	for _, run := range runs {
		if run.Status != "completed" || run.Conclusion != "success" {
			continue
		}

		duration, err := c.getJobDurationFromRun(ctx, run.ID, jobName)
		if err != nil {
			// Continue to next run if job not found in this run
			continue
		}
		return duration, nil
	}

	return nil, fmt.Errorf("no successful run found with job %s", jobName)
}

// workflowRun represents a workflow run
type workflowRun struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// workflowRunsResponse represents the response from workflow runs API
type workflowRunsResponse struct {
	WorkflowRuns []workflowRun `json:"workflow_runs"`
}

// job represents a job in a workflow run
type job struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
}

// jobsResponse represents the response from jobs API
type jobsResponse struct {
	Jobs []job `json:"jobs"`
}

// getJobDurationFromRun gets the duration of a specific job from a workflow run
func (c *Client) getJobDurationFromRun(ctx context.Context, runID int64, jobName string) (*JobDuration, error) {
	path := fmt.Sprintf("repos/%s/%s/actions/runs/%d/jobs", c.owner, c.repo, runID)

	var response jobsResponse
	err := c.restClient.Get(path, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch jobs: %w", err)
	}

	// Try to match job by name (case-insensitive)
	// GitHub API returns the display name, which might differ from the job ID
	for _, j := range response.Jobs {
		// Match by exact name or case-insensitive match
		if strings.EqualFold(j.Name, jobName) || j.Name == jobName {
			if j.StartedAt == "" || j.CompletedAt == "" {
				return nil, fmt.Errorf("job %s has incomplete timing information", jobName)
			}

			startTime, err := time.Parse(time.RFC3339, j.StartedAt)
			if err != nil {
				return nil, fmt.Errorf("failed to parse start time: %w", err)
			}

			completedTime, err := time.Parse(time.RFC3339, j.CompletedAt)
			if err != nil {
				return nil, fmt.Errorf("failed to parse completed time: %w", err)
			}

			duration := completedTime.Sub(startTime)

			return &JobDuration{
				JobName:  jobName,
				Duration: duration,
			}, nil
		}
	}

	return nil, fmt.Errorf("job %s not found in run %d", jobName, runID)
}

// GetRepoInfo gets repository owner and name from git remote
func GetRepoInfo() (host, owner, repo string, err error) {
	// Try to get from git remote
	cmd := exec.Command("git", "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get git remote: %w", err)
	}

	remoteURL := strings.TrimSpace(string(output))

	// Parse git remote URL
	// Support formats:
	// - https://github.com/owner/repo.git
	// - git@github.com:owner/repo.git
	// - https://github.com/owner/repo
	// - git@github.com:owner/repo

	host = "github.com"

	if strings.HasPrefix(remoteURL, "https://") {
		// https://github.com/owner/repo.git or https://github.com/owner/repo
		parts := strings.Split(strings.TrimPrefix(remoteURL, "https://"), "/")
		if len(parts) >= 3 {
			host = parts[0]
			owner = parts[1]
			repo = strings.TrimSuffix(parts[2], ".git")
		}
	} else if strings.HasPrefix(remoteURL, "git@") {
		// git@github.com:owner/repo.git or git@github.com:owner/repo
		parts := strings.Split(strings.TrimPrefix(remoteURL, "git@"), ":")
		if len(parts) == 2 {
			host = parts[0]
			repoPath := strings.TrimSuffix(parts[1], ".git")
			repoParts := strings.Split(repoPath, "/")
			if len(repoParts) >= 2 {
				owner = repoParts[0]
				repo = repoParts[1]
			}
		}
	}

	if owner == "" || repo == "" {
		return "", "", "", fmt.Errorf("failed to parse repository info from remote: %s", remoteURL)
	}

	return host, owner, repo, nil
}

// getWorkflowRuns gets workflow runs for a specific workflow file
func (c *Client) getWorkflowRuns(_ context.Context, workflowPath string) ([]workflowRun, error) {
	// Use the full workflow path (e.g., ".github/workflows/ci.yaml")
	// GitHub API accepts both workflow ID and workflow path
	// URL encode the path for the API call
	encodedPath := strings.ReplaceAll(workflowPath, "/", "%2F")
	path := fmt.Sprintf("repos/%s/%s/actions/workflows/%s/runs?per_page=10", c.owner, c.repo, encodedPath)

	var response workflowRunsResponse
	err := c.restClient.Get(path, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch workflow runs: %w", err)
	}

	return response.WorkflowRuns, nil
}
