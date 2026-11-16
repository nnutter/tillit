package auditor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/nnutter/spor/internal/config"
)

type statedb struct {
	*sql.DB
}

func openState(ctx context.Context, gitDir string) (statedb, error) {
	dir := filepath.Join(gitDir, "spor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return statedb{}, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "state.db"))
	if err != nil {
		return statedb{}, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return statedb{}, err
	}
	if err := createValidationTable(ctx, db); err != nil {
		_ = db.Close()
		return statedb{}, err
	}
	return statedb{DB: db}, nil
}

func createValidationTable(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var legacy, current int
	if err := tx.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'verification'),
		EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'validation')`).Scan(&legacy, &current); err != nil {
		return err
	}
	if legacy != 0 {
		if current != 0 {
			return errors.New("both legacy verification and validation tables exist; resolve the conflicting validation records")
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE verification RENAME TO validation`); err != nil {
			return fmt.Errorf("migrate validation records: %w", err)
		}
	}
	_, err = tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS validation (
			commit_hash TEXT NOT NULL,
			patch_id TEXT NOT NULL,
			change_id TEXT NOT NULL,
			passed INTEGER NOT NULL,
			created TEXT NOT NULL
		)
	`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// An empty patch id or change id is not shared, so it never matches another row.
func alreadyPassed(ctx context.Context, db statedb, mode config.Mode, item identity) (bool, error) {
	switch mode {
	case config.ModeCommit:
		return hasPass(ctx, db, `
			SELECT 1 FROM validation
			WHERE commit_hash = ? AND passed = 1
			LIMIT 1
		`, item.hash)
	case config.ModePatch:
		return hasKeyedPass(ctx, db, item.patchID, `
			SELECT 1 FROM validation
			WHERE patch_id = ? AND passed = 1
			LIMIT 1
		`)
	case config.ModeChange:
		return hasKeyedPass(ctx, db, item.changeID, `
			SELECT 1 FROM validation
			WHERE change_id = ? AND passed = 1
			LIMIT 1
		`)
	default:
		return false, fmt.Errorf("validate mode %q is not commit, patch, or change", mode)
	}
}

func hasKeyedPass(ctx context.Context, db statedb, key, query string) (bool, error) {
	if key == "" {
		return false, nil
	}
	return hasPass(ctx, db, query, key)
}

func hasPass(ctx context.Context, db statedb, query, key string) (bool, error) {
	var one int
	err := db.QueryRowContext(ctx, query, key).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func record(ctx context.Context, db statedb, item identity, passed bool) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO validation (commit_hash, patch_id, change_id, passed, created)
		VALUES (?, ?, ?, ?, ?)
	`, item.hash, item.patchID, item.changeID, passedBit(passed), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("record validation: %w", err)
	}
	return nil
}

func recordedOutcome(ctx context.Context, db statedb, mode config.Mode, item identity) (Outcome, bool, error) {
	passed, err := alreadyPassed(ctx, db, mode, item)
	if err != nil {
		return Outcome{}, false, err
	}
	if passed {
		return Outcome{Hash: item.hash, Passed: true}, true, nil
	}
	row := db.QueryRowContext(ctx, `
		SELECT passed FROM validation
		WHERE commit_hash = ?
		ORDER BY created DESC, rowid DESC
		LIMIT 1
	`, item.hash)
	var flag int
	err = row.Scan(&flag)
	if errors.Is(err, sql.ErrNoRows) {
		return Outcome{}, false, nil
	}
	if err != nil {
		return Outcome{}, false, err
	}
	return Outcome{Hash: item.hash, Passed: flag != 0}, true, nil
}

func passedBit(passed bool) int {
	if passed {
		return 1
	}
	return 0
}
