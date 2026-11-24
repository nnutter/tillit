package auditor

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type statedb struct {
	*sql.DB
}

func openState(ctx context.Context, gitDir string) (statedb, error) {
	dir := filepath.Join(gitDir, "spor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return statedb{}, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "state.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return statedb{}, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return statedb{}, err
	}
	return statedb{DB: db}, nil
}
