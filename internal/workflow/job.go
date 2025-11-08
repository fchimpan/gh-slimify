package workflow

import (
	"regexp"
	"strings"
)

var (
	// containerCommandPatterns lists regex patterns that match container commands
	// Each pattern is compiled and checked against run commands.
	// Future additions could include: podman commands, containerd commands, etc.
	containerCommandPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\bdocker[\s-](?:build|run|exec|ps|pull|push|tag|login)\b`),
		regexp.MustCompile(`\bdocker-compose\b`),
		regexp.MustCompile(`\bdocker\s+compose\b`),
	}

	// containerActionPrefixes lists prefixes that indicate container-based GitHub Actions
	// This covers:
	// - docker:// image syntax (e.g., "docker://alpine:latest")
	// - docker/ organization actions (e.g., "docker/build-push-action@v6")
	// Future additions could include: "container://", "podman/", etc.
	containerActionPrefixes = []string{"docker"}
)

// IsUbuntuLatest checks if a job runs on ubuntu-latest
func (j *Job) IsUbuntuLatest() bool {
	if j.RunsOn == nil {
		return false
	}

	switch v := j.RunsOn.(type) {
	case string:
		return v == "ubuntu-latest"
	case []any:
		// runs-on can be a matrix or array
		for _, item := range v {
			if str, ok := item.(string); ok && str == "ubuntu-latest" {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// HasDockerCommands checks if a job uses Docker commands
// It checks if the job uses any Docker commands in the run commands.
// Matches patterns like "docker build", "docker-compose", "sudo docker run", etc.
func (j *Job) HasDockerCommands() bool {
	for _, step := range j.Steps {
		if step.Run == "" {
			continue
		}

		runLower := strings.ToLower(step.Run)
		// Check if run command matches any container command pattern
		for _, pattern := range containerCommandPatterns {
			if pattern.MatchString(runLower) {
				return true
			}
		}
	}
	return false
}

// HasContainerActions checks if a job uses container-based GitHub Actions
// It detects actions that use container prefixes defined in containerActionPrefixes:
// - docker:// image syntax (e.g., "docker://alpine:latest")
// - docker/ organization actions (e.g., "docker/build-push-action@v6")
// Future container tools can be added by extending containerActionPrefixes.
func (j *Job) HasContainerActions() bool {
	for _, step := range j.Steps {
		if step.Uses == "" {
			continue
		}
		uses := step.Uses
		// Check if uses starts with any container action prefix
		for _, prefix := range containerActionPrefixes {
			if strings.HasPrefix(uses, prefix) {
				return true
			}
		}
	}
	return false
}

// HasServices checks if a job uses services
// Services are containers that are shared between jobs.
// Since ubuntu-slim runs itself inside a container and does not provide dockerd,
// nested container jobs are not supported.
func (j *Job) HasServices() bool {
	return j.Services != nil
}

// HasContainer checks if a job uses the container: syntax
// Jobs with container: run steps inside a Docker container, which requires
// access to the Docker daemon. Since ubuntu-slim runs itself inside a container
// and does not provide dockerd, nested container jobs are not supported.
func (j *Job) HasContainer() bool {
	return j.Container != nil
}

// GetMissingCommands extracts commands from job steps and returns a list of commands
// that exist in ubuntu-latest but are missing in ubuntu-slim.
// It parses shell commands from step.Run fields and checks them against the
// missing commands list.
func (j *Job) GetMissingCommands() []string {
	if !j.IsUbuntuLatest() {
		// Only check commands for ubuntu-latest jobs
		return nil
	}

	var missingCommands []string
	seen := make(map[string]bool)

	for _, step := range j.Steps {
		if step.Run == "" {
			continue
		}

		commands := extractCommands(step.Run)
		for _, cmd := range commands {
			// Normalize command name (remove path, keep only basename)
			cmdName := normalizeCommand(cmd)
			if cmdName == "" {
				continue
			}

			// Check if command is missing in slim and not already added
			if IsMissingInSlim(cmdName) && !seen[cmdName] {
				missingCommands = append(missingCommands, cmdName)
				seen[cmdName] = true
			}
		}
	}

	return missingCommands
}

// extractCommands extracts command names from a shell script string.
// It handles multi-line scripts, comments, variable assignments, and common shell constructs.
func extractCommands(script string) []string {
	var commands []string
	lines := strings.Split(script, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip comment lines
		if strings.HasPrefix(line, "#") {
			continue
		}

		// Handle shebang
		if strings.HasPrefix(line, "#!") {
			continue
		}

		// Extract commands before pipe, redirect, or logical operators
		parts := splitCommandLine(line)
		for _, part := range parts {
			cmd := extractCommandFromPart(part)
			if cmd != "" {
				commands = append(commands, cmd)
			}
		}
	}

	return commands
}

// splitCommandLine splits a command line by pipe, redirect, and logical operators
// while preserving the command parts.
func splitCommandLine(line string) []string {
	// Split by |, &&, ||, ;, >, <, >>, <<
	// Simple approach: split by these operators
	parts := []string{line}
	separators := []string{"|", "&&", "||", ";", ">>", "<<", ">", "<"}

	for _, sep := range separators {
		var newParts []string
		for _, part := range parts {
			split := strings.Split(part, sep)
			for i, s := range split {
				s = strings.TrimSpace(s)
				if s != "" {
					if i == 0 {
						newParts = append(newParts, s)
					} else {
						// For subsequent parts after separator, add them separately
						newParts = append(newParts, s)
					}
				}
			}
		}
		parts = newParts
	}

	return parts
}

// extractCommandFromPart extracts the command name from a command part.
// It handles prefixes like sudo, env, time, etc.
func extractCommandFromPart(part string) string {
	part = strings.TrimSpace(part)
	if part == "" {
		return ""
	}

	// Handle variable assignments (VAR=value command)
	// Split by space first to handle cases like "VAR=value command"
	fields := strings.Fields(part)
	if len(fields) == 0 {
		return ""
	}

	// Find the first field that doesn't contain = (the actual command)
	startIndex := 0
	for startIndex < len(fields) {
		if !strings.Contains(fields[startIndex], "=") {
			break
		}
		startIndex++
	}

	if startIndex >= len(fields) {
		// All fields contain =, no command found
		return ""
	}

	part = strings.Join(fields[startIndex:], " ")

	// Re-extract fields after handling variable assignments
	fields = strings.Fields(part)
	if len(fields) == 0 {
		return ""
	}

	// Common prefixes to skip
	prefixes := []string{"sudo", "env", "time", "nohup", "setsid", "stdbuf"}
	cmdStartIndex := 0

	for cmdStartIndex < len(fields) {
		field := fields[cmdStartIndex]
		found := false
		for _, prefix := range prefixes {
			if field == prefix {
				cmdStartIndex++
				found = true
				break
			}
		}
		if !found {
			break
		}
	}

	if cmdStartIndex >= len(fields) {
		return ""
	}

	return fields[cmdStartIndex]
}

// normalizeCommand normalizes a command name by removing path components.
// It returns only the basename of the command.
func normalizeCommand(cmd string) string {
	if cmd == "" {
		return ""
	}

	// Remove path components
	if strings.Contains(cmd, "/") {
		parts := strings.Split(cmd, "/")
		cmd = parts[len(parts)-1]
	}

	// Remove common suffixes that might be part of the command
	cmd = strings.TrimSpace(cmd)
	return cmd
}
