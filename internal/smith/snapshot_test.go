package smith_test

import (
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/smith"
)

func TestExtractSnapshotOmitsRemainderAndManualSubmission(t *testing.T) {
	repo, dir := newRepo(t, "main.go", []byte(renameBefore))
	stage(t, repo, dir, "main.go", []byte(renameAfter))
	base, index := snapshot(t, repo)
	tree := strings.TrimSpace(runGit(t, "-C", dir, "write-tree"))
	commits, err := smith.ExtractSnapshot(t.Context(), dir, base, tree)
	require.NoError(t, err)
	require.Len(t, commits, 1)
	commit, err := repo.CommitObject(plumbing.NewHash(commits[0].Hash))
	require.NoError(t, err)
	require.Equal(t, renamePeeled, fileText(t, commit, "main.go"))
	require.Equal(t, base, headHash(t, repo))
	require.Equal(t, index, indexSnapshot(t, repo))
	status, err := smith.Status(t.Context(), dir)
	require.NoError(t, err)
	require.Empty(t, status.Commits)
}
