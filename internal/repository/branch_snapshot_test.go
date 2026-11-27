package repository_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/gittest"

	"github.com/nnutter/tillit/internal/repository"
)

func TestSnapshotBranchBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     string
		upstream string
		wantBase bool
	}{
		{"different upstream", "current", "main", false},
		{"same upstream branch name", "current", "feature", true},
		{"other push mode", "simple", "main", true},
		{"unset push mode", "", "main", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, base := snapshotRepo(t)
			gitSnapshot(t, dir, "update-ref", "refs/remotes/origin/trunk", base)
			gitSnapshot(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
			gitSnapshot(t, dir, "commit", "--allow-empty", "-m", "feature")
			tip := gitSnapshot(t, dir, "rev-parse", "HEAD")
			gitSnapshot(t, dir, "update-ref", "refs/remotes/origin/"+tc.upstream, tip)
			gitSnapshot(t, dir, "config", "branch.feature.remote", "origin")
			gitSnapshot(t, dir, "config", "branch.feature.merge", "refs/heads/"+tc.upstream)
			if tc.mode != "" {
				gitSnapshot(t, dir, "config", "push.default", tc.mode)
			}
			snapshot, err := repository.SnapshotBranch(t.Context(), dir)
			require.NoError(t, err)
			assert.Equal(t, "refs/heads/feature", snapshot.Ref)
			assert.Equal(t, tip, snapshot.Tip)
			if tc.wantBase {
				assert.Equal(t, base, snapshot.Boundary)
				assert.Equal(t, []string{tip}, snapshot.Commits)
			} else {
				assert.Equal(t, tip, snapshot.Boundary)
				assert.Empty(t, snapshot.Commits)
			}
		})
	}
}

func TestSnapshotBranchMergedHistoryAndLinkedWorktree(t *testing.T) {
	dir, base := snapshotRepo(t)
	gitSnapshot(t, dir, "update-ref", "refs/remotes/origin/main", base)
	gitSnapshot(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	gitSnapshot(t, dir, "commit", "--allow-empty", "-m", "first")
	first := gitSnapshot(t, dir, "rev-parse", "HEAD")
	gitSnapshot(t, dir, "update-ref", "refs/agents/test/side", base)
	gitSnapshot(t, dir, "checkout", "--detach", "refs/agents/test/side")
	gitSnapshot(t, dir, "commit", "--allow-empty", "-m", "side")
	side := gitSnapshot(t, dir, "rev-parse", "HEAD")
	gitSnapshot(t, dir, "checkout", "feature")
	gitSnapshot(t, dir, "merge", "--no-ff", "-m", "merge", side)
	merge := gitSnapshot(t, dir, "rev-parse", "HEAD")
	linked := filepath.Join(t.TempDir(), "linked")
	gitSnapshot(t, dir, "worktree", "add", "--detach", linked, merge)
	gitSnapshot(t, dir, "checkout", "--detach", base)
	gitSnapshot(t, linked, "checkout", "feature")
	subdir := filepath.Join(linked, "subdir")
	require.NoError(t, os.Mkdir(subdir, 0o755))
	snapshot, err := repository.SnapshotBranch(t.Context(), subdir)
	require.NoError(t, err)
	assert.Equal(t, base, snapshot.Boundary)
	assert.Equal(t, merge, snapshot.Tip)
	require.Len(t, snapshot.Commits, 3)
	assert.ElementsMatch(t, []string{first, side}, snapshot.Commits[:2])
	assert.Equal(t, merge, snapshot.Commits[2])
	gitSnapshot(t, linked, "commit", "--allow-empty", "-m", "later")
	assert.Equal(t, merge, snapshot.Tip)
	assert.Len(t, snapshot.Commits, 3)
}

func TestSnapshotBranchRequiresBoundaryAndBranch(t *testing.T) {
	dir, base := snapshotRepo(t)
	_, err := repository.SnapshotBranch(t.Context(), dir)
	require.ErrorContains(t, err, "origin/HEAD")
	gitSnapshot(t, dir, "checkout", "--detach", base)
	_, err = repository.SnapshotBranch(t.Context(), dir)
	require.ErrorContains(t, err, "check out a branch")
}

func snapshotRepo(t *testing.T) (string, string) {
	t.Helper()
	gittest.WithSetenvEnviron(t)
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "missing"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	gitSnapshot(t, dir, "init", "-b", "feature")
	gitSnapshot(t, dir, "config", "user.name", "Test")
	gitSnapshot(t, dir, "config", "user.email", "test@example.com")
	gitSnapshot(t, dir, "remote", "add", "origin", "https://example.invalid/repo")
	gitSnapshot(t, dir, "commit", "--allow-empty", "-m", "base")
	return dir, gitSnapshot(t, dir, "rev-parse", "HEAD")
}

func gitSnapshot(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	return strings.TrimSpace(string(out))
}
