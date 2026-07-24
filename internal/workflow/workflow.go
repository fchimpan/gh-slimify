package workflow

import (
	"fmt"
	"io/fs"
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
	ID        string      // Job ID (the key in the jobs map)
	Name      string      `yaml:"name"` // Custom display name from YAML
	RunsOn    interface{} `yaml:"runs-on"`
	Uses      string      `yaml:"uses"` // Reusable workflow reference (job-level uses:)
	Steps     []Step      `yaml:"steps"`
	Services  interface{} `yaml:"services"`
	Container interface{} `yaml:"container"`
	LineStart int         // Line number of the job's runs-on key (or the job key if absent)

	analyses     []scriptAnalysis // lazily-built per-step shell analysis
	analysesDone bool
}

// Step represents a step in a job
type Step struct {
	Name  string                 `yaml:"name"`
	Uses  string                 `yaml:"uses"`
	Run   string                 `yaml:"run"`
	Shell string                 `yaml:"shell"`
	With  map[string]interface{} `yaml:"with"`
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

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("failed to parse YAML %s: %w", path, err)
	}

	jobs := make(map[string]*Job)
	_, jobsNode := mappingEntry(documentRoot(&root), "jobs")
	if jobsNode = deref(jobsNode); jobsNode != nil && jobsNode.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(jobsNode.Content); i += 2 {
			keyNode := jobsNode.Content[i]
			jobNode := deref(jobsNode.Content[i+1])
			jobID := keyNode.Value

			var job Job
			if err := jobNode.Decode(&job); err != nil {
				continue
			}

			job.ID = jobID
			// If Name field is not specified in YAML, use the job ID as the display name
			if job.Name == "" {
				job.Name = jobID
			}
			// Point at the runs-on key so displayed locations lead straight to
			// the line a fix would touch; fall back to the job key itself.
			if runsOnKey, _ := mappingEntry(jobNode, "runs-on"); runsOnKey != nil {
				job.LineStart = runsOnKey.Line
			} else {
				job.LineStart = keyNode.Line
			}
			jobs[jobID] = &job
		}
	}

	return &Workflow{
		Path: path,
		Jobs: jobs,
	}, nil
}

// UpdateRunsOn updates the runs-on value for a specific job in a workflow file.
// jobID is the key in the jobs map (e.g., "test", "build").
// The runs-on scalar is located via the YAML AST and replaced in place in the
// original source, so comments, quoting style, indentation, and line endings
// are all preserved byte-for-byte.
func UpdateRunsOn(filePath string, jobID string, newRunsOn string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}
	perm := fs.FileMode(0644)
	if info, err := os.Stat(filePath); err == nil {
		perm = info.Mode().Perm()
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("failed to parse YAML %s: %w", filePath, err)
	}

	_, jobsNode := mappingEntry(documentRoot(&root), "jobs")
	if jobsNode = deref(jobsNode); jobsNode == nil {
		return fmt.Errorf("no jobs section found in %s", filePath)
	}
	_, jobNode := mappingEntry(jobsNode, jobID)
	if jobNode = deref(jobNode); jobNode == nil {
		return fmt.Errorf("job %s not found in %s", jobID, filePath)
	}
	_, runsOnNode := mappingEntry(jobNode, "runs-on")
	if runsOnNode = deref(runsOnNode); runsOnNode == nil {
		return fmt.Errorf("failed to find runs-on for job %s in %s", jobID, filePath)
	}

	scalar, err := runsOnScalarToReplace(runsOnNode)
	if err != nil {
		return fmt.Errorf("cannot update runs-on for job %s in %s: %w", jobID, filePath, err)
	}

	updated, err := replaceScalarInSource(data, scalar, newRunsOn)
	if err != nil {
		return fmt.Errorf("failed to update runs-on for job %s in %s: %w", jobID, filePath, err)
	}

	if err := os.WriteFile(filePath, updated, perm); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	return nil
}

// runsOnScalarToReplace picks the migratable runner scalar node to rewrite.
// Multi-label arrays target self-hosted runners, so they are refused rather
// than collapsed into a single GitHub-hosted label.
func runsOnScalarToReplace(runsOn *yaml.Node) (*yaml.Node, error) {
	switch runsOn.Kind {
	case yaml.ScalarNode:
		if !migratableRunners[runsOn.Value] {
			return nil, fmt.Errorf("runs-on is %q, not ubuntu-latest or ubuntu-24.04", runsOn.Value)
		}
		return runsOn, nil
	case yaml.SequenceNode:
		if len(runsOn.Content) != 1 {
			return nil, fmt.Errorf("runs-on uses multiple runner labels")
		}
		item := deref(runsOn.Content[0])
		if item.Kind != yaml.ScalarNode || !migratableRunners[item.Value] {
			return nil, fmt.Errorf("runs-on label is not ubuntu-latest or ubuntu-24.04")
		}
		return item, nil
	default:
		return nil, fmt.Errorf("unsupported runs-on format")
	}
}

// replaceScalarInSource replaces the exact source text of a scalar node with
// newValue, leaving every other byte of the file untouched. The token found at
// the node's position is verified against the expected rendering before the
// edit, so a mismatch fails loudly instead of corrupting the file.
func replaceScalarInSource(data []byte, scalar *yaml.Node, newValue string) ([]byte, error) {
	var oldToken, newToken string
	switch scalar.Style {
	case 0, yaml.FlowStyle:
		oldToken, newToken = scalar.Value, newValue
	case yaml.SingleQuotedStyle:
		oldToken, newToken = "'"+scalar.Value+"'", "'"+newValue+"'"
	case yaml.DoubleQuotedStyle:
		oldToken, newToken = `"`+scalar.Value+`"`, `"`+newValue+`"`
	default:
		return nil, fmt.Errorf("unsupported scalar style for %q", scalar.Value)
	}

	lines := strings.SplitAfter(string(data), "\n")
	if scalar.Line < 1 || scalar.Line > len(lines) {
		return nil, fmt.Errorf("node position %d:%d is outside the file", scalar.Line, scalar.Column)
	}

	// yaml.v3 columns count characters, not bytes.
	runes := []rune(lines[scalar.Line-1])
	col := scalar.Column - 1
	if col < 0 || col+len([]rune(oldToken)) > len(runes) ||
		string(runes[col:col+len([]rune(oldToken))]) != oldToken {
		return nil, fmt.Errorf("expected %q at %d:%d", oldToken, scalar.Line, scalar.Column)
	}

	lines[scalar.Line-1] = string(runes[:col]) + newToken + string(runes[col+len([]rune(oldToken)):])
	return []byte(strings.Join(lines, "")), nil
}

// documentRoot unwraps the document node produced by unmarshalling into a
// yaml.Node, returning the top-level mapping.
func documentRoot(root *yaml.Node) *yaml.Node {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		return root.Content[0]
	}
	return root
}

// mappingEntry returns the key and value nodes for key within a mapping node.
func mappingEntry(node *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	node = deref(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i], node.Content[i+1]
		}
	}
	return nil, nil
}

// deref resolves alias nodes to their anchor targets.
func deref(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		return n.Alias
	}
	return n
}
