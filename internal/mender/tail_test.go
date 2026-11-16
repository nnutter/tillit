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

func TestConflictingPendingTailBlocksWholeIntegration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-b", "user")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "config", "user.email", "test@example.com")
	write(t, dir, "file", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "checkout", "--detach")
	write(t, dir, "A", "A\n")
	git(t, dir, "add", "A")
	git(t, dir, "commit", "-m", "A\n\nSpor-Id: S1\nChange-Id: IA")
	write(t, dir, "file", "spor\n")
	write(t, dir, "bad", "bad\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "C\n\nSpor-Id: S1\nChange-Id: IC")
	gitDir := filepath.Join(dir, ".git")
	git(t, dir, "update-ref", repository.HeadRef(gitDir), "HEAD")
	tip := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "user")
	write(t, dir, "file", "user\n")
	git(t, dir, "add", "file")
	git(t, dir, "commit", "-m", "user")
	head := git(t, dir, "rev-parse", "HEAD")
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "auditor.validate.command", []string{"sh", "-c", "test ! -f bad"}))
	result, err := auditor.Validate(t.Context(), dir, "")
	require.NoError(t, err)
	require.True(t, result.Failed)
	_, err = mender.Rebase(t.Context(), dir, "refs/heads/user")
	require.ErrorContains(t, err, "mender.conflict.model")
	require.Equal(t, head, git(t, dir, "rev-parse", "HEAD"))
	require.Equal(t, tip, git(t, dir, "rev-parse", repository.HeadRef(gitDir)))
	require.Empty(t, git(t, dir, "status", "--porcelain"))
}
