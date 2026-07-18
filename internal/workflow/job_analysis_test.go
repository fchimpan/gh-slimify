package workflow

import "testing"

// Regression tests for docker detection gaps in the old regex approach:
// subcommands outside the hard-coded list (buildx, load, save, stop, ...)
// escaped detection entirely.
func TestJob_HasDockerCommands_SubcommandCoverage(t *testing.T) {
	tests := []struct {
		name     string
		run      string
		expected bool
	}{
		{"docker buildx build", "docker buildx build -t foo .", true},
		{"docker load", "docker load -i image.tar", true},
		{"docker save", "docker save foo -o foo.tar", true},
		{"docker stop", "docker stop mycontainer", true},
		{"docker image prune", "docker image prune -f", true},
		{"docker system df", "docker system df", true},
		{"podman", "podman build -t foo .", true},
		{"nerdctl", "nerdctl run alpine", true},
		{"buildah", "buildah bud -t foo .", true},
		{"docker in pipeline", "make artifacts && docker buildx bake release", true},
		{"docker in command substitution", "IMAGE_ID=$(docker images -q foo)", true},
		{"docker behind sudo with flags", "sudo -E docker load -i image.tar", true},
		{"docker word as argument only", "echo docker is fun", false},
		{"docker in heredoc data", "cat <<EOF\ndocker build -t foo .\nEOF", false},
		{"docker in bash -c payload", `bash -c "docker buildx build ."`, true},
		{"docker in heredoc piped to shell", "bash <<'EOF'\ndocker run alpine\nEOF", true},
		{"docker in eval payload", `eval "docker compose up -d"`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Steps: []Step{{Run: tt.run}}}
			if got := job.HasDockerCommands(); got != tt.expected {
				t.Errorf("HasDockerCommands(%q) = %v, want %v", tt.run, got, tt.expected)
			}
		})
	}
}

// The old regex flagged privileged command names anywhere in the script text,
// including string arguments; command-position matching must not.
func TestJob_HasPrivilegedOperations_Precision(t *testing.T) {
	tests := []struct {
		name     string
		run      string
		expected bool
	}{
		{"mount as grep argument", "grep mount /proc/filesystems", false},
		{"mount in echoed string", `echo "please do not mount anything"`, false},
		{"mount in heredoc data", "cat <<EOF\nmount /dev/sda1 /mnt\nEOF", false},
		{"mount as command", "mount /dev/sda1 /mnt", true},
		{"mount via bash -c", `bash -c "mount /dev/sda1 /mnt"`, true},
		{"sysctl in for-loop body", "for i in 1 2; do sysctl -w net.ipv4.ip_forward=1; done", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Steps: []Step{{Run: tt.run}}}
			if got, _ := job.HasPrivilegedOperations(); got != tt.expected {
				t.Errorf("HasPrivilegedOperations(%q) = %v, want %v", tt.run, got, tt.expected)
			}
		})
	}
}

// Missing-command extraction must not treat heredoc bodies or string
// arguments as commands, and must find commands inside substitutions.
func TestJob_GetMissingCommands_ASTPrecision(t *testing.T) {
	tests := []struct {
		name            string
		run             string
		expectedMissing []string
	}{
		{
			// "mysql" exists on ubuntu-latest but not on ubuntu-slim; inside
			// heredoc data it must not be reported.
			name:            "command name inside heredoc data",
			run:             "cat <<EOF\nmysql -u root\nEOF",
			expectedMissing: nil,
		},
		{
			name:            "command inside command substitution is found",
			run:             "OPEN_PORTS=$(lsof -i -P -n)",
			expectedMissing: []string{"lsof"},
		},
		{
			name:            "github expression placeholder is not a missing command",
			run:             "${{ inputs.custom-command }} --version",
			expectedMissing: nil,
		},
		{
			// Unparseable shell falls back to line-based extraction, which
			// still finds the command.
			name:            "unparseable script falls back to line-based extraction",
			run:             "docker ps\nif [ incomplete",
			expectedMissing: []string{"docker"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{RunsOn: "ubuntu-latest", Steps: []Step{{Run: tt.run}}}
			got := job.GetMissingCommands()
			if len(got) != len(tt.expectedMissing) {
				t.Fatalf("GetMissingCommands(%q) = %v, want %v", tt.run, got, tt.expectedMissing)
			}
			for i, want := range tt.expectedMissing {
				if got[i] != want {
					t.Errorf("GetMissingCommands(%q)[%d] = %q, want %q", tt.run, i, got[i], want)
				}
			}
		})
	}
}

// Steps whose shell: is not a POSIX shell must fall back to pattern matching
// so container usage is still caught conservatively.
func TestJob_NonShellSteps_FallBackToPatterns(t *testing.T) {
	job := &Job{Steps: []Step{{
		Shell: "python",
		Run:   `import subprocess; subprocess.run(["docker", "build", "."])`,
	}}}
	if !job.HasDockerCommands() {
		t.Error("HasDockerCommands() = false for python step mentioning docker, want true (conservative fallback)")
	}
}
