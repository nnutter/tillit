// Package daemon owns spor work in the background.
package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/mender"
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

type kindValidate struct{}

func (kindValidate) sealedKind() {}

type kindRebase struct{}

func (kindRebase) sealedKind() {}

// Kinds in dispatch order.
var (
	KindAdd      Kind = kindAdd{}
	KindValidate Kind = kindValidate{}
	KindRebase   Kind = kindRebase{}
)

type request struct {
	id     string
	kind   Kind
	mode   string
	branch string
}

// EnqueueAdd records one add request for the daemon.
func EnqueueAdd(ctx context.Context, repoPath string) error {
	return enqueue(ctx, repoPath, KindAdd, "")
}

// EnqueueValidate records one validate request for the daemon.
func EnqueueValidate(ctx context.Context, repoPath, mode string) error {
	if mode != "" {
		if _, err := config.ParseMode(mode); err != nil {
			return err
		}
	}
	return enqueue(ctx, repoPath, KindValidate, mode)
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

// EnqueueRebase records the current branch as the target of integration.
func EnqueueRebase(ctx context.Context, repoPath string) error {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return err
	}
	defer func() { _ = repo.Close() }()
	head, err := repo.Storer.Reference("HEAD")
	if err != nil {
		return err
	}
	branch := head.Target().String()
	if branch == "" {
		return fmt.Errorf("rebase requires an attached branch")
	}
	db, err := openState(ctx, gitDir)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(ctx, `INSERT INTO request (id, created, kind, mode, done, branch) VALUES (?, ?, ?, '', 0, ?)`, newID(), time.Now().UTC().Format(time.RFC3339Nano), "rebase", branch)
	if err != nil {
		return fmt.Errorf("enqueue rebase: %w", err)
	}
	return nil
}

// ProcessPending runs the oldest queued request once.
// It reports whether it ran one. A request that errors is still marked done.
func ProcessPending(ctx context.Context, repoPath string) (bool, error) {
	lock, err := repository.AcquireWriter(repoPath)
	if errors.Is(err, repository.ErrWriterBusy) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer repository.ReleaseWriter(lock)
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
	var result string
	var dispatchErr error
	if _, ok := item.kind.(kindRebase); ok {
		result, dispatchErr = mender.Rebase(ctx, repoPath, item.branch)
	} else {
		dispatchErr = dispatch(ctx, repoPath, item)
	}
	if dispatchErr != nil {
		result = "Error: " + dispatchErr.Error()
	}
	markErr := markDone(ctx, gitDir, item.id, result)
	if markErr != nil {
		return true, markErr
	}
	return true, dispatchErr
}

func dispatch(ctx context.Context, repoPath string, item request) error {
	switch item.kind.(type) {
	case kindAdd:
		return dispatchAdd(ctx, repoPath)
	case kindRebase:
		_, err := mender.Rebase(ctx, repoPath, item.branch)
		return err
	case kindValidate:
		_, branchErr := auditor.ValidateBranch(ctx, repoPath)
		_, err := auditor.Validate(ctx, repoPath, config.Mode(item.mode))
		if errors.Is(err, auditor.ErrCommandNotConfigured) {
			err = nil
		}
		return errors.Join(branchErr, err)
	default:
		return fmt.Errorf("unknown request kind")
	}
}

func dispatchAdd(ctx context.Context, repoPath string) error {
	if _, err := smith.Add(ctx, repoPath); err != nil {
		return err
	}
	_, err := auditor.Validate(ctx, repoPath, "")
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
	row := db.QueryRowContext(ctx, `SELECT id, kind, mode, branch FROM request WHERE done = 0 ORDER BY created ASC, rowid ASC LIMIT 1`)
	var item request
	var kind, mode string
	err = row.Scan(&item.id, &kind, &mode, &item.branch)
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

func markDone(ctx context.Context, gitDir, id, result string) error {
	db, err := openState(ctx, gitDir)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(ctx, `UPDATE request SET done = 1, result = ? WHERE id = ?`, result, id)
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
	if _, err := db.ExecContext(ctx, `UPDATE request SET kind = 'validate' WHERE kind = 'verify'`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate validation requests: %w", err)
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
	if err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(request)`)
	if err != nil {
		return err
	}
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, name := range []string{"branch", "result"} {
		if !columns[name] {
			if _, err := db.ExecContext(ctx, `ALTER TABLE request ADD COLUMN `+name+` TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
	}
	return nil
}

// WorkError reports the latest completed add or validate failure, if any.
func WorkError(ctx context.Context, repoPath string) (string, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return "", err
	}
	_ = repo.Close()
	db, err := openState(ctx, gitDir)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	var failures []string
	for _, kind := range []string{"add", "validate"} {
		failure, err := latestWorkError(ctx, db, kind)
		if err != nil {
			return "", err
		}
		if failure != "" {
			failures = append(failures, kind+": "+failure)
		}
	}
	return strings.Join(failures, "\n"), nil
}

func latestWorkError(ctx context.Context, db *sql.DB, kind string) (string, error) {
	var result string
	err := db.QueryRowContext(ctx, `SELECT result FROM request WHERE kind = ? AND done = 1 ORDER BY created DESC, rowid DESC LIMIT 1`, kind).Scan(&result)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(result, "Error: ") {
		return "", nil
	}
	return result, nil
}

// RebaseOutcome reports the most recently completed rebase request.
func RebaseOutcome(ctx context.Context, repoPath string) (string, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return "", err
	}
	_ = repo.Close()
	db, err := openState(ctx, gitDir)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	var result string
	err = db.QueryRowContext(ctx, `SELECT result FROM request WHERE kind = 'rebase' AND done = 1 ORDER BY created DESC, rowid DESC LIMIT 1`).Scan(&result)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return result, err
}

func newID() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func kindName(kind Kind) string {
	switch kind.(type) {
	case kindAdd:
		return "add"
	case kindValidate:
		return "validate"
	case kindRebase:
		return "rebase"
	default:
		return ""
	}
}

func parseKind(value string) (Kind, error) {
	switch value {
	case "add":
		return KindAdd, nil
	case "validate":
		return KindValidate, nil
	case "rebase":
		return KindRebase, nil
	default:
		return nil, fmt.Errorf("unknown request kind %q", value)
	}
}
