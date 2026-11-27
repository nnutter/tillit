package status

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/nnutter/commitstack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/auditor"
	"github.com/nnutter/tillit/internal/ui"
	"github.com/nnutter/tillit/internal/verifier"
)

func sampleStatusData() (auditor.BranchResult, verifier.BranchResult) {
	branch := auditor.BranchResult{
		Snapshot: commitstack.Snapshot{
			Branch: "feature",
			Head:   "aaaaaaa1111111",
			Base:   "00000000000000",
		},
		Outcomes: []auditor.BranchOutcome{
			{
				Hash:    "aaaaaaa1111111",
				Subject: "First commit",
				State:   "passed",
			},
			{
				Hash:    "bbbbbbb2222222",
				Subject: "Second commit",
				State:   "error",
				Error:   "validate command is not configured",
			},
		},
	}
	verification := verifier.BranchResult{
		Snapshot: branch.Snapshot,
		Outcomes: []verifier.BranchOutcome{
			{
				Hash:   "aaaaaaa1111111",
				Result: &verifier.Result{Status: verifier.Verified},
			},
			{
				Hash: "bbbbbbb2222222",
				Result: &verifier.Result{
					Status: verifier.Unsupported,
					Reason: "commit must modify exactly one file",
				},
			},
		},
	}
	return branch, verification
}

func TestViewStacksValidationAndVerificationSummaries(t *testing.T) {
	branch, verification := sampleStatusData()
	m := NewModelWithRepo(branch, verification, true, ui.New(true, true), "/tmp/repo", 250*time.Millisecond)

	var validLine, verifLine string
	for line := range strings.SplitSeq(m.View().Content, "\n") {
		if strings.Contains(line, "Validation:") {
			validLine = line
		}
		if strings.Contains(line, "Verification:") {
			verifLine = line
		}
	}

	require.NotEmpty(t, validLine)
	require.NotEmpty(t, verifLine)
	assert.NotEqual(t, validLine, verifLine, "validation and verification summaries should be on separate lines")
	assert.LessOrEqual(t, lipgloss.Width(strings.TrimSpace(validLine)), 80)
	assert.LessOrEqual(t, lipgloss.Width(strings.TrimSpace(verifLine)), 80)
}
