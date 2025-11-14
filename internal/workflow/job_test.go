package workflow

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestJob_IsUbuntuLatest_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		job      *Job
		expected bool
	}{
		{
			name: "empty string",
			job: &Job{
				RunsOn: "",
			},
			expected: false,
		},
		{
			name: "ubuntu-latest with whitespace",
			job: &Job{
				RunsOn: "  ubuntu-latest  ",
			},
			expected: false, // Exact match required
		},
		{
			name: "ubuntu-latest-extra",
			job: &Job{
				RunsOn: "ubuntu-latest-extra",
			},
			expected: false,
		},
		{
			name: "empty array",
			job: &Job{
				RunsOn: []interface{}{},
			},
			expected: false,
		},
		{
			name: "array with non-string items",
			job: &Job{
				RunsOn: []interface{}{123, true, nil},
			},
			expected: false,
		},
		{
			name: "array with ubuntu-latest at end",
			job: &Job{
				RunsOn: []interface{}{"ubuntu-22.04", "macos-latest", "ubuntu-latest"},
			},
			expected: true,
		},
		{
			name: "unsupported type - int",
			job: &Job{
				RunsOn: 123,
			},
			expected: false,
		},
		{
			name: "unsupported type - map",
			job: &Job{
				RunsOn: map[string]interface{}{"os": "ubuntu-latest"},
			},
			expected: false,
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

func TestJob_HasDockerCommands_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		job      *Job
		expected bool
	}{
		{
			name: "empty steps",
			job: &Job{
				Steps: []Step{},
			},
			expected: false,
		},
		{
			name: "step with empty run",
			job: &Job{
				Steps: []Step{{Run: ""}},
			},
			expected: false,
		},
		{
			name: "docker command in comment",
			job: &Job{
				Steps: []Step{{Run: "# docker build should not match"}},
			},
			expected: true, // Current implementation detects docker in comments too
		},
		{
			name: "docker command with prefix",
			job: &Job{
				Steps: []Step{{Run: "sudo docker build -t app ."}},
			},
			expected: true,
		},
		{
			name: "docker command in multi-line script",
			job: &Job{
				Steps: []Step{{
					Run: `#!/bin/bash
set -e
echo "Building"
docker build -t app .
echo "Done"`,
				}},
			},
			expected: true,
		},
		{
			name: "docker command in variable",
			job: &Job{
				Steps: []Step{{Run: "CMD='docker build'; $CMD"}},
			},
			expected: true,
		},
		{
			name: "docker exec",
			job: &Job{
				Steps: []Step{{Run: "docker exec container echo hello"}},
			},
			expected: true,
		},
		{
			name: "docker ps",
			job: &Job{
				Steps: []Step{{Run: "docker ps -a"}},
			},
			expected: true,
		},
		{
			name: "docker pull",
			job: &Job{
				Steps: []Step{{Run: "docker pull alpine:latest"}},
			},
			expected: true,
		},
		{
			name: "docker push",
			job: &Job{
				Steps: []Step{{Run: "docker push myregistry/app:latest"}},
			},
			expected: true,
		},
		{
			name: "docker tag",
			job: &Job{
				Steps: []Step{{Run: "docker tag app:latest app:v1"}},
			},
			expected: true,
		},
		{
			name: "docker login",
			job: &Job{
				Steps: []Step{{Run: "docker login -u user -p pass registry.com"}},
			},
			expected: true,
		},
		{
			name: "word containing docker but not command",
			job: &Job{
				Steps: []Step{{Run: "echo 'This is not a docker command'"}},
			},
			expected: false,
		},
		{
			name: "dockerhub",
			job: &Job{
				Steps: []Step{{Run: "echo 'dockerhub.com'"}},
			},
			expected: false,
		},
		{
			name: "multiple steps - first has docker",
			job: &Job{
				Steps: []Step{
					{Run: "docker build -t app ."},
					{Run: "echo 'done'"},
				},
			},
			expected: true,
		},
		{
			name: "multiple steps - last has docker",
			job: &Job{
				Steps: []Step{
					{Run: "echo 'start'"},
					{Run: "docker build -t app ."},
				},
			},
			expected: true,
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

func TestJob_HasContainerActions_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		job      *Job
		expected bool
	}{
		{
			name: "empty steps",
			job: &Job{
				Steps: []Step{},
			},
			expected: false,
		},
		{
			name: "step with empty uses",
			job: &Job{
				Steps: []Step{{Uses: ""}},
			},
			expected: false,
		},
		{
			name: "step without uses field",
			job: &Job{
				Steps: []Step{{Run: "echo hello"}},
			},
			expected: false,
		},
		{
			name: "docker:// with tag",
			job: &Job{
				Steps: []Step{{Uses: "docker://alpine:3.18"}},
			},
			expected: true,
		},
		{
			name: "docker:// without tag",
			job: &Job{
				Steps: []Step{{Uses: "docker://alpine"}},
			},
			expected: true,
		},
		{
			name: "docker:// with digest",
			job: &Job{
				Steps: []Step{{Uses: "docker://alpine@sha256:abc123"}},
			},
			expected: true,
		},
		{
			name: "docker/ with version",
			job: &Job{
				Steps: []Step{{Uses: "docker/build-push-action@v6.0.0"}},
			},
			expected: true,
		},
		{
			name: "docker/ with branch",
			job: &Job{
				Steps: []Step{{Uses: "docker/build-push-action@main"}},
			},
			expected: true,
		},
		{
			name: "docker/ with commit SHA",
			job: &Job{
				Steps: []Step{{Uses: "docker/build-push-action@abc123def456"}},
			},
			expected: true,
		},
		{
			name: "docker/build-push-action without version",
			job: &Job{
				Steps: []Step{{Uses: "docker/build-push-action"}},
			},
			expected: true,
		},
		{
			name: "docker/login-action with version",
			job: &Job{
				Steps: []Step{{Uses: "docker/login-action@v3.0.0"}},
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
			name: "action containing docker but not docker/",
			job: &Job{
				Steps: []Step{{Uses: "myorg/docker-helper-action@v1"}},
			},
			expected: false,
		},
		{
			name: "action with docker in name but not prefix",
			job: &Job{
				Steps: []Step{{Uses: "myorg/build-docker-action@v1"}},
			},
			expected: false,
		},
		{
			name: "multiple steps - first has docker action",
			job: &Job{
				Steps: []Step{
					{Uses: "docker/build-push-action@v6"},
					{Uses: "actions/checkout@v4"},
				},
			},
			expected: true,
		},
		{
			name: "multiple steps - last has docker action",
			job: &Job{
				Steps: []Step{
					{Uses: "actions/checkout@v4"},
					{Uses: "docker/build-push-action@v6"},
				},
			},
			expected: true,
		},
		{
			name: "docker action in middle of steps",
			job: &Job{
				Steps: []Step{
					{Uses: "actions/checkout@v4"},
					{Uses: "docker/login-action@v3"},
					{Run: "echo done"},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.job.HasContainerActions()
			if got != tt.expected {
				t.Errorf("HasContainerActions() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestJob_HasServices_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		job      *Job
		expected bool
	}{
		{
			name: "nil services",
			job: &Job{
				Services: nil,
			},
			expected: false,
		},
		{
			name: "empty map",
			job: &Job{
				Services: map[string]any{},
			},
			expected: true, // Empty map is still defined
		},
		{
			name: "services with single service",
			job: &Job{
				Services: map[string]any{
					"postgres": map[string]any{
						"image": "postgres:14",
					},
				},
			},
			expected: true,
		},
		{
			name: "services with multiple services",
			job: &Job{
				Services: map[string]any{
					"postgres": map[string]any{
						"image": "postgres:14",
					},
					"redis": map[string]any{
						"image": "redis:7",
					},
				},
			},
			expected: true,
		},
		{
			name: "services with empty service config",
			job: &Job{
				Services: map[string]any{
					"postgres": map[string]any{},
				},
			},
			expected: true,
		},
		{
			name: "services as array",
			job: &Job{
				Services: []interface{}{"postgres", "redis"},
			},
			expected: true, // Non-nil means services are defined
		},
		{
			name: "services as string",
			job: &Job{
				Services: "postgres",
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

func TestJob_HasContainer_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		job      *Job
		expected bool
	}{
		{
			name: "nil container",
			job: &Job{
				Container: nil,
			},
			expected: false,
		},
		{
			name: "container with image string",
			job: &Job{
				Container: "node:18",
			},
			expected: true,
		},
		{
			name: "container with image map",
			job: &Job{
				Container: map[string]any{
					"image": "node:18",
				},
			},
			expected: true,
		},
		{
			name: "container with image and options",
			job: &Job{
				Container: map[string]any{
					"image": "node:18",
					"env": map[string]string{
						"NODE_ENV": "test",
					},
					"ports": []int{3000},
				},
			},
			expected: true,
		},
		{
			name: "container with credentials",
			job: &Job{
				Container: map[string]any{
					"image": "ghcr.io/private/image:latest",
					"credentials": map[string]string{
						"username": "${{ secrets.USERNAME }}",
						"password": "${{ secrets.PASSWORD }}",
					},
				},
			},
			expected: true,
		},
		{
			name: "empty container map",
			job: &Job{
				Container: map[string]any{},
			},
			expected: true, // Non-nil means container is defined
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.job.HasContainer()
			if got != tt.expected {
				t.Errorf("HasContainer() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestJob_CombinedChecks(t *testing.T) {
	tests := []struct {
		name           string
		job            *Job
		wantUbuntu     bool
		wantDockerCmd  bool
		wantDockerAct  bool
		wantServices   bool
		wantContainer  bool
	}{
		{
			name: "fully eligible job",
			job: &Job{
				RunsOn:    "ubuntu-latest",
				Steps:     []Step{{Run: "echo hello"}},
				Services:  nil,
				Container: nil,
			},
			wantUbuntu:    true,
			wantDockerCmd: false,
			wantDockerAct: false,
			wantServices:  false,
			wantContainer: false,
		},
		{
			name: "job with docker command",
			job: &Job{
				RunsOn:    "ubuntu-latest",
				Steps:     []Step{{Run: "docker build -t app ."}},
				Services:  nil,
				Container: nil,
			},
			wantUbuntu:    true,
			wantDockerCmd: true,
			wantDockerAct: false,
			wantServices:  false,
			wantContainer: false,
		},
		{
			name: "job with docker action",
			job: &Job{
				RunsOn:    "ubuntu-latest",
				Steps:     []Step{{Uses: "docker/build-push-action@v6"}},
				Services:  nil,
				Container: nil,
			},
			wantUbuntu:    true,
			wantDockerCmd: false,
			wantDockerAct: true,
			wantServices:  false,
			wantContainer: false,
		},
		{
			name: "job with services",
			job: &Job{
				RunsOn:    "ubuntu-latest",
				Steps:     []Step{{Run: "echo hello"}},
				Services: map[string]any{
					"postgres": map[string]any{},
				},
				Container: nil,
			},
			wantUbuntu:    true,
			wantDockerCmd: false,
			wantDockerAct: false,
			wantServices:  true,
			wantContainer: false,
		},
		{
			name: "job with container",
			job: &Job{
				RunsOn:    "ubuntu-latest",
				Steps:     []Step{{Run: "node --version"}},
				Services:  nil,
				Container: "node:18",
			},
			wantUbuntu:    true,
			wantDockerCmd: false,
			wantDockerAct: false,
			wantServices:  false,
			wantContainer: true,
		},
		{
			name: "job with all disqualifiers",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Run: "docker build -t app ."},
					{Uses: "docker/build-push-action@v6"},
				},
				Services: map[string]any{
					"postgres": map[string]any{},
				},
				Container: "node:18",
			},
			wantUbuntu:    true,
			wantDockerCmd: true,
			wantDockerAct: true,
			wantServices:  true,
			wantContainer: true,
		},
		{
			name: "non-ubuntu runner",
			job: &Job{
				RunsOn:    "ubuntu-22.04",
				Steps:     []Step{{Run: "echo hello"}},
				Services:  nil,
				Container: nil,
			},
			wantUbuntu:    false,
			wantDockerCmd: false,
			wantDockerAct: false,
			wantServices:  false,
			wantContainer: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.job.IsUbuntuLatest(); got != tt.wantUbuntu {
				t.Errorf("IsUbuntuLatest() = %v, want %v", got, tt.wantUbuntu)
			}
			if got := tt.job.HasDockerCommands(); got != tt.wantDockerCmd {
				t.Errorf("HasDockerCommands() = %v, want %v", got, tt.wantDockerCmd)
			}
			if got := tt.job.HasContainerActions(); got != tt.wantDockerAct {
				t.Errorf("HasContainerActions() = %v, want %v", got, tt.wantDockerAct)
			}
			if got := tt.job.HasServices(); got != tt.wantServices {
				t.Errorf("HasServices() = %v, want %v", got, tt.wantServices)
			}
			if got := tt.job.HasContainer(); got != tt.wantContainer {
				t.Errorf("HasContainer() = %v, want %v", got, tt.wantContainer)
			}
		})
	}
}

func TestJob_GetMissingCommands(t *testing.T) {
	tests := []struct {
		name            string
		job             *Job
		expectedMissing []string
	}{
		{
			name: "job with missing command",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Run: "docker ps"},
				},
			},
			expectedMissing: []string{"docker"},
		},
		{
			name: "job with multiple missing commands",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Run: "docker ps"},
					{Run: "lsof -i :8080"},
				},
			},
			expectedMissing: []string{"docker", "lsof"},
		},
		{
			name: "non-ubuntu-latest runner",
			job: &Job{
				RunsOn: "ubuntu-22.04",
				Steps: []Step{
					{Run: "docker ps"},
				},
			},
			expectedMissing: nil,
		},
		{
			name: "job with comment",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Run: "# This is a comment\ndocker ps"},
				},
			},
			expectedMissing: []string{"docker"},
		},
		{
			name: "job with variable assignment",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Run: "VAR=value docker ps"},
				},
			},
			expectedMissing: []string{"docker"},
		},
		{
			name: "job with sudo",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Run: "sudo docker ps"},
				},
			},
			expectedMissing: []string{"docker"},
		},
		{
			name: "job with pipe",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Run: "docker ps | grep running"},
				},
			},
			expectedMissing: []string{"docker"},
		},
		{
			name: "job with command that exists in slim",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Run: "echo hello"},
				},
			},
			expectedMissing: nil,
		},
		{
			name: "job with multi-line script",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Run: `#!/bin/bash
set -e
docker ps
lsof -i :8080`},
				},
			},
			expectedMissing: []string{"docker", "lsof"},
		},
		{
			name: "job with empty steps",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{},
			},
			expectedMissing: nil,
		},
		{
			name: "job with step without run",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "actions/checkout@v4"},
				},
			},
			expectedMissing: nil,
		},
		{
			name: "job with setup-go should not report go as missing",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "actions/setup-go@v5"},
					{Run: "go fmt ./..."},
					{Run: "go test ./..."},
				},
			},
			expectedMissing: nil,
		},
		{
			name: "job with setup-go and other missing commands",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "actions/setup-go@v5"},
					{Run: "go fmt ./..."},
					{Run: "docker ps"},
				},
			},
			expectedMissing: []string{"docker"},
		},
		{
			name: "job with setup-node should not report node/npm/npx as missing",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "actions/setup-node@v4"},
					{Run: "npm install"},
					{Run: "npx eslint ."},
					{Run: "node script.js"},
				},
			},
			expectedMissing: nil,
		},
		{
			name: "job with setup-python should not report python/pip as missing",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "actions/setup-python@v5"},
					{Run: "python -m pytest"},
					{Run: "pip install -r requirements.txt"},
				},
			},
			expectedMissing: nil,
		},
		{
			name: "job with setup-ruby should not report ruby/gem as missing",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "actions/setup-ruby@v1"},
					{Run: "ruby script.rb"},
					{Run: "gem install bundler"},
				},
			},
			expectedMissing: nil,
		},
		{
			name: "job with setup-java should not report java/javac/mvn/gradle as missing",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "actions/setup-java@v4"},
					{Run: "java -version"},
					{Run: "javac Main.java"},
					{Run: "mvn test"},
					{Run: "gradle build"},
				},
			},
			expectedMissing: nil,
		},
		{
			name: "job with multiple setup actions",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "actions/setup-go@v5"},
					{Uses: "actions/setup-node@v4"},
					{Run: "go build"},
					{Run: "npm install"},
					{Run: "docker ps"},
				},
			},
			expectedMissing: []string{"docker"},
		},
		{
			name: "job without setup-go should report go as missing if it's missing",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Run: "go fmt ./..."},
				},
			},
			// Note: This test assumes "go" is in the missing commands list
			// If "go" is actually available in ubuntu-slim, this test may need adjustment
			expectedMissing: []string{"go"},
		},
		{
			name: "job with setup-dotnet should not report dotnet as missing",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "actions/setup-dotnet@v4"},
					{Run: "dotnet build"},
					{Run: "dotnet test"},
				},
			},
			expectedMissing: nil,
		},
		{
			name: "job with setup-bun should not report bun as missing",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "oven-sh/setup-bun@v1"},
					{Run: "bun install"},
					{Run: "bun test"},
				},
			},
			expectedMissing: nil,
		},
		{
			name: "job with setup-deno should not report deno as missing",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "denoland/setup-deno@v1"},
					{Run: "deno test"},
					{Run: "deno run script.ts"},
				},
			},
			expectedMissing: nil,
		},
		{
			name: "job with setup-terraform should not report terraform as missing",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "hashicorp/setup-terraform@v3"},
					{Run: "terraform init"},
					{Run: "terraform plan"},
				},
			},
			expectedMissing: nil,
		},
		{
			name: "job with setup-uv should not report uv as missing",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "astral-sh/setup-uv@v1"},
					{Run: "uv pip install"},
					{Run: "uv run script.py"},
				},
			},
			expectedMissing: nil,
		},
		{
			name: "job with setup-beam should not report elixir/mix as missing",
			job: &Job{
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{Uses: "erlef/setup-beam@v1"},
					{Run: "elixir -v"},
					{Run: "mix test"},
				},
			},
			expectedMissing: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.job.GetMissingCommands()

			// Check length
			if len(got) != len(tt.expectedMissing) {
				t.Errorf("GetMissingCommands() returned %d commands, want %d: got=%v, want=%v",
					len(got), len(tt.expectedMissing), got, tt.expectedMissing)
				return
			}

			// Check contents (order may vary)
			gotMap := make(map[string]bool)
			for _, cmd := range got {
				gotMap[cmd] = true
			}

			for _, expected := range tt.expectedMissing {
				if !gotMap[expected] {
					t.Errorf("GetMissingCommands() missing expected command: %s, got=%v", expected, got)
				}
			}
		})
	}
}

