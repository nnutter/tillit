package daemon_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/testutil"
)

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
	testutil.IsolateGit(t)
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	cfg, err := repo.Config()
	require.NoError(t, err)
	cfg.User.Name = "Tillit Test"
	cfg.User.Email = "tillit@example.com"
	require.NoError(t, repo.SetConfig(cfg))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(beforeFile), 0o644))
	worktree, err := repo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add("main.go")
	require.NoError(t, err)
	_, err = worktree.Commit("init", &git.CommitOptions{Author: &object.Signature{
		Name:  "Tillit Test",
		Email: "tillit@example.com",
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
