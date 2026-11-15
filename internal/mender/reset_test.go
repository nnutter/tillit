package mender_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/mender"
	"github.com/nnutter/spor/internal/repository"
	"github.com/nnutter/spor/internal/smith"
)

func TestResetStopsAtIntegrationBarrierAndRestoresPreviousTip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-b", "user")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "config", "user.email", "test@example.com")
	write(t, dir, "main.go", "package p\nfunc f() { x := 1; _ = x }\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	write(t, dir, "main.go", "package p\nfunc f() { y := 1; _ = y }\n")
	git(t, dir, "add", "main.go")
	added, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.NotEmpty(t, added.Commits)
	gitDir := filepath.Join(dir, ".git")
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "auditor.verify.command", []string{"sh", "-c", "true"}))
	_, err = auditor.Verify(t.Context(), dir, "")
	require.NoError(t, err)
	git(t, dir, "reset", "--hard", "HEAD")
	_, err = mender.Rebase(t.Context(), dir, "refs/heads/user")
	require.NoError(t, err)
	tip := git(t, dir, "rev-parse", repository.HeadRef(gitDir))
	_, err = smith.Reset(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, tip, git(t, dir, "rev-parse", repository.HeadRef(gitDir)))
	write(t, dir, "main.go", "package p\nfunc f() { z := 1; _ = z }\n")
	git(t, dir, "add", "main.go")
	added, err = smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.NotEmpty(t, added.Commits)
	_, err = smith.Reset(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, tip, git(t, dir, "rev-parse", repository.HeadRef(gitDir)))
}
