package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/autosubmit"
	"github.com/nnutter/spor/internal/repository"
	"github.com/nnutter/spor/internal/verifier"
)

// Run owns the spor worktree lock and polls queued requests until cancelled.
// Only one daemon may hold the lock for a worktree at a time.
func Run(ctx context.Context, repoPath string) error {
	lock, err := acquireWorktreeLock(repoPath)
	if err != nil {
		return err
	}
	defer releaseWorktreeLock(lock)
	slog.InfoContext(ctx, "Daemon started", "repo", repoPath)
	defer slog.Info("Daemon stopped", "repo", repoPath)

	var lastAutoError string
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
		// Otherwise the daemon can continuously reacquire the writer lock and
		// starve the separate auto-submit process.
		timer.Reset(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			// A failed request is already marked done; the daemon
			// keeps polling so the next request still gets served.
			_, _ = ProcessPending(ctx, repoPath)
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
