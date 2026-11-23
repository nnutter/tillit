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
	ready := make(chan struct{}, 1)
	err = daemon.RunReady(attempt, dir, func() { ready <- struct{}{} })
	require.ErrorContains(t, err, "auto-submit is already running")
	require.Empty(t, ready)
	// A plain daemon can still run alongside the existing watcher after failure.
	once, stopDaemon := context.WithCancel(t.Context())
	stopDaemon()
	require.NoError(t, daemon.Run(once, dir))
}

func TestRunAutoReadyOwnsWatcher(t *testing.T) {
	dir, _ := branchRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- daemon.RunReady(ctx, dir, func() { close(ready) }) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	select {
	case <-ready:
	case err := <-done:
		done <- err
		require.NoError(t, err)
		require.FailNow(t, "daemon stopped before readiness")
	case <-time.After(10 * time.Second):
		require.FailNow(t, "daemon did not become ready")
	}
	require.ErrorContains(t, autosubmit.Run(t.Context(), dir), "already running")
}

func TestRunAutoDoesNotStartWatcherWhenDaemonAlreadyRunning(t *testing.T) {
	dir, _ := branchRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx, dir) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	require.Eventually(t, func() bool { return lockExists(t, dir) }, 5*time.Second, 10*time.Millisecond)
	require.ErrorIs(t, daemon.RunReady(t.Context(), dir, nil), daemon.ErrAlreadyRunning)
	_, err := os.Stat(filepath.Join(dir, ".git", "spor", "auto-submit.lock"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
