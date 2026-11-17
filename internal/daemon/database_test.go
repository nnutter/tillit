package daemon_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/daemon"
)

func TestEnqueueWaitsForTransientDatabaseWriter(t *testing.T) {
	dir := newRepo(t)
	require.NoError(t, daemon.EnqueueAdd(t.Context(), dir))
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, ".git", "spor", "state.db"))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	conn, err := db.Conn(t.Context())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = conn.ExecContext(t.Context(), "BEGIN IMMEDIATE")
	require.NoError(t, err)
	defer func() { _, _ = conn.ExecContext(t.Context(), "ROLLBACK") }()
	done := make(chan error, 1)
	go func() { done <- daemon.EnqueueValidate(t.Context(), dir, "") }()
	select {
	case err := <-done:
		require.FailNow(t, "enqueue returned before the write lock was released", "error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	_, err = conn.ExecContext(t.Context(), "ROLLBACK")
	require.NoError(t, err)
	require.NoError(t, <-done)
}
