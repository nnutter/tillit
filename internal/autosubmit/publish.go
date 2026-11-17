package autosubmit

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

func publish(ctx context.Context, db *sql.DB, root, dir string, s snapshot) error {
	prepared, err := candidateIndex(ctx, root, dir, s.tip)
	if err != nil {
		return err
	}
	s.prepared = prepared
	// Persist intent before changing either the branch or real index.
	_, err = db.ExecContext(ctx, `UPDATE auto_snapshot SET state='publishing',tip=?,prepared=? WHERE id=?`, s.tip, prepared, s.id)
	if err != nil {
		return err
	}
	lockPath := filepath.Join(dir, "index.lock")
	if err := writeIndexLock(lockPath, prepared); err != nil {
		return deferSnapshot(ctx, db, s, "Index locked; publication deferred")
	}
	if err := unchanged(ctx, root, dir, s); err != nil {
		_ = os.Remove(lockPath)
		return deferSnapshot(ctx, db, s, err.Error())
	}
	if !active(ctx, db) {
		_ = os.Remove(lockPath)
		return finish(ctx, db, s, "queued", "Watcher stopped")
	}
	if _, err := git(ctx, root, "", "update-ref", "-m", "spor auto-submit", s.branch, s.tip, s.base); err != nil {
		// A cancelled subprocess may have updated the ref before reporting an
		// error. Retain the journal and lock until recovery checks the ref.
		return err
	}
	// From this point leave the journal and lock intact on error. Restart recovery
	// reconciles only the recorded old index, never a newly staged user index.
	if err := os.Rename(lockPath, filepath.Join(dir, "index")); err != nil {
		return err
	}
	slog.InfoContext(ctx, "Published validated refactors", "submission", s.id, "commit", s.tip, "branch", s.branch)
	return finish(context.WithoutCancel(ctx), db, s, "published", s.tip)
}

func candidateIndex(ctx context.Context, root, dir, tip string) ([]byte, error) {
	file, err := os.CreateTemp(filepath.Join(dir, "spor"), "publication-index-")
	if err != nil {
		return nil, err
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	defer func() { _ = os.Remove(name); _ = os.Remove(name + ".lock") }()
	if _, err := git(ctx, root, name, "read-tree", tip); err != nil {
		return nil, err
	}
	return os.ReadFile(name)
}

func writeIndexLock(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(data); err != nil {
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func recoverPublication(ctx context.Context, db *sql.DB, root, dir string, s snapshot) error {
	branch, err := git(ctx, root, "", "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return err
	}
	if branch != s.branch {
		return fmt.Errorf("interrupted auto-publication %s: restore branch %s before recovery", s.id, s.branch)
	}
	tip, err := git(ctx, root, "", "rev-parse", s.branch)
	if err != nil {
		return err
	}
	index, err := os.ReadFile(filepath.Join(dir, "index"))
	if err != nil {
		return err
	}
	if tip == s.base {
		return rollbackPublication(ctx, db, dir, s, index)
	}
	if tip != s.tip {
		return fmt.Errorf("interrupted auto-publication %s: branch changed; manual recovery required", s.id)
	}
	return recoverIndex(ctx, db, dir, s, index)
}

func recoverIndex(ctx context.Context, db *sql.DB, dir string, s snapshot, index []byte) error {
	if bytes.Equal(index, s.prepared) {
		return finish(ctx, db, s, "published", s.tip)
	}
	if digest(index) != s.indexHash {
		return fmt.Errorf("interrupted auto-publication %s: index changed; refusing to overwrite", s.id)
	}
	if err := restoreIndexLock(dir, s.prepared); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(dir, "index.lock"), filepath.Join(dir, "index")); err != nil {
		return err
	}
	slog.InfoContext(ctx, "Recovered auto-publication", "submission", s.id, "commit", s.tip)
	return finish(ctx, db, s, "published", s.tip)
}

func restoreIndexLock(dir string, data []byte) error {
	path := filepath.Join(dir, "index.lock")
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return writeIndexLock(path, data)
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(existing, data) {
		return fmt.Errorf("index.lock differs from auto-publication journal; refusing to remove it")
	}
	return nil
}

func rollbackPublication(ctx context.Context, db *sql.DB, dir string, s snapshot, index []byte) error {
	if digest(index) != s.indexHash {
		return fmt.Errorf("interrupted auto-publication: index changed; manual recovery required")
	}
	path := filepath.Join(dir, "index.lock")
	existing, err := os.ReadFile(path)
	if err == nil {
		if !bytes.Equal(existing, s.prepared) {
			return fmt.Errorf("index.lock differs from auto-publication journal")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return finish(ctx, db, s, "queued", "Recovered unpublished candidate")
}
