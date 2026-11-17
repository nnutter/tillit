package autosubmit

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type snapshot struct {
	id        string
	branch    string
	base      string
	tree      string
	indexHash string
	state     string
	tip       string
	prepared  []byte
}

func openState(ctx context.Context, dir string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Join(dir, "spor"), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "spor", "state.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	_, err = db.ExecContext(ctx, `
 CREATE TABLE IF NOT EXISTS auto_session (id INTEGER PRIMARY KEY CHECK(id=1), expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS auto_snapshot (
 id TEXT PRIMARY KEY, branch TEXT NOT NULL, base TEXT NOT NULL, tree TEXT NOT NULL,
 index_hash TEXT NOT NULL, state TEXT NOT NULL, tip TEXT NOT NULL DEFAULT '',
 prepared BLOB, result TEXT NOT NULL DEFAULT '',
 UNIQUE(branch, base, tree, index_hash));`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func heartbeat(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `INSERT INTO auto_session VALUES(1,?) ON CONFLICT(id) DO UPDATE SET expires=excluded.expires`, time.Now().Add(5*time.Second).UnixNano())
	return err
}

func active(ctx context.Context, db *sql.DB) bool {
	var expires int64
	err := db.QueryRowContext(ctx, `SELECT expires FROM auto_session WHERE id=1`).Scan(&expires)
	if err != nil {
		return false
	}
	return expires > time.Now().UnixNano()
}

func next(ctx context.Context, db *sql.DB) (snapshot, error) {
	var s snapshot
	err := db.QueryRowContext(ctx, `SELECT id,branch,base,tree,index_hash,state,tip,prepared FROM auto_snapshot WHERE state IN ('queued','validating','publishing') ORDER BY id LIMIT 1`).Scan(&s.id, &s.branch, &s.base, &s.tree, &s.indexHash, &s.state, &s.tip, &s.prepared)
	return s, err
}

func finish(ctx context.Context, db *sql.DB, s snapshot, state, result string) error {
	_, err := db.ExecContext(ctx, `UPDATE auto_snapshot SET state=?,result=? WHERE id=?`, state, result, s.id)
	return err
}
