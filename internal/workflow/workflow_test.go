package workflow

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// loadTestData reads a file from the testdata directory
// It finds the testdata directory relative to the test file location
func loadTestData(t *testing.T, filename string) string {
	t.Helper()
	// Get the directory of this test file
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("Failed to get caller information")
	}
	testDir := filepath.Dir(testFile)
	testDataPath := filepath.Join(testDir, "testdata", filename)
	data, err := os.ReadFile(testDataPath)
	if err != nil {
		t.Fatalf("Failed to read testdata file %s: %v", filename, err)
	}
	return string(data)
}

func TestLoadWorkflow_Basic(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantJobs []string
		wantErr  bool
	}{
		{
			name:     "single job",
			filename: "single-job.yml",
			wantJobs: []string{"test"},
			wantErr:  false,
		},
		{
			name:     "multiple jobs",
			filename: "multiple-jobs.yml",
			wantJobs: []string{"job1", "job2"},
			wantErr:  false,
		},
		{
			name:     "job with services",
			filename: "job-with-services.yml",
			wantJobs: []string{"test"},
			wantErr:  false,
		},
		{
			name:     "job with docker action",
			filename: "job-with-docker-action.yml",
			wantJobs: []string{"build"},
			wantErr:  false,
		},
		{
			name:     "invalid YAML",
			filename: "invalid.yml",
			wantJobs: nil,
			wantErr:  true,
		},
		{
			name:     "no jobs section",
			filename: "no-jobs.yml",
			wantJobs: []string{},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Load testdata file
			content := loadTestData(t, tt.filename)

			// Create temporary file
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "workflow.yml")
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Load workflow
			wf, err := LoadWorkflow(filePath)

			if tt.wantErr {
				if err == nil {
					t.Errorf("LoadWorkflow() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("LoadWorkflow() unexpected error: %v", err)
				return
			}

			if wf == nil {
				t.Fatal("LoadWorkflow() returned nil workflow")
			}

			if wf.Path != filePath {
				t.Errorf("LoadWorkflow() Path = %v, want %v", wf.Path, filePath)
			}

			if len(wf.Jobs) != len(tt.wantJobs) {
				t.Errorf("LoadWorkflow() Jobs count = %d, want %d", len(wf.Jobs), len(tt.wantJobs))
			}

			for _, jobName := range tt.wantJobs {
				if _, ok := wf.Jobs[jobName]; !ok {
					t.Errorf("LoadWorkflow() missing job: %s", jobName)
				}
			}
		})
	}
}

func TestLoadWorkflow_LineNumbers(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		jobName      string
		wantLineNum  int
		wantLineText string
	}{
		{
			name:         "simple job",
			filename:     "simple-job.yml",
			jobName:      "test",
			wantLineNum:  5,
			wantLineText: "    runs-on: ubuntu-latest",
		},
		{
			name:         "job with permissions",
			filename:     "job-with-permissions.yml",
			jobName:      "build",
			wantLineNum:  7,
			wantLineText: "    runs-on: ubuntu-latest",
		},
		{
			name:         "multiple jobs",
			filename:     "multiple-jobs.yml",
			jobName:      "job2",
			wantLineNum:  9,
			wantLineText: "    runs-on: ubuntu-22.04",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Load testdata file
			content := loadTestData(t, tt.filename)

			// Create temporary file
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "workflow.yml")
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Load workflow
			wf, err := LoadWorkflow(filePath)
			if err != nil {
				t.Fatalf("LoadWorkflow() error: %v", err)
			}

			job, ok := wf.Jobs[tt.jobName]
			if !ok {
				t.Fatalf("Job %s not found", tt.jobName)
			}

			if job.LineStart != tt.wantLineNum {
				t.Errorf("Job %s LineStart = %d, want %d", tt.jobName, job.LineStart, tt.wantLineNum)
			}

			// Verify the line content matches
			lines := strings.Split(content, "\n")
			if job.LineStart > 0 && job.LineStart <= len(lines) {
				actualLine := strings.TrimSpace(lines[job.LineStart-1])
				wantLine := strings.TrimSpace(tt.wantLineText)
				if actualLine != wantLine {
					t.Errorf("Line %d content = %q, want %q", job.LineStart, actualLine, wantLine)
				}
			}
		})
	}
}

func TestLoadWorkflows_Basic(t *testing.T) {
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

	// Copy testdata workflow files
	testFiles := []string{"workflow1.yml", "workflow2.yaml", "workflow3.yml"}
	for _, filename := range testFiles {
		content := loadTestData(t, filename)
		filePath := filepath.Join(workflowDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", filename, err)
		}
	}

	// Load workflows
	loaded, err := LoadWorkflows()
	if err != nil {
		t.Fatalf("LoadWorkflows() error: %v", err)
	}

	if len(loaded) != len(testFiles) {
		t.Errorf("LoadWorkflows() returned %d workflows, want %d", len(loaded), len(testFiles))
	}

	// Verify all workflows are loaded
	loadedPaths := make(map[string]bool)
	for _, wf := range loaded {
		loadedPaths[filepath.Base(wf.Path)] = true
	}

	for _, filename := range testFiles {
		if !loadedPaths[filename] {
			t.Errorf("LoadWorkflows() missing workflow: %s", filename)
		}
	}
}

func TestLoadWorkflows_NoDirectory(t *testing.T) {
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

	// Try to load workflows from non-existent directory
	_, err = LoadWorkflows()
	if err == nil {
		t.Error("LoadWorkflows() expected error when directory doesn't exist")
	}
}

