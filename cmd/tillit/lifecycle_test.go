package main

import (
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

	"github.com/nnutter/gittest"

	"github.com/nnutter/tillit/internal/repository"
)

func TestDetachedDaemonLifecycle(t *testing.T) {
	gittest.WithSetenvEnviron(t)
	bin := filepath.Join(t.TempDir(), "tillit")
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
	for _, legacy := range []string{"spor", "timber-review"} {
		dir := filepath.Join(repo, ".git", legacy)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.db"), []byte(legacy), 0o644))
	}
	out, err := call(repo, "start")
	require.NoError(t, err, "%s", out)
	logPath := filepath.Join(repo, ".git", "tillit", "daemon.log")
	require.Contains(t, out, logPath)
	info, err := os.Stat(logPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.Eventually(t, func() bool {
		data, _ := os.ReadFile(logPath)
		return strings.Contains(string(data), "validation-output")
	}, 10*time.Second, 20*time.Millisecond)
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

	t.Run("foreground stop", func(t *testing.T) { checkForeground(t, bin, repo, env, false) })
	t.Run("foreground interrupt", func(t *testing.T) { checkForeground(t, bin, repo, env, true) })
	for _, legacy := range []string{"spor", "timber-review"} {
		data, err := os.ReadFile(filepath.Join(repo, ".git", legacy, "state.db"))
		require.NoError(t, err)
		require.Equal(t, legacy, string(data))
	}
}

func checkForeground(t *testing.T, bin, repo string, env []string, interrupt bool) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), bin, "--repo", repo, "start", "--foreground")
	cmd.Env = env
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	result := make(chan error, 1)
	go func() { result <- cmd.Wait() }()
	status := func() string {
		check := exec.CommandContext(t.Context(), bin, "--repo", repo, "status")
		check.Env = env
		out, _ := check.CombinedOutput()
		return string(out)
	}
	require.Eventually(t, func() bool {
		return strings.Contains(status(), "daemon: running")
	}, 10*time.Second, 50*time.Millisecond)
	if interrupt {
		require.NoError(t, cmd.Process.Signal(os.Interrupt))
	} else {
		stop := exec.CommandContext(t.Context(), bin, "--repo", repo, "stop")
		stop.Env = env
		out, err := stop.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "foreground daemon did not exit")
	}
	require.Contains(t, status(), "daemon: stopped")
}

func TestStopKillStopsValidationDescendants(t *testing.T) {
	gittest.WithSetenvEnviron(t)
	bin := filepath.Join(t.TempDir(), "tillit")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".")
	buildOut, err := build.CombinedOutput()
	require.NoError(t, err, "%s", buildOut)
	home := t.TempDir()
	env := cleanTestEnv(home)
	repo := newLifecycleRepo(t, env)
	marker := filepath.Join(repo, "descendant.pid")
	script := fmt.Sprintf("echo validation-output; echo $$ > %s; exec sleep 60", marker)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".tillit", "config.json"), []byte(fmt.Sprintf(`{"auditor":{"validate":{"command":[%q,"-c",%q]}}}`, "/bin/sh", script)), 0o644))
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
	git("config", "branch.feature.remote", ".")
	git("config", "branch.feature.merge", "refs/remotes/origin/main")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".tillit"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".tillit", "config.json"), []byte(fmt.Sprintf(`{"auditor":{"validate":{"command":[%q,"-c","echo validation-output; exit 0"]}}}`, "/bin/sh")), 0o644))
	git("add", ".tillit/config.json")
	git("commit", "-m", "configure")
	return dir
}
