package mender_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/mender"
	"github.com/nnutter/spor/internal/repository"
	"github.com/nnutter/spor/internal/smith"
)

func TestIntegratesVerifiedPrefixThenPreservesUserCommit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-b", "user")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "config", "user.email", "test@example.com")
	write(t, dir, "base", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	base := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "--detach")
	for _, name := range []string{"A", "B", "C"} {
		write(t, dir, name, name+"\n")
		git(t, dir, "add", name)
		git(t, dir, "commit", "-m", name+"\n\nSpor-Id: S1\nChange-Id: I"+name)
	}
	gitDir := filepath.Join(dir, ".git")
	git(t, dir, "update-ref", repository.HeadRef(gitDir), "HEAD")
	git(t, dir, "checkout", "user")
	require.Equal(t, base, git(t, dir, "rev-parse", "HEAD"))
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "auditor.verify.command", []string{"sh", "-c", "test ! -f C"}))
	result, err := auditor.Verify(t.Context(), dir, "")
	require.NoError(t, err)
	require.True(t, result.Failed)
	gitPath := filepath.Join(git(t, dir, "--exec-path"), "git")
	bin := t.TempDir()
	marker := filepath.Join(bin, "failed")
	script := fmt.Sprintf("#!/bin/sh\ncase \"$*\" in *\"update-ref refs/spor/\"*\"/head \"*) if [ ! -f %q ]; then touch %q; exit 1; fi ;; esac\nexec %q \"$@\"\n", marker, marker, gitPath)
	write(t, bin, "git", script)
	require.NoError(t, os.Chmod(filepath.Join(bin, "git"), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	require.Equal(t, "refs/heads/user", git(t, dir, "symbolic-ref", "-q", "HEAD"))
	_, err = mender.Rebase(t.Context(), dir, "refs/heads/user")
	require.ErrorContains(t, err, "reconcile")
	_, err = smith.Add(t.Context(), dir)
	require.ErrorContains(t, err, "pending rebase")
	_, err = smith.Reset(t.Context(), dir)
	require.ErrorContains(t, err, "pending rebase")
	_, err = auditor.Verify(t.Context(), dir, "")
	require.ErrorContains(t, err, "pending rebase")
	_, err = mender.Rebase(t.Context(), dir, "refs/heads/user")
	require.NoError(t, err)
	require.Equal(t, "B\nA\nbase", git(t, dir, "log", "--format=%s"))
	_, err = os.Stat(filepath.Join(dir, "C"))
	require.ErrorIs(t, err, os.ErrNotExist)
	empty, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.Contains(t, empty.Message, "No staged changes")
	write(t, dir, "new.go", "package p\n")
	git(t, dir, "add", "new.go")
	tip := git(t, dir, "rev-parse", repository.HeadRef(gitDir))
	_, err = smith.Add(t.Context(), dir)
	require.ErrorContains(t, err, "unintegrated")
	require.Equal(t, tip, git(t, dir, "rev-parse", repository.HeadRef(gitDir)))
	git(t, dir, "reset", "--hard", "HEAD")
	write(t, dir, "D", "D\n")
	git(t, dir, "add", "D")
	git(t, dir, "commit", "-m", "D")
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "auditor.verify.command", []string{"sh", "-c", "true"}))
	result, err = auditor.Verify(t.Context(), dir, "")
	require.NoError(t, err)
	require.False(t, result.Failed)
	_, err = mender.Rebase(t.Context(), dir, "refs/heads/user")
	require.NoError(t, err)
	require.Equal(t, "C\nD\nB\nA\nbase", git(t, dir, "log", "--format=%s"))
	_, err = mender.Rebase(t.Context(), dir, "refs/heads/user")
	require.NoError(t, err)
	require.Equal(t, "C\nD\nB\nA\nbase", git(t, dir, "log", "--format=%s"))
	write(t, dir, "new.go", "package p\n")
	git(t, dir, "add", "new.go")
	_, err = smith.Add(t.Context(), dir)
	require.NoError(t, err)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_PREFIX", "GIT_EXEC_PATH", "GIT_SEQUENCE_EDITOR":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
