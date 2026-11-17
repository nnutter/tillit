package mender_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/mender"
	"github.com/nnutter/spor/internal/repository"
)

func TestConflictNeedsConfiguredAgentAndLeavesUserBranchUntouched(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-b", "user")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "config", "user.email", "test@example.com")
	write(t, dir, "file", "base\n")
	git(t, dir, "add", "file")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "checkout", "--detach")
	write(t, dir, "file", "spor\n")
	git(t, dir, "add", "file")
	git(t, dir, "commit", "-m", "spor\n\nSpor-Id: S1\nChange-Id: I1")
	gitDir := filepath.Join(dir, ".git")
	git(t, dir, "update-ref", repository.HeadRef(gitDir), "HEAD")
	git(t, dir, "checkout", "user")
	write(t, dir, "file", "user\n")
	git(t, dir, "add", "file")
	git(t, dir, "commit", "-m", "user")
	before := git(t, dir, "rev-parse", "HEAD")
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "auditor.validate.command", []string{"sh", "-c", "true"}))
	result, err := auditor.Validate(t.Context(), dir, "")
	require.NoError(t, err)
	require.False(t, result.Failed)
	_, err = mender.Rebase(t.Context(), dir, "refs/heads/user")
	require.ErrorContains(t, err, "mender.conflict.model")
	require.Equal(t, before, git(t, dir, "rev-parse", "HEAD"))
	body, err := os.ReadFile(filepath.Join(dir, "file"))
	require.NoError(t, err)
	require.Equal(t, "user\n", string(body))
	require.Empty(t, git(t, dir, "status", "--porcelain"))
	bin := t.TempDir()
	write(t, bin, "pi", "#!/bin/sh\nprintf 'resolved\\n' > file\n")
	require.NoError(t, os.Chmod(filepath.Join(bin, "pi"), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "mender.conflict.model.provider", []string{"test"}))
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "mender.conflict.model.version", []string{"test"}))
	_, err = mender.Rebase(t.Context(), dir, "refs/heads/user")
	require.NoError(t, err)
	body, err = os.ReadFile(filepath.Join(dir, "file"))
	require.NoError(t, err)
	require.Equal(t, "resolved\n", string(body))
}
