package workflow

import (
	"regexp"
	"strings"
)

// setupActionCommands maps setup actions to the commands they provide.
// When a setup action is present in a job, these commands should not be
// reported as missing even if they're not in ubuntu-slim by default.
//
// This includes:
// - GitHub official setup actions (actions/setup-*)
// - Verified creator setup actions from GitHub Marketplace
//
// References:
// - https://github.com/marketplace?query=setup&verification=verified_creator&type=actions
var setupActionCommands = map[string][]string{
	"actions/setup-go":              {"go"},
	"actions/setup-node":            {"node", "npm", "npx"},
	"actions/setup-python":          {"python", "python3", "pip", "pip3"},
	"actions/setup-java":            {"java", "javac", "mvn", "gradle"},
	"actions/setup-dotnet":          {"dotnet"},
	"actions/setup-ruby":            {"ruby", "gem"},
	"hashicorp/setup-terraform":     {"terraform"},
	"hashicorp/setup-packer":        {"packer"},
	"oven-sh/setup-bun":             {"bun"},
	"astral-sh/setup-uv":            {"uv"},
	"erlef/setup-beam":              {"erl", "elixir", "mix", "rebar3", "hex"},
	"microsoft/setup-msbuild":       {"msbuild"},
	"denoland/setup-deno":           {"deno"},
	"jfrog/setup-jfrog-cli":         {"jfrog"},
	"supabase/setup-cli":            {"supabase"},
	"aws-actions/setup-sam":         {"sam"},
	"gruntwork-io/setup-terragrunt": {"terragrunt"},
	"pdm-project/setup-pdm":         {"pdm"},
}

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

// IsUbuntuSlim checks if a job already runs on ubuntu-slim
func (j *Job) IsUbuntuSlim() bool {
	if j.RunsOn == nil {
		return false
	}

	switch v := j.RunsOn.(type) {
	case string:
		return v == "ubuntu-slim"
	case []any:
		// runs-on can be a matrix or array
		for _, item := range v {
			if str, ok := item.(string); ok && str == "ubuntu-slim" {
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

// HasHomebrewSetupAction checks if a job uses the Homebrew setup action.
// The Homebrew setup action is slow and flaky on ubuntu-slim runners,
// so jobs that use it should not be migrated.
func (j *Job) HasHomebrewSetupAction() bool {
	for _, step := range j.Steps {
		if step.Uses == "" {
			continue
		}
		if strings.HasPrefix(step.Uses, "Homebrew/actions/setup-homebrew") {
			return true
		}
	}
	return false
}

// GetMissingCommands extracts commands from job steps and returns a list of commands
// that exist in ubuntu-latest but are missing in ubuntu-slim.
// It parses shell commands from step.Run fields and checks them against the
// missing commands list.
// Commands provided by setup actions (e.g., setup-go provides "go") are excluded
// from the missing commands list since they will be available after the setup action runs.
func (j *Job) GetMissingCommands() []string {
	if !j.IsUbuntuLatest() {
		// Only check commands for ubuntu-latest jobs
		return nil
	}

	// Collect commands provided by setup actions in this job
	setupProvidedCommands := j.getSetupProvidedCommands()

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

			// Skip if command is provided by a setup action
			if setupProvidedCommands[cmdName] {
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

// getSetupProvidedCommands returns a map of commands that are provided by setup actions
// in this job. The map keys are command names, and values are always true.
func (j *Job) getSetupProvidedCommands() map[string]bool {
	providedCommands := make(map[string]bool)

	for _, step := range j.Steps {
		if step.Uses == "" {
			continue
		}

		// Check if this step uses a setup action
		// Setup actions typically follow the pattern: actions/setup-<lang>@<version>
		// We match the base action name without version
		for actionPrefix, commands := range setupActionCommands {
			if strings.HasPrefix(step.Uses, actionPrefix) {
				// This setup action provides these commands
				for _, cmd := range commands {
					providedCommands[cmd] = true
				}
			}
		}
	}

	return providedCommands
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
