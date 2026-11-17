package daemon_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/autosubmit"
	"github.com/nnutter/spor/internal/daemon"
)

func TestRunAutoReturnsWatcherFailureAndReleasesDaemon(t *testing.T) {
	dir, _ := branchRepo(t)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, ".git", "spor", "state.db"))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- autosubmit.Run(ctx, dir) }()
	t.Cleanup(func() { stop(); require.NoError(t, <-done) })
	require.Eventually(t, func() bool {
		var count int
		err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM auto_session WHERE expires > ?`, time.Now().UnixNano()).Scan(&count)
		if err != nil {
			return false
		}
		return count == 1
	}, 5*time.Second, 10*time.Millisecond)
	// Bound the test if a regression starts the daemon without its watcher.
	attempt, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.ErrorContains(t, daemon.RunAuto(attempt, dir), "auto-submit is already running")
	// A plain daemon can still run alongside the existing watcher after failure.
	once, stopDaemon := context.WithCancel(t.Context())
	stopDaemon()
	require.NoError(t, daemon.Run(once, dir))
}

func TestRunAutoDoesNotStartWatcherWhenDaemonAlreadyRunning(t *testing.T) {
	dir, logPath := branchRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx, dir) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	require.Eventually(t, func() bool {
		_, err := os.Stat(logPath)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	require.ErrorIs(t, daemon.RunAuto(t.Context(), dir), daemon.ErrAlreadyRunning)
	_, err := os.Stat(filepath.Join(dir, ".git", "spor", "auto-submit.lock"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
