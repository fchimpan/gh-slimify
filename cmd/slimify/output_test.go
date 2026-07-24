package main

import (
	"testing"
	"time"

	"github.com/fchimpan/gh-slimify/internal/scan"
)

func TestClassifyCandidates(t *testing.T) {
	safeJob := &scan.Candidate{JobID: "safe", RawDuration: 4 * time.Minute}
	missingCmds := &scan.Candidate{JobID: "missing", RawDuration: 3 * time.Minute, MissingCommands: []string{"mysql"}}
	unknownDur := &scan.Candidate{JobID: "unknown"}
	nearLimit := &scan.Candidate{JobID: "near-limit", RawDuration: 12 * time.Minute}

	safe, warning := classifyCandidates([]*scan.Candidate{safeJob, missingCmds, unknownDur, nearLimit})

	if len(safe) != 1 || safe[0].JobID != "safe" {
		t.Errorf("safe = %+v, want only the 4m job with no missing commands", jobIDs(safe))
	}

	wantWarnings := map[string]bool{"missing": true, "unknown": true, "near-limit": true}
	if len(warning) != len(wantWarnings) {
		t.Fatalf("warning = %v, want %v", jobIDs(warning), wantWarnings)
	}
	for _, c := range warning {
		if !wantWarnings[c.JobID] {
			t.Errorf("unexpected warning job %s", c.JobID)
		}
	}
}

func jobIDs(cs []*scan.Candidate) []string {
	ids := make([]string, len(cs))
	for i, c := range cs {
		ids[i] = c.JobID
	}
	return ids
}
