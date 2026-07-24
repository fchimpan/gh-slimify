package scan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fchimpan/gh-slimify/internal/api"
)

// fakeResolver returns canned metadata and records how many times each ref
// was resolved.
type fakeResolver struct {
	metadata map[string]*api.ActionMetadata
	calls    map[string]int
}

func (f *fakeResolver) resolve(ref string) (*api.ActionMetadata, error) {
	f.calls[ref]++
	if meta, ok := f.metadata[ref]; ok {
		return meta, nil
	}
	return nil, errors.New("not found")
}

func newFakeResolver(metadata map[string]*api.ActionMetadata) *fakeResolver {
	return &fakeResolver{metadata: metadata, calls: map[string]int{}}
}

func TestVerifyContainerActions(t *testing.T) {
	docker := &api.ActionMetadata{Using: "docker"}
	node := &api.ActionMetadata{Using: "node20"}

	t.Run("docker action moves candidate to ineligible", func(t *testing.T) {
		r := newFakeResolver(map[string]*api.ActionMetadata{
			"acme/scan-action@v1": docker,
		})
		candidates := []*Candidate{
			{JobID: "scan", usesRefs: []string{"actions/checkout@v4", "acme/scan-action@v1"}},
			{JobID: "lint", usesRefs: []string{"actions/checkout@v4"}},
		}

		remaining, ineligible := verifyContainerActions(candidates, nil, r.resolve, false)

		if len(remaining) != 1 || remaining[0].JobID != "lint" {
			t.Fatalf("remaining = %+v, want only lint", remaining)
		}
		if len(ineligible) != 1 || ineligible[0].JobID != "scan" {
			t.Fatalf("ineligible = %+v, want only scan", ineligible)
		}
		if !strings.Contains(ineligible[0].Reasons[0], "acme/scan-action@v1") {
			t.Errorf("reason = %q, want mention of the docker action ref", ineligible[0].Reasons[0])
		}
		if r.calls["actions/checkout@v4"] != 0 {
			t.Error("actions/ refs must not be resolved via the API")
		}
	})

	t.Run("node action stays eligible", func(t *testing.T) {
		r := newFakeResolver(map[string]*api.ActionMetadata{
			"acme/setup-tool@v2": node,
		})
		candidates := []*Candidate{{JobID: "build", usesRefs: []string{"acme/setup-tool@v2"}}}

		remaining, ineligible := verifyContainerActions(candidates, nil, r.resolve, false)

		if len(remaining) != 1 || len(ineligible) != 0 {
			t.Fatalf("remaining = %d, ineligible = %d, want 1/0", len(remaining), len(ineligible))
		}
	})

	t.Run("unresolvable ref falls back to eligible", func(t *testing.T) {
		r := newFakeResolver(nil)
		candidates := []*Candidate{{JobID: "build", usesRefs: []string{"acme/unknown-action@v1"}}}

		remaining, ineligible := verifyContainerActions(candidates, nil, r.resolve, false)

		if len(remaining) != 1 || len(ineligible) != 0 {
			t.Fatalf("remaining = %d, ineligible = %d, want 1/0 (heuristic fallback)", len(remaining), len(ineligible))
		}
	})

	t.Run("composite action wrapping a docker action is detected", func(t *testing.T) {
		r := newFakeResolver(map[string]*api.ActionMetadata{
			"acme/wrapper@v1": {Using: "composite", Steps: []api.ActionStep{
				{Uses: "actions/cache@v4"},
				{Uses: "acme/inner@v3"},
			}},
			"acme/inner@v3": docker,
		})
		candidates := []*Candidate{{JobID: "build", usesRefs: []string{"acme/wrapper@v1"}}}

		remaining, ineligible := verifyContainerActions(candidates, nil, r.resolve, false)

		if len(remaining) != 0 || len(ineligible) != 1 {
			t.Fatalf("remaining = %d, ineligible = %d, want 0/1", len(remaining), len(ineligible))
		}
	})

	t.Run("composite action running docker commands is detected", func(t *testing.T) {
		r := newFakeResolver(map[string]*api.ActionMetadata{
			"acme/builder@v1": {Using: "composite", Steps: []api.ActionStep{
				{Run: "docker buildx build -t app ."},
			}},
		})
		candidates := []*Candidate{{JobID: "build", usesRefs: []string{"acme/builder@v1"}}}

		remaining, ineligible := verifyContainerActions(candidates, nil, r.resolve, false)

		if len(remaining) != 0 || len(ineligible) != 1 {
			t.Fatalf("remaining = %d, ineligible = %d, want 0/1", len(remaining), len(ineligible))
		}
	})

	t.Run("composite step with docker:// image is detected without resolving", func(t *testing.T) {
		r := newFakeResolver(map[string]*api.ActionMetadata{
			"acme/wrapper@v1": {Using: "composite", Steps: []api.ActionStep{
				{Uses: "docker://alpine:3.20"},
			}},
		})
		candidates := []*Candidate{{JobID: "build", usesRefs: []string{"acme/wrapper@v1"}}}

		_, ineligible := verifyContainerActions(candidates, nil, r.resolve, false)

		if len(ineligible) != 1 {
			t.Fatalf("ineligible = %d, want 1", len(ineligible))
		}
	})

	t.Run("each unique ref is resolved once across candidates", func(t *testing.T) {
		r := newFakeResolver(map[string]*api.ActionMetadata{
			"acme/setup-tool@v2": node,
		})
		candidates := []*Candidate{
			{JobID: "a", usesRefs: []string{"acme/setup-tool@v2"}},
			{JobID: "b", usesRefs: []string{"acme/setup-tool@v2", "acme/setup-tool@v2"}},
		}

		verifyContainerActions(candidates, nil, r.resolve, false)

		if r.calls["acme/setup-tool@v2"] != 1 {
			t.Errorf("resolver called %d times for the same ref, want 1", r.calls["acme/setup-tool@v2"])
		}
	})

	t.Run("cyclic composite references terminate", func(t *testing.T) {
		r := newFakeResolver(map[string]*api.ActionMetadata{
			"acme/a@v1": {Using: "composite", Steps: []api.ActionStep{{Uses: "acme/b@v1"}}},
			"acme/b@v1": {Using: "composite", Steps: []api.ActionStep{{Uses: "acme/a@v1"}}},
		})
		candidates := []*Candidate{{JobID: "build", usesRefs: []string{"acme/a@v1"}}}

		remaining, _ := verifyContainerActions(candidates, nil, r.resolve, false)

		if len(remaining) != 1 {
			t.Fatalf("remaining = %d, want 1 (cycle must not hang or flag)", len(remaining))
		}
	})
}

