package daemon_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
