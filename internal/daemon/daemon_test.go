package daemon_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/daemon"
	"github.com/nnutter/spor/internal/smith"
)

func TestProcessPendingAdd(t *testing.T) {
	dir := newRepo(t)
	require.NoError(t, daemon.EnqueueAdd(t.Context(), dir))
	status, err := smith.Status(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, "No submissions.\n", smith.Format(status))

	ok, err := daemon.ProcessPending(t.Context(), dir)
	require.NoError(t, err)
	require.True(t, ok)

	status, err = smith.Status(t.Context(), dir)
	require.NoError(t, err)
	require.NotEmpty(t, status.Commits)

	ok, err = daemon.ProcessPending(t.Context(), dir)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestEnqueueVerifyRejectsUnknownMode(t *testing.T) {
	dir := newRepo(t)
	require.ErrorContains(t, daemon.EnqueueVerify(t.Context(), dir, "nope"), "nope")
}

const beforeFile = `package p
func f() {
	x := 1
	_ = x
}
`

const afterFile = `package p
func f() {
	y := 1
	_ = y
}
`

func newRepo(t *testing.T) string {
	t.Helper()
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
		t.Setenv(key, "")
	}
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	cfg, err := repo.Config()
	require.NoError(t, err)
	cfg.User.Name = "Spor Test"
	cfg.User.Email = "spor@example.com"
	require.NoError(t, repo.SetConfig(cfg))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(beforeFile), 0o644))
	worktree, err := repo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add("main.go")
	require.NoError(t, err)
	_, err = worktree.Commit("init", &git.CommitOptions{Author: &object.Signature{
		Name:  "Spor Test",
		Email: "spor@example.com",
		When:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(afterFile), 0o644))
	worktree, err = repo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add("main.go")
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	return dir
}
