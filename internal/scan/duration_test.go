package scan

import (
	"strings"
	"testing"
	"time"

	"github.com/fchimpan/gh-slimify/internal/workflow"
)

func TestEnforceDurationLimit(t *testing.T) {
	candidates := []*Candidate{
		{JobID: "fast", Duration: "4m", RawDuration: 4 * time.Minute},
		{JobID: "near-limit", Duration: "12m", RawDuration: 12 * time.Minute},
		{JobID: "over-limit", Duration: "23m", RawDuration: 23 * time.Minute},
		{JobID: "unknown"}, // duration not fetched
	}

	remaining, ineligible := enforceDurationLimit(candidates, nil)

	if len(remaining) != 3 {
		t.Fatalf("remaining = %d candidates, want 3: %+v", len(remaining), remaining)
	}
	for _, c := range remaining {
		if c.JobID == "over-limit" {
			t.Error("job exceeding 15 minutes must not remain a candidate")
		}
	}

	if len(ineligible) != 1 {
		t.Fatalf("ineligible = %d jobs, want 1", len(ineligible))
	}
	if ineligible[0].JobID != "over-limit" {
		t.Errorf("ineligible job = %s, want over-limit", ineligible[0].JobID)
	}
	if len(ineligible[0].Reasons) != 1 || !strings.Contains(ineligible[0].Reasons[0], "15-minute limit") {
		t.Errorf("reason = %v, want mention of the 15-minute limit", ineligible[0].Reasons)
	}
}

func TestCheckEligibility_RunnerKinds(t *testing.T) {
	tests := []struct {
		name         string
		job          *workflow.Job
		wantEligible bool
		wantReason   string
	}{
		{
			name:         "ubuntu-24.04 is migratable",
			job:          &workflow.Job{RunsOn: "ubuntu-24.04", Steps: []workflow.Step{{Run: "echo hi"}}},
			wantEligible: true,
		},
		{
			name:       "ubuntu-22.04 is not migratable",
			job:        &workflow.Job{RunsOn: "ubuntu-22.04"},
			wantReason: "does not run on ubuntu-latest or ubuntu-24.04",
		},
		{
			name:       "reusable workflow call",
			job:        &workflow.Job{Uses: "owner/repo/.github/workflows/ci.yml@v1"},
			wantReason: "calls a reusable workflow",
		},
		{
			name:       "matrix expression runs-on",
			job:        &workflow.Job{RunsOn: "${{ matrix.os }}"},
			wantReason: "set by an expression",
		},
		{
			name:       "multi-label runs-on",
			job:        &workflow.Job{RunsOn: []any{"ubuntu-latest", "self-hosted"}},
			wantReason: "multiple runner labels",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eligible, reasons := checkEligibility(tt.job)
			if eligible != tt.wantEligible {
				t.Fatalf("checkEligibility() = %v (reasons %v), want %v", eligible, reasons, tt.wantEligible)
			}
			if tt.wantReason != "" {
				if len(reasons) == 0 || !strings.Contains(reasons[0], tt.wantReason) {
					t.Errorf("reasons = %v, want mention of %q", reasons, tt.wantReason)
				}
			}
		})
	}
}

func TestCandidate_NearDurationLimit(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     bool
	}{
		{"unknown duration", 0, false},
		{"well under threshold", 5 * time.Minute, false},
		{"at threshold", SlimDurationWarnThreshold, false},
		{"above threshold", 12 * time.Minute, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Candidate{RawDuration: tt.duration}
			if got := c.NearDurationLimit(); got != tt.want {
				t.Errorf("NearDurationLimit() = %v, want %v", got, tt.want)
			}
		})
	}
}
