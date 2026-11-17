package autosubmit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/nnutter/spor/internal/repository"
)

// Capture queues an immutable tracked-file snapshot without touching the user's
// index or files. It requires a clean index. The caller must hold the writer lock.
func Capture(ctx context.Context, start string) (string, error) {
	root, dir, err := location(start)
	if err != nil {
		return "", err
	}
	db, err := openState(ctx, dir)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	if _, err := next(ctx, db); err == nil {
		return "Awaiting daemon processing", nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return captureReady(ctx, db, root, dir)
}

func captureReady(ctx context.Context, db *sql.DB, root, dir string) (string, error) {
	branch, base, err := safeState(ctx, root, dir)
	if err != nil {
		return "", err
	}
	before, err := os.ReadFile(filepath.Join(dir, "index"))
	if err != nil {
		return "", err
	}
	tree, err := workingTree(ctx, root, dir, base)
	if err != nil {
		return "", err
	}
	s := snapshot{branch: branch, base: base, tree: tree, indexHash: digest(before)}
	if err := unchanged(ctx, root, dir, s); err != nil {
		return "", err
	}
	baseTree, err := git(ctx, root, "", "rev-parse", base+"^{tree}")
	if err != nil {
		return "", err
	}
	if tree == baseTree {
		return "No tracked working changes", nil
	}
	return enqueueSnapshot(ctx, db, root, dir, s)
}

func enqueueSnapshot(ctx context.Context, db *sql.DB, root, dir string, s snapshot) (string, error) {
	s.id = fmt.Sprint(time.Now().UnixNano())
	ref := snapshotRef(dir, s.id)
	if _, err := git(ctx, root, "", "update-ref", ref, s.tree); err != nil {
		return "", err
	}
	result, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO auto_snapshot(id,branch,base,tree,index_hash,state) VALUES(?,?,?,?,?,'queued')`, s.id, s.branch, s.base, s.tree, s.indexHash)
	if err != nil {
		return "", err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if count == 0 {
		_, err = git(ctx, root, "", "update-ref", "-d", ref)
		return "Snapshot already processed", err
	}
	slog.InfoContext(ctx, "Submitted working snapshot", "submission", s.id, "branch", s.branch, "base", s.base)
	return "Submitted working snapshot", nil
}

func snapshotRef(dir, id string) string { return repository.HeadRef(dir) + "-auto/" + id }

func unchanged(ctx context.Context, root, dir string, s snapshot) error {
	branch, base, err := safeState(ctx, root, dir)
	if err != nil {
		return err
	}
	if branch != s.branch {
		return fmt.Errorf("branch changed")
	}
	if base != s.base {
		return fmt.Errorf("branch tip changed")
	}
	index, err := os.ReadFile(filepath.Join(dir, "index"))
	if err != nil {
		return err
	}
	if digest(index) != s.indexHash {
		return fmt.Errorf("index changed")
	}
	tree, err := workingTree(ctx, root, dir, base)
	if err != nil {
		return err
	}
	if tree != s.tree {
		return fmt.Errorf("working changes changed")
	}
	return nil
}
