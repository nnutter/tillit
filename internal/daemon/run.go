package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/nnutter/tillit/internal/auditor"
	"github.com/nnutter/tillit/internal/repository"
	"github.com/nnutter/tillit/internal/verifier"
)

// Run owns the tillit worktree lock and polls queued requests until cancelled.
// Only one daemon may hold the lock for a worktree at a time.
func Run(ctx context.Context, repoPath string) error {
	return runReady(ctx, repoPath, nil)
}

func runReady(ctx context.Context, repoPath string, ready func()) error {
	lock, err := acquireWorktreeLock(repoPath)
	if err != nil {
		return err
	}
	defer releaseWorktreeLock(lock)
	slog.InfoContext(ctx, "Daemon started", "repo", repoPath)
	defer slog.Info("Daemon stopped", "repo", repoPath)

	if ready != nil {
		ready()
	}
	return poll(ctx, repoPath)
}

func poll(ctx context.Context, repoPath string) error {
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	for {
		// Status reports snapshot errors; a missing boundary must not stop polling.
		if lock, err := repository.AcquireWriter(repoPath); err == nil {
			_, _ = verifier.VerifyBranch(ctx, repoPath)
			_, _ = auditor.ValidateBranch(ctx, repoPath)
			repository.ReleaseWriter(lock)
		}
		// Leave an idle interval even when validation took longer than a poll.
		timer.Reset(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
	}
}
