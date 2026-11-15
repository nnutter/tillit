package smith

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type storedCommit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

type submission struct {
	id      string
	ref     string
	outcome string
	commits []storedCommit
}

func recordSubmission(ctx context.Context, gitDir string, item submission) error {
	db, err := openState(ctx, gitDir)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	body, err := json.Marshal(item.commits)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO submission (id, created, ref, outcome, commits)
		VALUES (?, ?, ?, ?, ?)
	`, item.id, time.Now().UTC().Format(time.RFC3339Nano), item.ref, item.outcome, string(body))
	if err != nil {
		return fmt.Errorf("record submission: %w", err)
	}
	return nil
}

func latestSubmission(ctx context.Context, gitDir string) (submission, bool, error) {
	db, err := openState(ctx, gitDir)
	if err != nil {
		return submission{}, false, err
	}
	defer func() { _ = db.Close() }()
	row := db.QueryRowContext(ctx, `
		SELECT id, ref, outcome, commits
		FROM submission
		ORDER BY created DESC, rowid DESC
		LIMIT 1
	`)
	var item submission
	var body string
	err = row.Scan(&item.id, &item.ref, &item.outcome, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return submission{}, false, nil
	}
	if err != nil {
		return submission{}, false, err
	}
	if err := json.Unmarshal([]byte(body), &item.commits); err != nil {
		return submission{}, false, err
	}
	return item, true, nil
}

func openState(ctx context.Context, gitDir string) (*sql.DB, error) {
	dir := filepath.Join(gitDir, "spor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "state.db"))
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := createSubmissionTable(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func createSubmissionTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS submission (
			id TEXT PRIMARY KEY,
			created TEXT NOT NULL,
			ref TEXT NOT NULL,
			outcome TEXT NOT NULL,
			commits TEXT NOT NULL
		)
	`)
	return err
}
