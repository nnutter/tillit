package status

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/nnutter/commitstack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/auditor"
	"github.com/nnutter/tillit/internal/ui"
	"github.com/nnutter/tillit/internal/verifier"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

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
					Status: verifier.Bypassed,
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

func TestTableShowsOnlyStatusBadgesWithoutDescriptions(t *testing.T) {
	branch, verification := sampleStatusData()
	m := NewModelWithRepo(branch, verification, true, ui.New(true, true), "/tmp/repo", 250*time.Millisecond)

	view := m.View().Content
	assert.Contains(t, view, "✓ passed")
	assert.Contains(t, view, "⚠ error")
	assert.Contains(t, view, "✓ verified")
	assert.Contains(t, view, "○ bypassed")
	assert.NotContains(t, view, "validate command is not configured")
	assert.NotContains(t, view, "commit must modify exactly one file")
}

func TestSelectedRowShowsIndicatorWithoutIndenting(t *testing.T) {
	branch, verification := sampleStatusData()
	m := NewModelWithRepo(branch, verification, true, ui.New(true, true), "/tmp/repo", 250*time.Millisecond)

	findLine := func(content, substr string) string {
		for line := range strings.SplitSeq(content, "\n") {
			plain := stripANSI(line)
			if strings.Contains(plain, substr) {
				return plain
			}
		}
		return ""
	}

	colOf := func(line, substr string) int {
		idx := strings.Index(line, substr)
		require.NotEqual(t, -1, idx, "substring %q not found in %q", substr, line)
		return lipgloss.Width(line[:idx])
	}

	view := m.View().Content
	header := findLine(view, "Commit")
	row0 := findLine(view, "First commit")
	row1 := findLine(view, "Second commit")
	require.NotEmpty(t, header)
	require.NotEmpty(t, row0)
	require.NotEmpty(t, row1)

	assert.Contains(t, row0, "▸")
	assert.NotContains(t, row1, "▸")
	assert.Equal(t, colOf(header, "Commit"), colOf(row0, "aaaaaaa"), "selected row should align with header")
	assert.Equal(t, colOf(row0, "aaaaaaa"), colOf(row1, "bbbbbbb"), "selected and unselected rows should align")

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	moved := next.(model)
	view = moved.View().Content
	row0 = findLine(view, "First commit")
	row1 = findLine(view, "Second commit")
	assert.NotContains(t, row0, "▸")
	assert.Contains(t, row1, "▸")
	assert.Equal(t, colOf(row0, "aaaaaaa"), colOf(row1, "bbbbbbb"), "alignment should hold after moving selection")
}

func TestAutoRefreshMetadataAppearsInFooter(t *testing.T) {
	branch, verification := sampleStatusData()
	m := NewModelWithRepo(branch, verification, true, ui.New(true, true), "/tmp/repo", 250*time.Millisecond)

	plain := stripANSI(m.View().Content)
	tableIdx := strings.Index(plain, "Second commit")
	helpIdx := strings.Index(plain, "view feedback")
	refreshIdx := strings.Index(plain, "auto-refresh")

	require.NotEqual(t, -1, tableIdx)
	require.NotEqual(t, -1, helpIdx)
	require.NotEqual(t, -1, refreshIdx)
	assert.Greater(t, refreshIdx, tableIdx, "auto-refresh metadata should appear after the commit table")
	assert.Greater(t, refreshIdx, helpIdx, "auto-refresh metadata should appear below the help footer")
}
