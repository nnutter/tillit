package daemon_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/daemon"
	"github.com/nnutter/spor/internal/repository"
)

func branchGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	return strings.TrimSpace(string(out))
}

func branchRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := newRepo(t)
	branchGit(t, dir, "reset", "--hard", "HEAD")
	base := branchGit(t, dir, "rev-parse", "HEAD")
	branchGit(t, dir, "update-ref", "refs/remotes/origin/main", base)
	branchGit(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	log := filepath.Join(t.TempDir(), "calls")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".spor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".spor", "config.json"), []byte(`{"auditor":{"validate":{"command":["sh","-c","echo run >> `+log+`"]}}}`), 0o644))
	branchGit(t, dir, "add", ".spor/config.json")
	branchGit(t, dir, "commit", "-m", "configure validation")
	return dir, log
}

func TestRunValidatesBranchAtStartupAndOnPoll(t *testing.T) {
	dir, log := branchRepo(t)
	lock, err := repository.AcquireWriter(dir)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx, dir) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	// The daemon cannot validate while another writer owns the checkout.
	time.Sleep(300 * time.Millisecond)
	_, err = os.Stat(log)
	require.ErrorIs(t, err, os.ErrNotExist)
	repository.ReleaseWriter(lock)
	require.Eventually(t, func() bool {
		data, _ := os.ReadFile(log)
		return strings.Count(string(data), "run\n") == 1
	}, 10*time.Second, 50*time.Millisecond)
	branchGit(t, dir, "commit", "--allow-empty", "-m", "another change")
	require.Eventually(t, func() bool {
		data, _ := os.ReadFile(log)
		return strings.Count(string(data), "run\n") == 2
	}, 10*time.Second, 50*time.Millisecond)
	time.Sleep(500 * time.Millisecond)
	data, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(string(data), "run\n"))
}

func TestRunContinuesAfterMissingBoundary(t *testing.T) {
	dir, log := branchRepo(t)
	base := branchGit(t, dir, "rev-parse", "HEAD^")
	branchGit(t, dir, "update-ref", "-d", "refs/remotes/origin/main")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx, dir) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	require.NoError(t, daemon.EnqueueValidate(t.Context(), dir, ""))
	require.Eventually(t, func() bool {
		failure, _ := daemon.WorkError(t.Context(), dir)
		return strings.Contains(failure, "fork-point")
	}, 10*time.Second, 50*time.Millisecond)
	branchGit(t, dir, "update-ref", "refs/remotes/origin/main", base)
	require.Eventually(t, func() bool {
		data, _ := os.ReadFile(log)
		return strings.Contains(string(data), "run\n")
	}, 10*time.Second, 50*time.Millisecond)
}

func TestValidateRequestChecksBranchAndReportsMissingBoundary(t *testing.T) {
	dir, log := branchRepo(t)
	require.NoError(t, daemon.EnqueueValidate(t.Context(), dir, ""))
	processed, err := daemon.ProcessPending(t.Context(), dir)
	require.True(t, processed)
	require.NoError(t, err)
	data, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(data), "run\n"))
	report, err := auditor.ReportBranch(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, "passed", report.Outcomes[0].State)
	branchGit(t, dir, "update-ref", "-d", "refs/remotes/origin/main")
	require.NoError(t, daemon.EnqueueValidate(t.Context(), dir, ""))
	processed, err = daemon.ProcessPending(t.Context(), dir)
	require.True(t, processed)
	require.ErrorContains(t, err, "fork-point")
	workErr, err := daemon.WorkError(t.Context(), dir)
	require.NoError(t, err)
	require.Contains(t, workErr, "fork-point")
}