// TestJob_GetMissingCommands_RealWorkflows tests GetMissingCommands with actual workflow files
// from .github/workflows directory. This ensures the function works correctly with real-world examples.
func TestJob_GetMissingCommands_RealWorkflows(t *testing.T) {
	// Get the workspace root directory
	// This test assumes it's run from the repository root
	workspaceRoot := findWorkspaceRoot(t)
	workflowDir := filepath.Join(workspaceRoot, ".github", "workflows")

	// Check if .github/workflows directory exists
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		t.Skipf("Skipping test: .github/workflows directory not found at %s", workflowDir)
	}

	// Save original working directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Change to workspace root directory
	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}
	defer func() {
		os.Chdir(originalWd)
	}()

	// Load all workflow files
	workflows, err := LoadWorkflows()
	if err != nil {
		t.Fatalf("Failed to load workflows: %v", err)
	}

	if len(workflows) == 0 {
		t.Skip("Skipping test: no workflow files found")
	}

	// Test each workflow
	for _, wf := range workflows {
		t.Run(filepath.Base(wf.Path), func(t *testing.T) {
			for jobName, job := range wf.Jobs {
				t.Run(jobName, func(t *testing.T) {
					missingCommands := job.GetMissingCommands()

					// Log the results for debugging
					if len(missingCommands) > 0 {
						t.Logf("Job '%s' in %s uses missing commands: %v", jobName, wf.Path, missingCommands)
					}

					// Verify that if job is ubuntu-latest, GetMissingCommands returns results
					// (may be empty if no missing commands are used)
					if job.IsUbuntuLatest() {
						// This is fine - the function should work without errors
						// The actual commands depend on what's in the workflow
						_ = missingCommands
					} else {
						// For non-ubuntu-latest jobs, should return nil
						if missingCommands != nil {
							t.Errorf("GetMissingCommands() should return nil for non-ubuntu-latest job, got %v", missingCommands)
						}
					}
				})
			}
		})
	}
}

// findWorkspaceRoot finds the workspace root directory by looking for go.mod
func findWorkspaceRoot(t *testing.T) string {
	t.Helper()
	// Get the directory of this test file
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("Failed to get caller information")
	}
	currentDir := filepath.Dir(testFile)

	// Walk up the directory tree to find go.mod
	dir := currentDir
	for {
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root directory
			t.Fatalf("Failed to find workspace root (go.mod not found)")
		}
		dir = parent
	}
}
