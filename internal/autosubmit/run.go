package autosubmit

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nnutter/spor/internal/repository"
)

// Run watches tracked working changes until cancellation. Only one watcher may
// run per worktree. Its heartbeat authorizes publication only while it is alive.
func Run(ctx context.Context, start string) error {
	return RunReady(ctx, start, nil)
}

// RunReady calls ready after the watcher lock and first heartbeat are acquired.
func RunReady(ctx context.Context, start string, ready func()) error {
	root, dir, err := location(start)
	if err != nil {
		return err
	}
	db, err := openState(ctx, dir)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	lock, err := watcherLock(dir)
	if err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }()
	defer func() { _, _ = db.ExecContext(context.WithoutCancel(ctx), `DELETE FROM auto_session`) }()
	slog.InfoContext(ctx, "Auto-submit started", "repo", root)
	defer slog.Info("Auto-submit stopped", "repo", root)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var previous, lastMessage string
	for {
		if err := heartbeat(ctx, db); err != nil {
			return err
		}
		if ready != nil {
			ready()
			ready = nil
		}
		message, fingerprint := watchPoll(ctx, root, dir, previous)
		previous = fingerprint
		if message != lastMessage {
			slog.InfoContext(ctx, "Auto-submit state", "detail", message)
			lastMessage = message
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func watcherLock(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "spor", "auto-submit.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, errors.New("spor auto-submit is already running")
	}
	return f, nil
}

func watchPoll(ctx context.Context, root, dir, previous string) (string, string) {
	lock, err := repository.AcquireWriter(root)
	if err != nil {
		return err.Error(), previous
	}
	defer repository.ReleaseWriter(lock)
	branch, base, err := safeState(ctx, root, dir)
	if err != nil {
		return err.Error(), ""
	}
	tree, err := workingTree(ctx, root, dir, base)
	if err != nil {
		return err.Error(), ""
	}
	fingerprint := branch + base + tree
	if fingerprint != previous {
		return "Working changes detected; waiting for a stable snapshot", fingerprint
	}
	message, err := Capture(ctx, root)
	if err != nil {
		return err.Error(), fingerprint
	}
	return message, fingerprint
}
