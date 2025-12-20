package scan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fchimpan/gh-slimify/internal/workflow"
)

func TestIsEligible_UbuntuLatest(t *testing.T) {
	tests := []struct {
		name     string
		job      *workflow.Job
		expected bool
	}{
		{
			name: "eligible job with ubuntu-latest",
			job: &workflow.Job{
				RunsOn:   "ubuntu-latest",
				Steps:    []workflow.Step{{Run: "echo hello"}},
				Services: nil,
			},
			expected: true,
		},
		{
			name: "not eligible - runs on ubuntu-22.04",
			job: &workflow.Job{
				RunsOn:   "ubuntu-22.04",
				Steps:    []workflow.Step{{Run: "echo hello"}},
				Services: nil,
			},
			expected: false,
		},
		{
			name: "not eligible - runs-on is nil",
			job: &workflow.Job{
				RunsOn:   nil,
				Steps:    []workflow.Step{{Run: "echo hello"}},
				Services: nil,
			},
			expected: false,
		},
		{
			name: "not eligible - uses docker build",
			job: &workflow.Job{
				RunsOn:   "ubuntu-latest",
				Steps:    []workflow.Step{{Run: "docker build -t myapp ."}},
				Services: nil,
			},
			expected: false,
		},
		{
			name: "not eligible - uses docker run",
			job: &workflow.Job{
				RunsOn:   "ubuntu-latest",
				Steps:    []workflow.Step{{Run: "docker run myapp"}},
				Services: nil,
			},
			expected: false,
		},
		{
			name: "not eligible - uses docker compose",
			job: &workflow.Job{
				RunsOn:   "ubuntu-latest",
				Steps:    []workflow.Step{{Run: "docker compose up"}},
				Services: nil,
			},
			expected: false,
		},
		{
			name: "not eligible - uses docker-compose",
			job: &workflow.Job{
				RunsOn:   "ubuntu-latest",
				Steps:    []workflow.Step{{Run: "docker-compose up"}},
				Services: nil,
			},
			expected: false,
		},
		{
			name: "not eligible - uses docker action",
			job: &workflow.Job{
				RunsOn:   "ubuntu-latest",
				Steps:    []workflow.Step{{Uses: "docker://alpine:latest"}},
				Services: nil,
			},
			expected: false,
		},
		{
			name: "not eligible - uses docker/build-push-action",
			job: &workflow.Job{
				RunsOn:   "ubuntu-latest",
				Steps:    []workflow.Step{{Uses: "docker/build-push-action@v6"}},
				Services: nil,
			},
			expected: false,
		},
		{
			name: "not eligible - uses docker/login-action",
			job: &workflow.Job{
				RunsOn:   "ubuntu-latest",
				Steps:    []workflow.Step{{Uses: "docker/login-action@v3"}},
				Services: nil,
			},
			expected: false,
		},
		{
			name: "not eligible - uses docker/setup-buildx-action",
			job: &workflow.Job{
				RunsOn:   "ubuntu-latest",
				Steps:    []workflow.Step{{Uses: "docker/setup-buildx-action@v3"}},
				Services: nil,
			},
			expected: false,
		},
		{
			name: "not eligible - uses docker/setup-qemu-action",
			job: &workflow.Job{
				RunsOn:   "ubuntu-latest",
				Steps:    []workflow.Step{{Uses: "docker/setup-qemu-action@v3"}},
				Services: nil,
			},
			expected: false,
		},
		{
			name: "not eligible - uses docker/ organization action",
			job: &workflow.Job{
				RunsOn:   "ubuntu-latest",
				Steps:    []workflow.Step{{Uses: "docker/custom-action@v1"}},
				Services: nil,
			},
			expected: false,
		},
		{
			name: "eligible - uses standard actions",
			job: &workflow.Job{
				RunsOn: "ubuntu-latest",
				Steps: []workflow.Step{
					{Uses: "actions/checkout@v4"},
					{Uses: "actions/setup-go@v5"},
				},
				Services: nil,
			},
			expected: true,
		},
		{
			name: "not eligible - has services",
			job: &workflow.Job{
				RunsOn:   "ubuntu-latest",
				Steps:    []workflow.Step{{Run: "echo hello"}},
				Services: map[string]interface{}{"postgres": map[string]interface{}{}},
			},
			expected: false,
		},
		{
			name: "not eligible - has container",
			job: &workflow.Job{
				RunsOn:    "ubuntu-latest",
				Steps:     []workflow.Step{{Run: "node --version"}},
				Services:  nil,
				Container: "node:18",
			},
			expected: false,
		},
		{
			name: "not eligible - has container with map",
			job: &workflow.Job{
				RunsOn:    "ubuntu-latest",
				Steps:     []workflow.Step{{Run: "node --version"}},
				Services:  nil,
				Container: map[string]interface{}{"image": "node:18"},
			},
			expected: false,
		},
		{
			name: "eligible - case insensitive docker check",
			job: &workflow.Job{
				RunsOn:   "ubuntu-latest",
				Steps:    []workflow.Step{{Run: "echo DOCKER_BUILD"}}, // Should not match
				Services: nil,
			},
			expected: true,
		},
		{
			name: "not eligible - docker command in multi-line script",
			job: &workflow.Job{
				RunsOn: "ubuntu-latest",
				Steps: []workflow.Step{{
					Run: `#!/bin/bash
echo "Building"
docker build -t app .
echo "Done"`,
				}},
				Services: nil,
			},
			expected: false,
		},
		{
			name: "eligible - multiple steps without docker",
			job: &workflow.Job{
				RunsOn: "ubuntu-latest",
				Steps: []workflow.Step{
					{Uses: "actions/checkout@v4"},
					{Run: "npm install"},
					{Run: "npm test"},
				},
				Services: nil,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEligible(tt.job)
			if got != tt.expected {
				t.Errorf("isEligible() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsEligible_MatrixStrategy(t *testing.T) {
	tests := []struct {
		name     string
		job      *workflow.Job
		expected bool
	}{
		{
			name: "eligible - matrix with ubuntu-latest",
			job: &workflow.Job{
				RunsOn:   []interface{}{"ubuntu-latest"},
				Steps:    []workflow.Step{{Run: "echo hello"}},
				Services: nil,
			},
			expected: true,
		},
		{
			name: "not eligible - matrix without ubuntu-latest",
			job: &workflow.Job{
				RunsOn:   []interface{}{"ubuntu-22.04", "macos-latest"},
				Steps:    []workflow.Step{{Run: "echo hello"}},
				Services: nil,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEligible(tt.job)
			if got != tt.expected {
				t.Errorf("isEligible() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestScan_Integration(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	workflowDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("Failed to create workflow directory: %v", err)
	}

	// Save original working directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Change to temporary directory
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}
	defer func() {
		os.Chdir(originalWd)
	}()

	tests := []struct {
		name          string
		filename      string
		content       string
		expectedCount int
		expectedJobs  []string
		expectError   bool
	}{
		{
			name:     "single eligible job",
			filename: "test.yml",
			content: `name: test
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "hello"`,
			expectedCount: 1,
			expectedJobs:  []string{"test"},
			expectError:   false,
		},
		{
			name:     "job with docker - not eligible",
			filename: "docker.yml",
			content: `name: docker
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: docker build -t app .`,
			expectedCount: 0,
			expectedJobs:  []string{},
			expectError:   false,
		},
		{
			name:     "job with docker action - not eligible",
			filename: "docker-action.yml",
			content: `name: docker-action
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: docker/build-push-action@v6
        with:
          context: .
          push: true`,
			expectedCount: 0,
			expectedJobs:  []string{},
			expectError:   false,
		},
		{
			name:     "job with docker:// image - not eligible",
			filename: "docker-image.yml",
			content: `name: docker-image
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: docker://alpine:latest
        with:
          args: echo "test"`,
			expectedCount: 0,
			expectedJobs:  []string{},
			expectError:   false,
		},
		{
			name:     "job with container - not eligible",
			filename: "container.yml",
			content: `name: container
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    container:
      image: node:18
    steps:
      - run: node --version`,
			expectedCount: 0,
			expectedJobs:  []string{},
			expectError:   false,
		},
		{
			name:     "multiple jobs - mixed eligibility",
			filename: "mixed.yml",
			content: `name: mixed
on: push
jobs:
  eligible:
    runs-on: ubuntu-latest
    steps:
      - run: echo "hello"
  not-eligible-docker:
    runs-on: ubuntu-latest
    steps:
      - run: docker build .
  not-eligible-runner:
    runs-on: ubuntu-22.04
    steps:
      - run: echo "hello"
  not-eligible-container:
    runs-on: ubuntu-latest
    container:
      image: node:18
    steps:
      - run: node --version`,
			expectedCount: 1,
			expectedJobs:  []string{"eligible"},
			expectError:   false,
		},
		{
			name:     "no jobs section",
			filename: "no-jobs.yml",
			content: `name: no-jobs
on: push`,
			expectedCount: 0,
			expectedJobs:  []string{},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up previous test files
			files, _ := filepath.Glob(filepath.Join(workflowDir, "*.yml"))
			for _, f := range files {
				os.Remove(f)
			}

			// Write test workflow file
			filePath := filepath.Join(workflowDir, tt.filename)
			if err := os.WriteFile(filePath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Run Scan (skip duration for tests to avoid API calls)
			result, err := Scan(true, false)

			if tt.expectError && err == nil {
				t.Errorf("Scan() expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Scan() unexpected error: %v", err)
			}

			if result == nil {
				if tt.expectedCount > 0 {
					t.Errorf("Scan() returned nil result, expected %d candidates", tt.expectedCount)
				}
				return
			}

			candidates := result.Candidates
			if len(candidates) != tt.expectedCount {
				t.Errorf("Scan() returned %d candidates, want %d", len(candidates), tt.expectedCount)
			}

			// Check job names
			if len(candidates) > 0 {
				jobNames := make(map[string]bool)
				for _, c := range candidates {
					jobNames[c.JobName] = true
				}

				for _, expectedJob := range tt.expectedJobs {
					if !jobNames[expectedJob] {
						t.Errorf("Scan() missing expected job: %s", expectedJob)
					}
				}

				if len(jobNames) != len(tt.expectedJobs) {
					t.Errorf("Scan() returned %d unique jobs, want %d", len(jobNames), len(tt.expectedJobs))
				}
			}
		})
	}
}

func TestScan_NoWorkflowDirectory(t *testing.T) {
	// Create a temporary directory without .github/workflows
	tmpDir := t.TempDir()

	// Save original working directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Change to temporary directory
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}
	defer func() {
		os.Chdir(originalWd)
	}()

	result, err := Scan(true, false)
	if err == nil {
		t.Error("Scan() expected error when workflow directory doesn't exist")
	}
	if result != nil {
		t.Errorf("Scan() expected nil result, got %v", result)
	}
}

func TestIsEligible_AlreadySlim(t *testing.T) {
	tests := []struct {
		name           string
		job            *workflow.Job
		expectedSlim   bool
		expectedEligible bool
	}{
		{
			name: "already using ubuntu-slim",
			job: &workflow.Job{
				RunsOn: "ubuntu-slim",
				Steps:  []workflow.Step{{Run: "echo hello"}},
			},
			expectedSlim:     true,
			expectedEligible: false,
		},
		{
			name: "ubuntu-slim in matrix",
			job: &workflow.Job{
				RunsOn: []interface{}{"ubuntu-slim"},
				Steps:  []workflow.Step{{Run: "echo hello"}},
			},
			expectedSlim:     true,
			expectedEligible: false,
		},
		{
			name: "ubuntu-latest - eligible",
			job: &workflow.Job{
				RunsOn: "ubuntu-latest",
				Steps:  []workflow.Step{{Run: "echo hello"}},
			},
			expectedSlim:     false,
			expectedEligible: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSlim := tt.job.IsUbuntuSlim()
			if gotSlim != tt.expectedSlim {
				t.Errorf("IsUbuntuSlim() = %v, want %v", gotSlim, tt.expectedSlim)
			}

			if !gotSlim {
				gotEligible := isEligible(tt.job)
				if gotEligible != tt.expectedEligible {
					t.Errorf("isEligible() = %v, want %v", gotEligible, tt.expectedEligible)
				}
			}
		})
	}
}

func TestScan_AlreadySlimJobs(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	workflowDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("Failed to create workflow directory: %v", err)
	}

	// Save original working directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Change to temporary directory
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}
	defer func() {
		os.Chdir(originalWd)
	}()

	// Create a workflow with an already-slim job
	workflowContent := `name: test
on: push
jobs:
  already-slim:
    runs-on: ubuntu-slim
    steps:
      - run: echo "already slim"
  eligible:
    runs-on: ubuntu-latest
    steps:
      - run: echo "can migrate"
  ineligible:
    runs-on: ubuntu-22.04
    steps:
      - run: echo "cannot migrate"`

	workflowPath := filepath.Join(workflowDir, "test.yml")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	result, err := Scan(true, false)
	if err != nil {
		t.Fatalf("Scan() returned error: %v", err)
	}

	if result == nil {
		t.Fatal("Scan() returned nil result")
	}

	// Check candidates (should have 1 eligible job)
	if len(result.Candidates) != 1 {
		t.Errorf("Expected 1 candidate, got %d", len(result.Candidates))
	}
	if len(result.Candidates) > 0 && result.Candidates[0].JobID != "eligible" {
		t.Errorf("Expected eligible job, got %s", result.Candidates[0].JobID)
	}

	// Check ineligible jobs (should have 1 ineligible job)
	if len(result.IneligibleJobs) != 1 {
		t.Errorf("Expected 1 ineligible job, got %d", len(result.IneligibleJobs))
	}
	if len(result.IneligibleJobs) > 0 && result.IneligibleJobs[0].JobID != "ineligible" {
		t.Errorf("Expected ineligible job, got %s", result.IneligibleJobs[0].JobID)
	}

	// Check already slim jobs (should have 1 already-slim job)
	if len(result.AlreadySlimJobs) != 1 {
		t.Errorf("Expected 1 already-slim job, got %d", len(result.AlreadySlimJobs))
	}
	if len(result.AlreadySlimJobs) > 0 && result.AlreadySlimJobs[0].JobID != "already-slim" {
		t.Errorf("Expected already-slim job, got %s", result.AlreadySlimJobs[0].JobID)
	}
}
