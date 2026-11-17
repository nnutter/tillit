package autosubmit

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/repository"
	"github.com/nnutter/spor/internal/smith"
)

// Process handles one auto-submission in the daemon. It is independent of the
// manual add/rebase queue and serializes access to the daemon checkout.
func Process(ctx context.Context, start string) error {
	lock, err := repository.AcquireWriter(start)
	if errors.Is(err, repository.ErrWriterBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	defer repository.ReleaseWriter(lock)
	root, dir, err := location(start)
	if err != nil {
		return err
	}
	db, err := openState(ctx, dir)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	s, err := next(ctx, db)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// Complete or roll back an interrupted publication even without a watcher.
	if s.state == "publishing" {
		return recoverPublication(ctx, db, root, dir, s)
	}
	if !active(ctx, db) {
		return nil
	}
	return processSnapshot(ctx, db, root, dir, s)
}

func processSnapshot(ctx context.Context, db *sql.DB, root, dir string, s snapshot) error {
	if err := unchanged(ctx, root, dir, s); err != nil {
		return deferSnapshot(ctx, db, s, err.Error())
	}
	if err := finish(ctx, db, s, "validating", ""); err != nil {
		return err
	}
	commits, err := smith.ExtractSnapshot(ctx, root, s.base, s.tree)
	if err != nil {
		return failSnapshot(ctx, db, s, err)
	}
	if len(commits) == 0 {
		slog.InfoContext(ctx, "No refactors detected", "submission", s.id)
		return finish(ctx, db, s, "none", "No refactors detected")
	}
	s.tip = commits[len(commits)-1].Hash
	if _, err := git(ctx, root, "", "update-ref", snapshotRef(dir, s.id)+"-candidate", s.tip); err != nil {
		return err
	}
	slog.InfoContext(ctx, "Detected refactors", "submission", s.id, "commits", len(commits))
	hashes := make([]string, len(commits))
	for i, commit := range commits {
		hashes[i] = commit.Hash
	}
	return validateAndPublish(ctx, db, root, dir, s, hashes)
}

func validateAndPublish(ctx context.Context, db *sql.DB, root, dir string, s snapshot, hashes []string) error {
	passed, err := auditor.ValidateSnapshot(ctx, root, hashes)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return failSnapshot(ctx, db, s, err)
	}
	if !passed {
		return deferSnapshot(ctx, db, s, "Validation failed; candidate not published")
	}
	if !active(ctx, db) {
		return finish(ctx, db, s, "queued", "Watcher inactive; publication deferred")
	}
	if err := publish(ctx, db, root, dir, s); err != nil {
		return err
	}
	return nil
}

func failSnapshot(ctx context.Context, db *sql.DB, s snapshot, err error) error {
	slog.ErrorContext(ctx, "Auto-submission failed", "submission", s.id, "error", err)
	return finish(ctx, db, s, "failed", err.Error())
}

func deferSnapshot(ctx context.Context, db *sql.DB, s snapshot, reason string) error {
	slog.InfoContext(ctx, "Auto-publication skipped", "submission", s.id, "reason", reason)
	return finish(ctx, db, s, "skipped", reason)
}
