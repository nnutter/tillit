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
	"github.com/nnutter/tillit/internal/daemon"
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
	m := NewModelWithRepo(branch, verification, daemon.State{Running: true}, ui.New(true, true), "/tmp/repo", 250*time.Millisecond)

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
	m := NewModelWithRepo(branch, verification, daemon.State{Running: true}, ui.New(true, true), "/tmp/repo", 250*time.Millisecond)

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
	m := NewModelWithRepo(branch, verification, daemon.State{Running: true}, ui.New(true, true), "/tmp/repo", 250*time.Millisecond)

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

func TestViewOmitsAutoRefreshFooter(t *testing.T) {
	branch, verification := sampleStatusData()
	m := NewModelWithRepo(branch, verification, daemon.State{Running: true}, ui.New(true, true), "/tmp/repo", 250*time.Millisecond)

	plain := stripANSI(m.View().Content)
	assert.NotContains(t, plain, "auto-refresh")
	assert.NotContains(t, plain, "last updated")
}

func TestHeaderLayout(t *testing.T) {
	branch, verification := sampleStatusData()
	m := NewModelWithRepo(branch, verification, daemon.State{Running: true}, ui.New(true, true), "/tmp/repo", 250*time.Millisecond)

	view := m.View()
	assert.Equal(t, "Tillit", view.WindowTitle)

	lines := strings.Split(stripANSI(view.Content), "\n")
	titleIdx, metaIdx, validIdx := -1, -1, -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Tillit" {
			titleIdx = i
		}
		if strings.Contains(line, "Branch:") {
			metaIdx = i
		}
		if strings.Contains(line, "Validation:") {
			validIdx = i
		}
	}

	require.NotEqual(t, -1, titleIdx, "expected standalone Tillit title line")
	require.NotEqual(t, -1, metaIdx, "expected branch metadata line")
	require.NotEqual(t, -1, validIdx, "expected validation summary line")
	assert.Equal(t, strings.Index(lines[metaIdx], "Branch:"), strings.Index(lines[titleIdx], "Tillit"), "Tillit title should align with Branch without extra indent")
	assert.Contains(t, lines[metaIdx], "Daemon:")
	assert.Equal(t, metaIdx+2, validIdx, "expected blank line between metadata line and summaries")
	assert.Empty(t, strings.TrimSpace(lines[metaIdx+1]))
}

func TestTableColumnOrderAndExpandedSubject(t *testing.T) {
	branch, verification := sampleStatusData()
	longSubject := "Refactor status table layout so commit subjects use remaining terminal width"
	branch.Outcomes[0].Subject = longSubject
	m := NewModelWithRepo(branch, verification, daemon.State{Running: true}, ui.New(true, true), "/tmp/repo", 250*time.Millisecond)

	resized, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	plain := stripANSI(resized.(model).View().Content)

	var header string
	for line := range strings.SplitSeq(plain, "\n") {
		if strings.Contains(line, "Commit Subject") {
			header = line
			break
		}
	}
	require.NotEmpty(t, header)

	commitPos := strings.Index(header, "Commit")
	validPos := strings.Index(header, "Validation")
	verifPos := strings.Index(header, "Verification")
	subjPos := strings.Index(header, "Commit Subject")
	assert.Less(t, commitPos, validPos)
	assert.Less(t, validPos, verifPos)
	assert.Less(t, verifPos, subjPos)
	assert.Contains(t, plain, longSubject)
}

func TestViewShowsDaemonNeedsRestart(t *testing.T) {
	branch, verification := sampleStatusData()

	synced := NewModelWithRepo(branch, verification, daemon.State{Running: true}, ui.New(true, true), "/tmp/repo", 250*time.Millisecond)
	assert.NotContains(t, stripANSI(synced.View().Content), "(needs restart)")

	stale := NewModelWithRepo(branch, verification, daemon.State{Running: true, NeedsRestart: true}, ui.New(true, true), "/tmp/repo", 250*time.Millisecond)
	assert.Contains(t, stripANSI(stale.View().Content), "● running (needs restart)")
}
