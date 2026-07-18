package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempWorkflow writes content to a temp workflow file and returns its path.
func writeTempWorkflow(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wf.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Regression tests for the string-matching bugs the AST-based UpdateRunsOn
// fixes: commented lines being "un-commented" into duplicate keys, trailing
// comments being deleted, arrays being collapsed, and matching strings inside
// run: scripts.
func TestUpdateRunsOn_Regressions(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		jobID   string
		wantErr bool
		want    string // expected full file content after update (when wantErr is false)
	}{
		{
			name: "commented runs-on line is left untouched",
			yaml: `jobs:
  build:
    # runs-on: ubuntu-latest-16-core (old config)
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`,
			jobID: "build",
			want: `jobs:
  build:
    # runs-on: ubuntu-latest-16-core (old config)
    runs-on: ubuntu-slim
    steps:
      - run: echo hi
`,
		},
		{
			name: "trailing comment is preserved",
			yaml: `jobs:
  build:
    runs-on: ubuntu-latest # do not change without infra approval
    steps:
      - run: echo hi
`,
			jobID: "build",
			want: `jobs:
  build:
    runs-on: ubuntu-slim # do not change without infra approval
    steps:
      - run: echo hi
`,
		},
		{
			name: "runs-on string inside a run script is not rewritten",
			yaml: `jobs:
  build:
    steps:
      - run: |
          grep -r "runs-on: ubuntu-latest" .github/workflows
    runs-on: ubuntu-latest
`,
			jobID: "build",
			want: `jobs:
  build:
    steps:
      - run: |
          grep -r "runs-on: ubuntu-latest" .github/workflows
    runs-on: ubuntu-slim
`,
		},
		{
			name: "single-element flow sequence keeps its brackets",
			yaml: `jobs:
  build:
    runs-on: [ubuntu-latest]
    steps:
      - run: echo hi
`,
			jobID: "build",
			want: `jobs:
  build:
    runs-on: [ubuntu-slim]
    steps:
      - run: echo hi
`,
		},
		{
			name: "single-element block sequence keeps its shape",
			yaml: `jobs:
  build:
    runs-on:
      - ubuntu-latest
    steps:
      - run: echo hi
`,
			jobID: "build",
			want: `jobs:
  build:
    runs-on:
      - ubuntu-slim
    steps:
      - run: echo hi
`,
		},
		{
			name: "double-quoted scalar keeps its quotes",
			yaml: `jobs:
  build:
    runs-on: "ubuntu-latest"
    steps:
      - run: echo hi
`,
			jobID: "build",
			want: `jobs:
  build:
    runs-on: "ubuntu-slim"
    steps:
      - run: echo hi
`,
		},
		{
			name: "multi-label runs-on is refused instead of collapsed",
			yaml: `jobs:
  build:
    runs-on: [ubuntu-latest, my-custom-label]
    steps:
      - run: echo hi
`,
			jobID:   "build",
			wantErr: true,
		},
		{
			name: "non-migratable runner is refused",
			yaml: `jobs:
  build:
    runs-on: ubuntu-22.04
    steps:
      - run: echo hi
`,
			jobID:   "build",
			wantErr: true,
		},
		{
			name: "ubuntu-24.04 is migratable",
			yaml: `jobs:
  build:
    runs-on: ubuntu-24.04
    steps:
      - run: echo hi
`,
			jobID: "build",
			want: `jobs:
  build:
    runs-on: ubuntu-slim
    steps:
      - run: echo hi
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempWorkflow(t, tt.yaml)

			err := UpdateRunsOn(path, tt.jobID, "ubuntu-slim")
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none; file after:\n%s", after)
				}
				if string(after) != tt.yaml {
					t.Errorf("file must be untouched on error.\nbefore:\n%s\nafter:\n%s", tt.yaml, after)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(after) != tt.want {
				t.Errorf("unexpected result.\ngot:\n%s\nwant:\n%s", after, tt.want)
			}
		})
	}
}

func TestUpdateRunsOn_PreservesCRLF(t *testing.T) {
	yaml := "jobs:\r\n  build:\r\n    runs-on: ubuntu-latest\r\n    steps:\r\n      - run: echo hi\r\n"
	path := writeTempWorkflow(t, yaml)

	if err := UpdateRunsOn(path, "build", "ubuntu-slim"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "jobs:\r\n  build:\r\n    runs-on: ubuntu-slim\r\n    steps:\r\n      - run: echo hi\r\n"
	if string(after) != want {
		t.Errorf("CRLF line endings must be preserved.\ngot:  %q\nwant: %q", after, want)
	}
}

// The line number must point at the real runs-on key even when a run script
// or comment mentions "runs-on:" earlier in the job.
func TestLoadWorkflow_LineNumberIgnoresLookalikes(t *testing.T) {
	yaml := `jobs:
  build:
    # runs-on: ubuntu-latest (commented out)
    steps:
      - run: |
          echo "runs-on: ubuntu-latest"
    runs-on: ubuntu-latest
`
	path := writeTempWorkflow(t, yaml)

	wf, err := LoadWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}
	job, ok := wf.Jobs["build"]
	if !ok {
		t.Fatal("job build not found")
	}

	lines := strings.Split(yaml, "\n")
	if job.LineStart < 1 || job.LineStart > len(lines) ||
		!strings.HasPrefix(strings.TrimSpace(lines[job.LineStart-1]), "runs-on:") ||
		strings.HasPrefix(strings.TrimSpace(lines[job.LineStart-1]), "#") {
		t.Errorf("LineStart = %d, which is not the real runs-on line", job.LineStart)
	}
	if job.LineStart != 7 {
		t.Errorf("LineStart = %d, want 7", job.LineStart)
	}
}
