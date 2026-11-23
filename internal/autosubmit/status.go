package autosubmit

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Status describes the watcher and its latest captured snapshot.
type Status struct {
	Active     bool
	SnapshotID string
	State      string
	Result     string
}

// Report reads the worktree's watcher heartbeat and most recent snapshot.
func Report(ctx context.Context, start string) (Status, error) {
	var status Status
	_, dir, err := location(start)
	if err != nil {
		return status, err
	}
	db, err := openState(ctx, dir)
	if err != nil {
		return status, err
	}
	defer func() { _ = db.Close() }()
	var expires int64
	err = db.QueryRowContext(ctx, `SELECT expires FROM auto_session WHERE id=1`).Scan(&expires)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return status, err
	}
	status.Active = expires > time.Now().UnixNano()
	err = db.QueryRowContext(ctx, `SELECT id,state,result FROM auto_snapshot ORDER BY id DESC LIMIT 1`).Scan(&status.SnapshotID, &status.State, &status.Result)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return status, err
}