func TestLoadWorkflows_InvalidFile(t *testing.T) {
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

	// Copy valid workflow from testdata
	validContent := loadTestData(t, "valid.yml")
	validFile := filepath.Join(workflowDir, "valid.yml")
	if err := os.WriteFile(validFile, []byte(validContent), 0644); err != nil {
		t.Fatalf("Failed to write valid file: %v", err)
	}

	// Copy invalid YAML file from testdata
	invalidContent := loadTestData(t, "invalid.yml")
	invalidFile := filepath.Join(workflowDir, "invalid.yml")
	if err := os.WriteFile(invalidFile, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("Failed to write invalid file: %v", err)
	}

	// Load workflows - should succeed but skip invalid file
	loaded, err := LoadWorkflows()
	if err != nil {
		t.Errorf("LoadWorkflows() unexpected error: %v", err)
	}

	// Should load at least the valid workflow
	if len(loaded) == 0 {
		t.Error("LoadWorkflows() should load at least valid workflow")
	}

	// Verify valid workflow is loaded
	found := false
	for _, wf := range loaded {
		if filepath.Base(wf.Path) == "valid.yml" {
			found = true
			break
		}
	}
	if !found {
		t.Error("LoadWorkflows() should load valid.yml")
	}
}

func TestJob_IsUbuntuLatest(t *testing.T) {
	tests := []struct {
		name     string
		job      *Job
		expected bool
	}{
		{
			name: "ubuntu-latest string",
			job: &Job{
				RunsOn: "ubuntu-latest",
			},
			expected: true,
		},
		{
			name: "ubuntu-22.04 string",
			job: &Job{
				RunsOn: "ubuntu-22.04",
			},
			expected: false,
		},
		{
			name: "nil runs-on",
			job: &Job{
				RunsOn: nil,
			},
			expected: false,
		},
		{
			name: "array with ubuntu-latest",
			job: &Job{
				RunsOn: []interface{}{"ubuntu-latest"},
			},
			expected: true,
		},
		{
			name: "array without ubuntu-latest",
			job: &Job{
				RunsOn: []interface{}{"ubuntu-22.04", "macos-latest"},
			},
			expected: false,
		},
		{
			name: "array with mixed types",
			job: &Job{
				RunsOn: []interface{}{"ubuntu-22.04", "ubuntu-latest", 123},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.job.IsUbuntuLatest()
			if got != tt.expected {
				t.Errorf("IsUbuntuLatest() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestJob_HasDockerCommands(t *testing.T) {
	tests := []struct {
		name     string
		job      *Job
		expected bool
	}{
		{
			name: "no docker commands",
			job: &Job{
				Steps: []Step{{Run: "echo hello"}},
			},
			expected: false,
		},
		{
			name: "docker build",
			job: &Job{
				Steps: []Step{{Run: "docker build -t app ."}},
			},
			expected: true,
		},
		{
			name: "docker run",
			job: &Job{
				Steps: []Step{{Run: "docker run myapp"}},
			},
			expected: true,
		},
		{
			name: "docker compose",
			job: &Job{
				Steps: []Step{{Run: "docker compose up"}},
			},
			expected: true,
		},
		{
			name: "docker-compose",
			job: &Job{
				Steps: []Step{{Run: "docker-compose up"}},
			},
			expected: true,
		},
		{
			name: "case insensitive",
			job: &Job{
				Steps: []Step{{Run: "DOCKER BUILD -t app ."}},
			},
			expected: true,
		},
		{
			name: "multiple steps without docker",
			job: &Job{
				Steps: []Step{
					{Run: "npm install"},
					{Run: "npm test"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.job.HasDockerCommands()
			if got != tt.expected {
				t.Errorf("HasDockerCommands() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestJob_HasDockerActions(t *testing.T) {
	tests := []struct {
		name     string
		job      *Job
		expected bool
	}{
		{
			name: "no docker actions",
			job: &Job{
				Steps: []Step{{Uses: "actions/checkout@v4"}},
			},
			expected: false,
		},
		{
			name: "docker:// image",
			job: &Job{
				Steps: []Step{{Uses: "docker://alpine:latest"}},
			},
			expected: true,
		},
		{
			name: "docker/ organization",
			job: &Job{
				Steps: []Step{{Uses: "docker/custom-action@v1"}},
			},
			expected: true,
		},
		{
			name: "docker/build-push-action",
			job: &Job{
				Steps: []Step{{Uses: "docker/build-push-action@v6"}},
			},
			expected: true,
		},
		{
			name: "docker/login-action",
			job: &Job{
				Steps: []Step{{Uses: "docker/login-action@v3"}},
			},
			expected: true,
		},
		{
			name: "docker/setup-buildx-action",
			job: &Job{
				Steps: []Step{{Uses: "docker/setup-buildx-action@v3"}},
			},
			expected: true,
		},
		{
			name: "docker/setup-qemu-action",
			job: &Job{
				Steps: []Step{{Uses: "docker/setup-qemu-action@v3"}},
			},
			expected: true,
		},
		{
			name: "standard actions",
			job: &Job{
				Steps: []Step{
					{Uses: "actions/checkout@v4"},
					{Uses: "actions/setup-go@v5"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.job.HasDockerActions()
			if got != tt.expected {
				t.Errorf("HasDockerActions() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestJob_HasServices(t *testing.T) {
	tests := []struct {
		name     string
		job      *Job
		expected bool
	}{
		{
			name: "no services",
			job: &Job{
				Services: nil,
			},
			expected: false,
		},
		{
			name: "has services",
			job: &Job{
				Services: map[string]any{"postgres": map[string]any{}},
			},
			expected: true,
		},
		{
			name: "empty services map",
			job: &Job{
				Services: map[string]any{},
			},
			expected: true, // Non-nil means services are defined
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.job.HasServices()
			if got != tt.expected {
				t.Errorf("HasServices() = %v, want %v", got, tt.expected)
			}
		})
	}
}
