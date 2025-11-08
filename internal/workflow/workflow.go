package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Workflow represents a GitHub Actions workflow file
type Workflow struct {
	Path string
	Jobs map[string]*Job
}

// Job represents a job in a GitHub Actions workflow
type Job struct {
	Name      string
	RunsOn    interface{} `yaml:"runs-on"`
	Steps     []Step      `yaml:"steps"`
	Services  interface{} `yaml:"services"`
	LineStart int         // Line number where the job starts
}

// Step represents a step in a job
type Step struct {
	Name string                 `yaml:"name"`
	Uses string                 `yaml:"uses"`
	Run  string                 `yaml:"run"`
	With map[string]interface{} `yaml:"with"`
}

// LoadWorkflows loads all workflow files from .github/workflows directory
func LoadWorkflows() ([]*Workflow, error) {
	workflowDir := ".github/workflows"

	// Check if directory exists
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("workflow directory not found: %s", workflowDir)
	}

	var workflows []*Workflow

	err := filepath.Walk(workflowDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only process .yml and .yaml files
		if !info.IsDir() && (strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml")) {
			wf, err := LoadWorkflow(path)
			if err != nil {
				// Log error but continue processing other files
				fmt.Fprintf(os.Stderr, "Warning: failed to load %s: %v\n", path, err)
				return nil
			}
			workflows = append(workflows, wf)
		}

		return nil
	})

	return workflows, err
}

// LoadWorkflow loads a single workflow file
func LoadWorkflow(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	var workflowData map[string]any
	if err := yaml.Unmarshal(data, &workflowData); err != nil {
		return nil, fmt.Errorf("failed to parse YAML %s: %w", path, err)
	}

	// Parse jobs
	jobs := make(map[string]*Job)
	if jobsData, ok := workflowData["jobs"].(map[string]any); ok {
		// Convert file content to lines for line number detection
		lines := strings.Split(string(data), "\n")

		for jobName, jobData := range jobsData {
			jobBytes, err := yaml.Marshal(jobData)
			if err != nil {
				continue
			}

			var job Job
			if err := yaml.Unmarshal(jobBytes, &job); err != nil {
				continue
			}

			job.Name = jobName
			// Find line number for this job's runs-on by searching in original file
			job.LineStart = findRunsOnLineNumber(lines, jobName)
			jobs[jobName] = &job
		}
	}

	return &Workflow{
		Path: path,
		Jobs: jobs,
	}, nil
}

// findRunsOnLineNumber finds the line number of runs-on for a specific job by searching in file lines
func findRunsOnLineNumber(lines []string, jobName string) int {
	inJobsSection := false
	inTargetJob := false
	indentLevel := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if we're in jobs section
		if trimmed == "jobs:" {
			inJobsSection = true
			continue
		}

		if !inJobsSection {
			continue
		}

		// Calculate indentation level
		lineIndent := 0
		for _, char := range line {
			switch char {
			case ' ':
				lineIndent++
			case '\t':
				lineIndent += 4 // Treat tab as 4 spaces
			default:
			}
		}

		// Check if we've left the jobs section (back to top level or another top-level key)
		if inJobsSection && lineIndent == 0 && trimmed != "" && !strings.HasSuffix(trimmed, ":") {
			break
		}

		// Check if this is the target job name
		if inJobsSection && strings.HasPrefix(trimmed, jobName+":") {
			inTargetJob = true
			indentLevel = lineIndent
			continue
		}

		// If we're in the target job, look for runs-on
		if inTargetJob {
			// Check if we've left this job (back to same or lower indent level)
			if lineIndent <= indentLevel && trimmed != "" && !strings.HasPrefix(trimmed, " ") {
				break
			}

			// Look for runs-on line
			if strings.Contains(trimmed, "runs-on:") {
				return i + 1 // Line numbers are 1-based
			}
		}
	}

	return 0
}

// UpdateRunsOn updates the runs-on value for a specific job in a workflow file
// It preserves the original file formatting by doing line-by-line replacement
func UpdateRunsOn(filePath string, jobName string, newRunsOn string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	lines := strings.Split(string(data), "\n")
	updated := false
	inJobsSection := false
	inTargetJob := false
	indentLevel := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if we're in jobs section
		if trimmed == "jobs:" {
			inJobsSection = true
			continue
		}

		if !inJobsSection {
			continue
		}

		// Calculate indentation level
		lineIndent := 0
		for _, char := range line {
			switch char {
			case ' ':
				lineIndent++
			case '\t':
				lineIndent += 4 // Treat tab as 4 spaces
			default:
				// Not a space or tab, stop counting
			}
		}

		// Check if we've left the jobs section
		if inJobsSection && lineIndent == 0 && trimmed != "" && !strings.HasSuffix(trimmed, ":") {
			break
		}

		// Check if this is the target job name
		if inJobsSection && strings.HasPrefix(trimmed, jobName+":") {
			inTargetJob = true
			indentLevel = lineIndent
			continue
		}

		// If we're in the target job, look for runs-on
		if inTargetJob {
			// Check if we've left this job
			if lineIndent <= indentLevel && trimmed != "" && !strings.HasPrefix(trimmed, " ") {
				break
			}

			// Look for runs-on line and replace ubuntu-latest with new value
			if strings.Contains(trimmed, "runs-on:") {
				// Handle both "runs-on: ubuntu-latest" and "runs-on:ubuntu-latest" formats
				if strings.Contains(trimmed, "ubuntu-latest") {
					// Extract original indentation from the line (preserve exact whitespace)
					originalIndent := ""
					for j := 0; j < len(line); j++ {
						char := line[j]
						if char == ' ' || char == '\t' {
							originalIndent += string(char)
						} else {
							break
						}
					}
					// Replace the value while preserving original indentation and format
					// Use the exact same format as the original line
					lines[i] = originalIndent + "runs-on: " + newRunsOn
					updated = true
					break
				}
			}
		}
	}

	if !updated {
		return fmt.Errorf("failed to find runs-on for job %s in %s", jobName, filePath)
	}

	// Write updated content back to file
	updatedContent := strings.Join(lines, "\n")
	if err := os.WriteFile(filePath, []byte(updatedContent), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	return nil
}
