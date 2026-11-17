package mender

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type operation struct {
	branch, oldHead, oldTip, oldBoundary, prefix, tip string
}

func state(ctx context.Context, gitDir string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(gitDir, "spor", "state.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS mender_state (id INTEGER PRIMARY KEY CHECK(id = 1), boundary TEXT NOT NULL DEFAULT '', branch TEXT NOT NULL DEFAULT '', old_head TEXT NOT NULL DEFAULT '', old_tip TEXT NOT NULL DEFAULT '', old_boundary TEXT NOT NULL DEFAULT '', prefix TEXT NOT NULL DEFAULT '', tip TEXT NOT NULL DEFAULT '')`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO mender_state(id) VALUES (1)`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func pending(ctx context.Context, db *sql.DB) (operation, error) {
	var op operation
	err := db.QueryRowContext(ctx, `SELECT branch, old_head, old_tip, old_boundary, prefix, tip FROM mender_state WHERE id = 1`).Scan(&op.branch, &op.oldHead, &op.oldTip, &op.oldBoundary, &op.prefix, &op.tip)
	return op, err
}

func save(ctx context.Context, db *sql.DB, op operation) error {
	res, err := db.ExecContext(ctx, `UPDATE mender_state SET branch=?, old_head=?, old_tip=?, old_boundary=?, prefix=?, tip=? WHERE id=1 AND branch='' AND boundary=?`, op.branch, op.oldHead, op.oldTip, op.oldBoundary, op.prefix, op.tip, op.oldBoundary)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("mender state changed while preparing")
	}
	return nil
}

func finish(ctx context.Context, db *sql.DB, op operation) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Submissions made before integration are a reset barrier.
	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS mender_retired_submission (id TEXT PRIMARY KEY)`)
	if err != nil {
		return err
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='submission'`).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO mender_retired_submission SELECT id FROM submission`); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE mender_state SET boundary=?, branch='', old_head='', old_tip='', old_boundary='', prefix='', tip='' WHERE id=1 AND branch=? AND old_head=? AND tip=?`, op.prefix, op.branch, op.oldHead, op.tip)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("mender journal changed")
	}
	return tx.Commit()
}
