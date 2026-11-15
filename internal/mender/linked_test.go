package mender_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/mender"
	"github.com/nnutter/spor/internal/repository"
)

func TestIntegratesIntoLinkedWorktreeOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
		t.Setenv(key, "")
	}
	root := t.TempDir()
	main := filepath.Join(root, "main")
	git(t, root, "init", "-b", "main", main)
	git(t, main, "config", "user.name", "Test")
	git(t, main, "config", "user.email", "test@example.com")
	write(t, main, "base", "base\n")
	git(t, main, "add", ".")
	git(t, main, "commit", "-m", "base")
	linked := filepath.Join(root, "linked")
	git(t, main, "worktree", "add", "-b", "linked", linked)
	git(t, linked, "checkout", "--detach")
	write(t, linked, "change", "change\n")
	git(t, linked, "add", "change")
	git(t, linked, "commit", "-m", "change\n\nSpor-Id: S1\nChange-Id: I1")
	repo, gitDir, err := repository.Open(linked)
	require.NoError(t, err)
	require.NoError(t, repo.Close())
	git(t, linked, "update-ref", repository.HeadRef(gitDir), "HEAD")
	git(t, linked, "checkout", "linked")
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "auditor.verify.command", []string{"sh", "-c", "true"}))
	_, err = auditor.Verify(t.Context(), linked, "")
	require.NoError(t, err)
	_, err = mender.Rebase(t.Context(), linked, "refs/heads/linked")
	require.NoError(t, err)
	require.Equal(t, "change\nbase", git(t, linked, "log", "--format=%s"))
	require.Equal(t, "base", git(t, main, "log", "--format=%s"))
}
