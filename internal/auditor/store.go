package auditor

import (
	"context"
	"database/sql"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type statedb struct {
	*sql.DB
}

func openState(ctx context.Context, gitDir string) (statedb, error) {
	dir := filepath.Join(gitDir, "tillit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return statedb{}, err
	}
	path := filepath.Join(dir, "state.db")
	if err := privateStateFile(path); err != nil {
		return statedb{}, err
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return statedb{}, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return statedb{}, err
	}
	return statedb{DB: db}, nil
}

// Validation output can contain secrets. Restrict existing Tillit databases
// as well as newly created ones before writing evidence.
func privateStateFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Mode().Perm() == 0o600 {
		return nil
	}
	return file.Chmod(0o600)
}

func ensureBranchValidation(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS branch_validation (
		commit_hash TEXT NOT NULL, input TEXT NOT NULL, state TEXT NOT NULL, error TEXT NOT NULL,
		evidence TEXT NOT NULL DEFAULT '', PRIMARY KEY (commit_hash, input)
	)`)
	if err != nil {
		return err
	}
	exists, err := branchEvidenceColumn(ctx, db)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = db.ExecContext(ctx, `ALTER TABLE branch_validation ADD COLUMN evidence TEXT NOT NULL DEFAULT ''`)
	return err
}

func branchEvidenceColumn(ctx context.Context, db *sql.DB) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pragma_table_info('branch_validation') WHERE name = 'evidence')`).Scan(&exists)
	return exists, err
}

func recordBranchOutcome(ctx context.Context, db *sql.DB, key string, outcome BranchOutcome) error {
	var raw string
	if outcome.Evidence != nil {
		data, err := json.Marshal(outcome.Evidence)
		if err != nil {
			return err
		}
		raw = string(data)
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO branch_validation (commit_hash, input, state, error, evidence) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (commit_hash, input) DO UPDATE SET
			state = excluded.state, error = excluded.error, evidence = excluded.evidence
	`, outcome.Hash, key, outcome.State, outcome.Error, raw)
	return err
}

func readBranchOutcome(ctx context.Context, db *sql.DB, hasEvidence bool, key string, outcome *BranchOutcome) error {
	if !hasEvidence {
		return db.QueryRowContext(ctx, `SELECT state, error FROM branch_validation WHERE commit_hash = ? AND input = ?`, outcome.Hash, key).
			Scan(&outcome.State, &outcome.Error)
	}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT state, error, evidence FROM branch_validation WHERE commit_hash = ? AND input = ?`, outcome.Hash, key).
		Scan(&outcome.State, &outcome.Error, &raw)
	if err != nil {
		return err
	}
	if raw == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), &outcome.Evidence); err != nil {
		return fmt.Errorf("decode validation evidence: %w", err)
	}
	return nil
}

func openExistingState(ctx context.Context, gitDir string) (statedb, bool, error) {
	path := filepath.Join(gitDir, "tillit", "state.db")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return statedb{}, false, nil
	} else if err != nil {
		return statedb{}, false, err
	}
	db, err := openState(ctx, gitDir)
	if err != nil {
		return statedb{}, false, err
	}
	var exists int
	err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='branch_validation'`).Scan(&exists)
	if err != nil {
		_ = db.Close()
		return statedb{}, false, err
	}
	if exists == 0 {
		_ = db.Close()
		return statedb{}, false, nil
	}
	return db, true, nil
}

func deleteFailedBranchOutcome(ctx context.Context, db *sql.DB, hash, key string) (bool, error) {
	res, err := db.ExecContext(ctx,
		`DELETE FROM branch_validation WHERE commit_hash = ? AND input = ? AND state = 'failed'`,
		hash, key,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
