package daemon

import (
	"context"
	"time"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/repository"
)

// Run owns the spor worktree lock and polls queued requests until cancelled.
// Only one daemon may hold the lock for a worktree at a time.
func Run(ctx context.Context, repoPath string) error {
	lock, err := acquireWorktreeLock(repoPath)
	if err != nil {
		return err
	}
	defer releaseWorktreeLock(lock)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		// Status reports snapshot errors; a missing boundary must not stop polling.
		if lock, err := repository.AcquireWriter(repoPath); err == nil {
			_, _ = auditor.ValidateBranch(ctx, repoPath)
			repository.ReleaseWriter(lock)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// A failed request is already marked done; the daemon
			// keeps polling so the next request still gets served.
			_, _ = ProcessPending(ctx, repoPath)
		}
	}
}