func TestLoadLocalActionMetadata(t *testing.T) {
	dir := t.TempDir()
	content := "name: Local\nruns:\n  using: docker\n  image: Dockerfile\n"
	if err := os.WriteFile(filepath.Join(dir, "action.yml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	meta, err := loadLocalActionMetadata(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Using != "docker" {
		t.Errorf("Using = %q, want docker", meta.Using)
	}

	if _, err := loadLocalActionMetadata(filepath.Join(dir, "missing")); err == nil {
		t.Error("expected error for missing local action")
	}
}

func TestActionResolver_Offline(t *testing.T) {
	// A nil client means offline: remote lookups are skipped.
	resolve := newActionResolver(nil)

	if _, err := resolve("acme/tool@v1"); !errors.Is(err, errOfflineResolution) {
		t.Errorf("remote resolution offline: err = %v, want errOfflineResolution", err)
	}

	// Local actions still resolve from disk in offline mode.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "action.yaml"), []byte("runs:\n  using: composite\n"), 0644); err != nil {
		t.Fatal(err)
	}
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	meta, err := resolve("./.")
	if err != nil {
		t.Fatalf("local resolution offline: unexpected error: %v", err)
	}
	if meta.Using != "composite" {
		t.Errorf("Using = %q, want composite", meta.Using)
	}
}

func TestResolveChildRef(t *testing.T) {
	tests := []struct {
		parent string
		child  string
		want   string
	}{
		{"acme/wrapper@v1", "acme/other@v2", "acme/other@v2"},
		{"acme/wrapper@v1", "./setup", "acme/wrapper/setup@v1"},
		{"acme/wrapper/sub@v1", "./setup", "acme/wrapper/setup@v1"},
		{"./local/action", "./nested", "./local/action/nested"},
	}
	for _, tt := range tests {
		if got := resolveChildRef(tt.parent, tt.child); got != tt.want {
			t.Errorf("resolveChildRef(%q, %q) = %q, want %q", tt.parent, tt.child, got, tt.want)
		}
	}
}
