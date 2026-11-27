package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/daemon"
	"github.com/nnutter/tillit/internal/repository"
)

func TestRunRejectsSecondDaemonWhileFirstHoldsLock(t *testing.T) {
	dir := newRepo(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- daemon.Run(ctx, dir)
	}()
	<-started
	// Give the first daemon a moment to acquire its worktree lock.
	require.Eventually(t, func() bool {
		return lockExists(t, dir)
	}, time.Second, 10*time.Millisecond)

	// A second call for the same worktree must fail fast.
	secondCtx, secondCancel := context.WithTimeout(context.Background(), time.Second)
	defer secondCancel()
	err := daemon.Run(secondCtx, dir)
	require.ErrorIs(t, err, daemon.ErrAlreadyRunning)

	cancel()
	require.NoError(t, <-done)

	// After the first daemon exits, the lock is released.
	thirdCtx, thirdCancel := context.WithCancel(context.Background())
	defer thirdCancel()
	thirdDone := make(chan error, 1)
	go func() { thirdDone <- daemon.Run(thirdCtx, dir) }()
	require.Eventually(t, func() bool { return lockExists(t, dir) }, time.Second, 10*time.Millisecond)
	thirdCancel()
	require.NoError(t, <-thirdDone)

	// Lock is freed: a fresh daemon can start.
	fourthCtx, fourthCancel := context.WithCancel(context.Background())
	defer fourthCancel()
	fourthDone := make(chan error, 1)
	go func() { fourthDone <- daemon.Run(fourthCtx, dir) }()
	require.Eventually(t, func() bool { return lockExists(t, dir) }, time.Second, 10*time.Millisecond)
	fourthCancel()
	require.NoError(t, <-fourthDone)
}

func TestRunAllowsDifferentWorktrees(t *testing.T) {
	first := newRepo(t)
	second := newRepo(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstDone := make(chan error, 1)
	go func() { firstDone <- daemon.Run(ctx, first) }()
	require.Eventually(t, func() bool { return lockExists(t, first) }, time.Second, 10*time.Millisecond)

	// A different worktree has its own lock and may run concurrently.
	secondCtx, secondCancel := context.WithCancel(context.Background())
	defer secondCancel()
	secondDone := make(chan error, 1)
	go func() { secondDone <- daemon.Run(secondCtx, second) }()
	require.Eventually(t, func() bool { return lockExists(t, second) }, time.Second, 10*time.Millisecond)

	// Both are running; cancelling both drains them.
	cancel()
	secondCancel()
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
}

func lockExists(t *testing.T, repoPath string) bool {
	t.Helper()
	_, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return false
	}
	checkout, err := repository.CheckoutDir(gitDir)
	if err != nil {
		return false
	}
	_, err = os.Stat(checkout + ".lock")
	return err == nil
}

func TestStatusDetectsOutOfSyncDaemon(t *testing.T) {
	dir := newRepo(t)

	before, err := daemon.Status(dir)
	require.NoError(t, err)
	require.False(t, before.Running)
	require.False(t, before.NeedsRestart)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx, dir) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })

	require.Eventually(t, func() bool {
		state, err := daemon.Status(dir)
		if err != nil {
			return false
		}
		return state.Running
	}, time.Second, 10*time.Millisecond)

	state, err := daemon.Status(dir)
	require.NoError(t, err)
	require.True(t, state.Running)
	require.False(t, state.NeedsRestart)

	// Simulate an older daemon binary by overwriting daemon.version.
	versionPath := filepath.Join(dir, ".git", "tillit", "daemon.version")
	require.NoError(t, os.WriteFile(versionPath, []byte("stale-fingerprint\n"), 0o600))
	state, err = daemon.Status(dir)
	require.NoError(t, err)
	require.True(t, state.Running)
	require.True(t, state.NeedsRestart)

	// Missing daemon.version (e.g. daemon started before version tracking) also needs restart.
	require.NoError(t, os.Remove(versionPath))
	state, err = daemon.Status(dir)
	require.NoError(t, err)
	require.True(t, state.Running)
	require.True(t, state.NeedsRestart)
}
