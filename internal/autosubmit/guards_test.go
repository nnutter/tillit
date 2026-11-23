package autosubmit

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/repository"
)

func TestManualSubmissionBlocksAutomaticPublication(t *testing.T) {
	root, dir := fixture(t, "true")
	base := mustGit(t, root, "rev-parse", "HEAD")
	mustGit(t, root, "commit", "--allow-empty", "-m", "legacy", "-m", "Spor-Id: Slegacy")
	legacy := mustGit(t, root, "rev-parse", "HEAD")
	ref := repository.HeadRef(dir)
	mustGit(t, root, "update-ref", ref, legacy)
	mustGit(t, root, "reset", "--hard", base)
	writeFile(t, filepath.Join(root, "main.go"), renamed)
	_, err := Capture(t.Context(), root)
	require.ErrorContains(t, err, "manual Spor commits")
	require.ErrorContains(t, err, "previous Spor version")
	require.Equal(t, legacy, mustGit(t, root, "rev-parse", ref))
}

func TestIntegratedLegacyBoundaryDoesNotBlockPublication(t *testing.T) {
	root, dir := fixture(t, "true")
	base := mustGit(t, root, "rev-parse", "HEAD")
	mustGit(t, root, "commit", "--allow-empty", "-m", "legacy", "-m", "Spor-Id: Slegacy")
	legacy := mustGit(t, root, "rev-parse", "HEAD")
	mustGit(t, root, "update-ref", repository.HeadRef(dir), legacy)
	mustGit(t, root, "reset", "--hard", base)
	db, err := sql.Open("sqlite", filepath.Join(dir, "spor", "state.db"))
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE mender_state (id INTEGER PRIMARY KEY, branch TEXT, boundary TEXT); INSERT INTO mender_state VALUES (1, '', ?)`, legacy)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	writeFile(t, filepath.Join(root, "main.go"), renamed)
	message, err := Capture(t.Context(), root)
	require.NoError(t, err)
	require.Contains(t, message, "Submitted")
}

func TestInterruptedLegacyIntegrationBlocksPublication(t *testing.T) {
	root, dir := fixture(t, "true")
	db, err := sql.Open("sqlite", filepath.Join(dir, "spor", "state.db"))
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE mender_state (id INTEGER PRIMARY KEY, branch TEXT); INSERT INTO mender_state VALUES (1, 'refs/heads/main')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	writeFile(t, filepath.Join(root, "main.go"), renamed)
	base := mustGit(t, root, "rev-parse", "HEAD")
	_, err = Capture(t.Context(), root)
	require.ErrorContains(t, err, "pending integration")
	require.ErrorContains(t, err, "previous Spor version")
	require.Equal(t, base, mustGit(t, root, "rev-parse", "HEAD"))
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
