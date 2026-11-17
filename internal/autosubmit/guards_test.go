package autosubmit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/smith"
)

func TestManualSubmissionBlocksAutomaticPublication(t *testing.T) {
	root, _ := fixture(t, "true")
	writeFile(t, filepath.Join(root, "main.go"), renamed)
	mustGit(t, root, "add", "main.go")
	result, err := smith.Add(t.Context(), root)
	require.NoError(t, err)
	require.NotEmpty(t, result.Commits)
	mustGit(t, root, "reset", "--mixed", "HEAD")
	_, err = Capture(t.Context(), root)
	require.ErrorContains(t, err, "manual Spor commits")
	manual, err := smith.Status(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, result.SporID, manual.SporID)
}

func TestMissingValidationCommandDoesNotPublish(t *testing.T) {
	root, dir := fixture(t, "true")
	writeFile(t, filepath.Join(dir, "spor", "config.json"), "{}")
	writeFile(t, filepath.Join(root, "main.go"), renamed)
	base := mustGit(t, root, "rev-parse", "HEAD")
	_, err := Capture(t.Context(), root)
	require.NoError(t, err)
	enable(t, dir)
	require.NoError(t, Process(t.Context(), root))
	require.Equal(t, "failed", state(t, dir))
	require.Equal(t, base, mustGit(t, root, "rev-parse", "HEAD"))
}

func TestLinkedWorktreeUsesItsOwnIndexAndQueue(t *testing.T) {
	main, _ := fixture(t, "true")
	linked := filepath.Join(t.TempDir(), "linked")
	mustGit(t, main, "worktree", "add", "-b", "linked", linked)
	_, dir, err := location(linked)
	require.NoError(t, err)
	base := mustGit(t, main, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(linked, "main.go"), renamed)
	_, err = Capture(t.Context(), linked)
	require.NoError(t, err)
	enable(t, dir)
	require.NoError(t, Process(t.Context(), linked))
	require.Equal(t, "published", state(t, dir))
	require.Empty(t, mustGit(t, linked, "diff", "--cached"))
	require.Equal(t, base, mustGit(t, main, "rev-parse", "HEAD"))
	require.Empty(t, mustGit(t, main, "status", "--porcelain"))
}

func TestIndexLockPreventsPublication(t *testing.T) {
	root, dir := fixture(t, "true")
	base := mustGit(t, root, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(root, "main.go"), renamed)
	_, err := Capture(t.Context(), root)
	require.NoError(t, err)
	enable(t, dir)
	writeFile(t, filepath.Join(dir, "index.lock"), "external git lock")
	require.NoError(t, Process(t.Context(), root))
	require.Equal(t, base, mustGit(t, root, "rev-parse", "HEAD"))
	data, err := os.ReadFile(filepath.Join(dir, "index.lock"))
	require.NoError(t, err)
	require.Equal(t, "external git lock", string(data))
}
