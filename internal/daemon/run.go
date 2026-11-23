package daemon

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/nnutter/spor/internal/autosubmit"
)

// Run owns the spor worktree lock and processes automatic snapshots until cancelled.
// Only one daemon may hold the lock for a worktree at a time.
func Run(ctx context.Context, repoPath string) error {
	return run(ctx, repoPath, false, nil)
}

// RunReady runs the watcher and calls ready once it has acquired its lock.
func RunReady(ctx context.Context, repoPath string, ready func()) error {
	return run(ctx, repoPath, true, ready)
}

func run(ctx context.Context, repoPath string, auto bool, ready func()) error {
	lock, err := acquireWorktreeLock(repoPath)
	if err != nil {
		return err
	}
	defer releaseWorktreeLock(lock)
	slog.InfoContext(ctx, "Daemon started", "repo", repoPath)
	defer slog.Info("Daemon stopped", "repo", repoPath)

	if auto {
		return pollWithWatcher(ctx, repoPath, ready)
	}
	if ready != nil {
		ready()
	}
	return pollAutomatic(ctx, repoPath)
}

func pollWithWatcher(ctx context.Context, repoPath string, ready func()) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		err := autosubmit.RunReady(ctx, repoPath, ready)
		cancel()
		done <- err
	}()
	err := pollAutomatic(ctx, repoPath)
	cancel()
	watcherErr := <-done
	if errors.Is(watcherErr, context.Canceled) {
		watcherErr = nil
	}
	return errors.Join(err, watcherErr)
}

func pollAutomatic(ctx context.Context, repoPath string) error {
	var lastAutoError string
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			lastAutoError = processAuto(ctx, repoPath, lastAutoError)
		}
	}
}

func processAuto(ctx context.Context, start, previous string) string {
	err := autosubmit.Process(ctx, start)
	if err == nil {
		return ""
	}
	message := err.Error()
	if message != previous {
		slog.ErrorContext(ctx, "Auto-submission processing stopped", "error", err)
	}
	return message
}
