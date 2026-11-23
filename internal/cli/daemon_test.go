package cli_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/cli"
)

func TestDaemonAutoPublishesAndStopsItsWatcher(t *testing.T) {
	dir := cleanRepo(t)
	writeConfig(t, dir, []string{"true"}, "")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(after), 0o644))
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	defer func() { _ = repo.Close() }()
	base, err := repo.Head()
	require.NoError(t, err)
	root := cli.New("test")
	root.SetArgs([]string{"--repo", dir, "start", "--foreground"})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	require.Eventually(t, func() bool {
		head, err := repo.Head()
		if err != nil {
			return false
		}
		return head.Hash() != base.Hash()
	}, 15*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool {
		out, err := runCLI(t, dir, "status")
		return err == nil && strings.Contains(out, "watcher: running") && strings.Contains(out, "published")
	}, 5*time.Second, 20*time.Millisecond)
	content, err := os.ReadFile(filepath.Join(dir, "main.go"))
	require.NoError(t, err)
	assert.Equal(t, after, string(content))
	worktree, err := repo.Worktree()
	require.NoError(t, err)
	status, err := worktree.Status()
	require.NoError(t, err)
	assert.False(t, status.IsClean(), "the unfinished feature must remain in the working diff")
	assert.Equal(t, git.Unmodified, status.File("main.go").Staging)
	assert.Equal(t, git.Modified, status.File("main.go").Worktree)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, ".git", "spor", "state.db"))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	cancel()
	require.Eventually(t, func() bool {
		var count int
		err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM auto_session`).Scan(&count)
		if err != nil {
			return false
		}
		return count == 0
	}, 5*time.Second, 10*time.Millisecond)
}
