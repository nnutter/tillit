package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/repository"
	"github.com/nnutter/spor/internal/testutil"
)

func TestDetachedDaemonLifecycle(t *testing.T) {
	testutil.IsolateGit(t)
	bin := filepath.Join(t.TempDir(), "spor")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".")
	buildOut, err := build.CombinedOutput()
	require.NoError(t, err, "%s", buildOut)
	home := t.TempDir()
	env := cleanTestEnv(home)
	repo := newLifecycleRepo(t, env)
	other := newLifecycleRepo(t, env)
	call := func(dir string, args ...string) (string, error) {
		cmd := exec.CommandContext(t.Context(), bin, append([]string{"--repo", dir}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	t.Cleanup(func() {
		for _, dir := range []string{repo, other} {
			cmd := exec.Command(bin, "--repo", dir, "stop", "--kill")
			cmd.Env = env
			_ = cmd.Run()
		}
	})
	out, err := call(repo, "start", "--auto")
	require.NoError(t, err, "%s", out)
	logPath := filepath.Join(repo, ".git", "spor", "daemon.log")
	require.Contains(t, out, logPath)
	info, err := os.Stat(logPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.Eventually(t, func() bool {
		data, _ := os.ReadFile(logPath)
		return strings.Contains(string(data), "validation-output")
	}, 10*time.Second, 20*time.Millisecond)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(repo, ".git", "spor", "state.db"))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM auto_session WHERE expires > ?", time.Now().UnixNano()).Scan(&count))
	require.Equal(t, 1, count)
	out, err = call(repo, "start")
	require.Error(t, err)
	require.Contains(t, out, "already running")
	out, err = call(other, "start")
	require.NoError(t, err, "%s", out)
	out, err = call(other, "stop")
	require.NoError(t, err, "%s", out)
	out, err = call(other, "start")
	require.NoError(t, err, "%s", out)
	out, err = call(other, "stop")
	require.NoError(t, err, "%s", out)

	gitDir := filepath.Join(repo, ".git")
	checkout, err := repository.CheckoutDir(gitDir)
	require.NoError(t, err)
	pidBytes, err := os.ReadFile(checkout + ".lock")
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	require.NoError(t, err)
	require.NoError(t, syscall.Kill(pid, syscall.SIGSTOP))
	out, err = call(repo, "stop")
	require.Error(t, err)
	require.Contains(t, out, "--kill")
	out, err = call(repo, "stop", "--kill")
	require.NoError(t, err, "%s", out)
	out, err = call(repo, "start")
	require.NoError(t, err, "%s", out)
	out, err = call(repo, "stop")
	require.NoError(t, err, "%s", out)
}

func TestStopKillStopsValidationDescendants(t *testing.T) {
	testutil.IsolateGit(t)
	bin := filepath.Join(t.TempDir(), "spor")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".")
	buildOut, err := build.CombinedOutput()
	require.NoError(t, err, "%s", buildOut)
	home := t.TempDir()
	env := cleanTestEnv(home)
	repo := newLifecycleRepo(t, env)
	marker := filepath.Join(repo, "descendant.pid")
	script := fmt.Sprintf("echo validation-output; echo $$ > %s; exec sleep 60", marker)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".spor", "config.json"), []byte(fmt.Sprintf(`{"auditor":{"validate":{"command":[%q,"-c",%q]}}}`, "/bin/sh", script)), 0o644))
	cmd := exec.CommandContext(t.Context(), "git", "commit", "-am", "sleep validation")
	cmd.Dir, cmd.Env = repo, env
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	t.Cleanup(func() {
		stop := exec.Command(bin, "--repo", repo, "stop", "--kill")
		stop.Env = env
		_ = stop.Run()
	})
	start := exec.CommandContext(t.Context(), bin, "--repo", repo, "start")
	start.Env = env
	out, err = start.CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Eventually(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)
	pidText, err := os.ReadFile(marker)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidText)))
	require.NoError(t, err)
	stop := exec.CommandContext(t.Context(), bin, "--repo", repo, "stop", "--kill")
	stop.Env = env
	out, err = stop.CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Eventually(t, func() bool {
		err := syscall.Kill(pid, 0)
		return errors.Is(err, syscall.ESRCH)
	}, 5*time.Second, 20*time.Millisecond)
}

func cleanTestEnv(home string) []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "HOME" {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "HOME="+home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(home, "missing"))
}

func newLifecycleRepo(t *testing.T, env []string) string {
	t.Helper()
	base, err := filepath.Abs("../../.tmp")
	require.NoError(t, err)
	dir, err := os.MkdirTemp(base, "d-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	git("init", "-b", "feature")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.com")
	git("config", "commit.gpgsign", "false")
	git("commit", "--allow-empty", "-m", "base")
	baseCommit := git("rev-parse", "HEAD")
	git("update-ref", "refs/remotes/origin/main", baseCommit)
	git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".spor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".spor", "config.json"), []byte(fmt.Sprintf(`{"auditor":{"validate":{"command":[%q,"-c","echo validation-output; exit 0"]}}}`, "/bin/sh")), 0o644))
	git("add", ".spor/config.json")
	git("commit", "-m", "configure")
	return dir
}
