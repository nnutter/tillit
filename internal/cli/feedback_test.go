package cli_test

import (
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/auditor"
)

func TestFeedbackReportsGoFailureWithoutRunningChecks(t *testing.T) {
	dir := cleanRepo(t)
	writeConfig(t, dir, []string{"go", "test", "./..."}, "")
	head := commitFailingGoTest(t, dir)

	pending := execute(t, dir, "feedback", "--json")
	require.Contains(t, pending, `"validation":"pending"`)
	_, err := auditor.ValidateBranch(t.Context(), dir)
	require.NoError(t, err)

	// Feedback must not invoke the toolchain again, even if it's available.
	t.Setenv("GOFLAGS", "-unrecognized-test-option")
	out := execute(t, dir, "feedback", "--json")
	var report struct {
		Version int                     `json:"version"`
		Summary map[string]int          `json:"summary"`
		Commits []auditor.BranchOutcome `json:"commits"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &report))
	require.Equal(t, 1, report.Version)
	require.Equal(t, 1, report.Summary["failed"])
	require.Len(t, report.Commits, 1)
	commit := report.Commits[0]
	require.Equal(t, head, commit.Hash)
	require.Equal(t, "failed", commit.State)
	require.NotNil(t, commit.Evidence)
	require.Equal(t, []string{"go", "test", "-json", "./..."}, commit.Evidence.Command)
	require.Len(t, commit.Evidence.Failures, 1)
	failure := commit.Evidence.Failures[0]
	require.Equal(t, "test", failure.Kind)
	require.Equal(t, "example.com/fixture", failure.Package)
	require.Equal(t, "TestExample/fail+[case]", failure.Test)
	require.Contains(t, failure.Output, "intentional failure")
	require.NotContains(t, failure.Output, "passing sibling")
	require.Contains(t, execute(t, dir, "feedback"), "intentional failure")
}

func commitFailingGoTest(t *testing.T, dir string) string {
	t.Helper()
	const source = `package p
import "testing"
func TestExample(t *testing.T) {
	t.Run("passing", func(t *testing.T) { t.Log("passing sibling") })
	t.Run("fail+[case]", func(t *testing.T) { t.Error("intentional failure") })
}
`
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	defer func() { _ = repo.Close() }()
	wt, err := repo.Worktree()
	require.NoError(t, err)
	for name, body := range map[string]string{
		"go.mod":       "module example.com/fixture\n\ngo 1.25\n",
		"main_test.go": source,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
		_, err := wt.Add(name)
		require.NoError(t, err)
	}
	head, err := wt.Commit("add failing test", &git.CommitOptions{
		Author: new(object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()}),
	})
	require.NoError(t, err)
	return head.String()
}
