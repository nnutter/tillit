// Package daemon owns spor work in the background.
package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/repository"
	"github.com/nnutter/spor/internal/smith"

	_ "modernc.org/sqlite"
)

// Kind is the kind of one queued request.
//
// It is sealed: only the kinds declared in this package implement it.
// Switch over concrete types with go-check-sumtype exhaustiveness.
//
//sumtype:decl
type Kind interface {
	sealedKind()
}

type kindAdd struct{}

func (kindAdd) sealedKind() {}

type kindVerify struct{}

func (kindVerify) sealedKind() {}

// Kinds in dispatch order.
var (
	KindAdd    Kind = kindAdd{}
	KindVerify Kind = kindVerify{}
)

type request struct {
	id   string
	kind Kind
	mode string
}

// EnqueueAdd records one add request for the daemon.
func EnqueueAdd(ctx context.Context, repoPath string) error {
	return enqueue(ctx, repoPath, KindAdd, "")
}

// EnqueueVerify records one verify request for the daemon.
func EnqueueVerify(ctx context.Context, repoPath, mode string) error {
	if mode != "" {
		if _, err := config.ParseMode(mode); err != nil {
			return err
		}
	}
	return enqueue(ctx, repoPath, KindVerify, mode)
}

func enqueue(ctx context.Context, repoPath string, kind Kind, mode string) error {
	_, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return err
	}
	db, err := openState(ctx, gitDir)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(ctx, `INSERT INTO request (id, created, kind, mode, done) VALUES (?, ?, ?, ?, 0)`, newID(), time.Now().UTC().Format(time.RFC3339Nano), kindName(kind), mode)
	if err != nil {
		return fmt.Errorf("enqueue request: %w", err)
	}
	return nil
}

// ProcessPending runs the oldest queued request once.
// It reports whether it ran one. A request that errors is still marked done.
func ProcessPending(ctx context.Context, repoPath string) (bool, error) {
	_, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return false, err
	}
	item, ok, err := oldestPending(ctx, gitDir)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	dispatchErr := dispatch(ctx, repoPath, item)
	markErr := markDone(ctx, gitDir, item.id)
	if markErr != nil {
		return true, markErr
	}
	return true, dispatchErr
}

func dispatch(ctx context.Context, repoPath string, item request) error {
	switch item.kind.(type) {
	case kindAdd:
		return dispatchAdd(ctx, repoPath)
	case kindVerify:
		_, err := auditor.Verify(ctx, repoPath, config.Mode(item.mode))
		if errors.Is(err, auditor.ErrCommandNotConfigured) {
			return nil
		}
		return err
	default:
		return fmt.Errorf("unknown request kind")
	}
}

func dispatchAdd(ctx context.Context, repoPath string) error {
	if _, err := smith.Add(ctx, repoPath); err != nil {
		return err
	}
	_, err := auditor.Verify(ctx, repoPath, "")
	if errors.Is(err, auditor.ErrCommandNotConfigured) {
		return nil
	}
	return err
}

func oldestPending(ctx context.Context, gitDir string) (request, bool, error) {
	db, err := openState(ctx, gitDir)
	if err != nil {
		return request{}, false, err
	}
	defer func() { _ = db.Close() }()
	row := db.QueryRowContext(ctx, `SELECT id, kind, mode FROM request WHERE done = 0 ORDER BY created ASC, rowid ASC LIMIT 1`)
	var item request
	var kind, mode string
	err = row.Scan(&item.id, &kind, &mode)
	if errors.Is(err, sql.ErrNoRows) {
		return request{}, false, nil
	}
	if err != nil {
		return request{}, false, err
	}
	item.mode = mode
	item.kind, err = parseKind(kind)
	if err != nil {
		return request{}, false, err
	}
	return item, true, nil
}

func markDone(ctx context.Context, gitDir, id string) error {
	db, err := openState(ctx, gitDir)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(ctx, `UPDATE request SET done = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mark request done: %w", err)
	}
	return nil
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
	if err := createRequestTable(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func createRequestTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS request (
			id TEXT PRIMARY KEY,
			created TEXT NOT NULL,
			kind TEXT NOT NULL,
			mode TEXT NOT NULL,
			done INTEGER NOT NULL
		)
	`)
	return err
}

func newID() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func kindName(kind Kind) string {
	switch kind.(type) {
	case kindAdd:
		return "add"
	case kindVerify:
		return "verify"
	default:
		return ""
	}
}

func parseKind(value string) (Kind, error) {
	switch value {
	case "add":
		return KindAdd, nil
	case "verify":
		return KindVerify, nil
	default:
		return nil, fmt.Errorf("unknown request kind %q", value)
	}
}
