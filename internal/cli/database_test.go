package cli_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStatusWaitsForTransientDatabaseWriter(t *testing.T) {
	dir := cleanRepo(t)
	_, err := runCLI(t, dir, "status")
	require.NoError(t, err)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, ".git", "spor", "state.db"))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	conn, err := db.Conn(t.Context())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = conn.ExecContext(t.Context(), "BEGIN EXCLUSIVE")
	require.NoError(t, err)
	defer func() { _, _ = conn.ExecContext(t.Context(), "ROLLBACK") }()
	done := make(chan error, 1)
	go func() { _, err := runCLI(t, dir, "status"); done <- err }()
	select {
	case err := <-done:
		require.FailNow(t, "status returned before the write lock was released", "error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	_, err = conn.ExecContext(t.Context(), "ROLLBACK")
	require.NoError(t, err)
	require.NoError(t, <-done)
}
