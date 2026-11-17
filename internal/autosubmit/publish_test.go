package autosubmit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/smith"
)

func publicationFixture(t *testing.T) (string, string, snapshot) {
	t.Helper()
	root, dir := fixture(t, "true")
	writeFile(t, filepath.Join(root, "main.go"), renamed)
	_, err := Capture(t.Context(), root)
	require.NoError(t, err)
	db, err := openState(t.Context(), dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	s, err := next(t.Context(), db)
	require.NoError(t, err)
	commits, err := smith.ExtractSnapshot(t.Context(), root, s.base, s.tree)
	require.NoError(t, err)
	require.NotEmpty(t, commits)
	s.tip = commits[len(commits)-1].Hash
	s.prepared, err = candidateIndex(t.Context(), root, dir, s.tip)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE auto_snapshot SET state='publishing',tip=?,prepared=? WHERE id=?`, s.tip, s.prepared, s.id)
	require.NoError(t, err)
	return root, dir, s
}

func TestRecoveryCompletesBranchUpdateWithoutWritingFiles(t *testing.T) {
	root, dir, s := publicationFixture(t)
	require.NoError(t, writeIndexLock(filepath.Join(dir, "index.lock"), s.prepared))
	mustGit(t, root, "update-ref", s.branch, s.tip, s.base)
	require.NoError(t, Process(t.Context(), root))
	require.Equal(t, "published", state(t, dir))
	require.Empty(t, mustGit(t, root, "diff", "--cached"))
	content, err := os.ReadFile(filepath.Join(root, "main.go"))
	require.NoError(t, err)
	require.Equal(t, renamed, string(content))
}

func TestRecoveryRollsBackBeforeBranchUpdate(t *testing.T) {
	root, dir, s := publicationFixture(t)
	require.NoError(t, writeIndexLock(filepath.Join(dir, "index.lock"), s.prepared))
	require.NoError(t, Process(t.Context(), root))
	require.Equal(t, "queued", state(t, dir))
	require.Equal(t, s.base, mustGit(t, root, "rev-parse", "HEAD"))
	_, err := os.Stat(filepath.Join(dir, "index.lock"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRecoveryRefusesNewStagingAndForeignLock(t *testing.T) {
	for _, changeIndex := range []bool{true, false} {
		t.Run(map[bool]string{true: "staged", false: "foreign lock"}[changeIndex], func(t *testing.T) {
			root, dir, s := publicationFixture(t)
			mustGit(t, root, "update-ref", s.branch, s.tip, s.base)
			if changeIndex {
				writeFile(t, filepath.Join(root, "new.txt"), "staged")
				mustGit(t, root, "add", "new.txt")
			} else {
				writeFile(t, filepath.Join(dir, "index.lock"), "foreign lock")
			}
			index, err := os.ReadFile(filepath.Join(dir, "index"))
			require.NoError(t, err)
			require.Error(t, Process(t.Context(), root))
			after, err := os.ReadFile(filepath.Join(dir, "index"))
			require.NoError(t, err)
			require.Equal(t, index, after)
		})
	}
}

func TestPublicationRechecksAfterValidation(t *testing.T) {
	root, dir := fixture(t, "true")
	writeFile(t, filepath.Join(root, "main.go"), renamed)
	_, err := Capture(t.Context(), root)
	require.NoError(t, err)
	db, err := openState(t.Context(), dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	s, err := next(t.Context(), db)
	require.NoError(t, err)
	commits, err := smith.ExtractSnapshot(t.Context(), root, s.base, s.tree)
	require.NoError(t, err)
	s.tip = commits[len(commits)-1].Hash
	enable(t, dir)
	writeFile(t, filepath.Join(root, "main.go"), renamed+"// edited during validation\n")
	require.NoError(t, publish(t.Context(), db, root, dir, s))
	require.Equal(t, "skipped", state(t, dir))
	require.Equal(t, s.base, mustGit(t, root, "rev-parse", "HEAD"))
	require.Empty(t, mustGit(t, root, "diff", "--cached"))
}
