package api

import "testing"

func TestParseActionRef(t *testing.T) {
	tests := []struct {
		uses    string
		owner   string
		repo    string
		subpath string
		ref     string
		wantErr bool
	}{
		{uses: "aquasecurity/trivy-action@0.24.0", owner: "aquasecurity", repo: "trivy-action", ref: "0.24.0"},
		{uses: "owner/repo/sub/dir@v1", owner: "owner", repo: "repo", subpath: "sub/dir", ref: "v1"},
		{uses: "owner/repo", owner: "owner", repo: "repo"},
		{uses: "owner/repo@8f4b7f84864484a7bf31766abe9204da3cbe65b3", owner: "owner", repo: "repo", ref: "8f4b7f84864484a7bf31766abe9204da3cbe65b3"},
		{uses: "not-a-ref", wantErr: true},
		{uses: "/repo@v1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.uses, func(t *testing.T) {
			owner, repo, subpath, ref, err := ParseActionRef(tt.uses)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseActionRef(%q) expected error", tt.uses)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseActionRef(%q) unexpected error: %v", tt.uses, err)
			}
			if owner != tt.owner || repo != tt.repo || subpath != tt.subpath || ref != tt.ref {
				t.Errorf("ParseActionRef(%q) = (%q, %q, %q, %q), want (%q, %q, %q, %q)",
					tt.uses, owner, repo, subpath, ref, tt.owner, tt.repo, tt.subpath, tt.ref)
			}
		})
	}
}

func TestParseActionMetadata(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantUsing string
		wantSteps int
	}{
		{
			name: "docker action",
			yaml: `name: My Docker Action
runs:
  using: docker
  image: Dockerfile
`,
			wantUsing: "docker",
		},
		{
			name: "node action",
			yaml: `name: My JS Action
runs:
  using: node20
  main: dist/index.js
`,
			wantUsing: "node20",
		},
		{
			name: "composite action",
			yaml: `name: My Composite
runs:
  using: composite
  steps:
    - uses: actions/checkout@v4
    - run: echo hi
      shell: bash
`,
			wantUsing: "composite",
			wantSteps: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := ParseActionMetadata([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if meta.Using != tt.wantUsing {
				t.Errorf("Using = %q, want %q", meta.Using, tt.wantUsing)
			}
			if len(meta.Steps) != tt.wantSteps {
				t.Errorf("Steps = %d, want %d", len(meta.Steps), tt.wantSteps)
			}
		})
	}
}
