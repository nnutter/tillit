// Package autosubmit captures working changes for daemon-owned validation and
// publishes validated refactors without writing working files.
package autosubmit

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nnutter/spor/internal/repository"
	"github.com/nnutter/spor/internal/smith"
)

func git(ctx context.Context, root, index string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0")
	if index != "" {
		cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+index)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func location(start string) (string, string, error) {
	repo, dir, err := repository.Open(start)
	if err != nil {
		return "", "", err
	}
	_ = repo.Close()
	root, err := repository.WorktreeRoot(dir)
	return root, dir, err
}

func digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

// workingTree builds a tracked-file snapshot with an alternate index. Untracked
// files are deliberately excluded and the real index is never refreshed.
func workingTree(ctx context.Context, root, dir, base string) (string, error) {
	temp, err := os.CreateTemp(filepath.Join(dir, "spor"), "snapshot-index-")
	if err != nil {
		return "", err
	}
	name := temp.Name()
	_ = temp.Close()
	_ = os.Remove(name)
	defer func() { _ = os.Remove(name); _ = os.Remove(name + ".lock") }()
	if _, err := git(ctx, root, name, "read-tree", base); err != nil {
		return "", err
	}
	if _, err := git(ctx, root, name, "add", "-u", "--", "."); err != nil {
		return "", err
	}
	return git(ctx, root, name, "write-tree")
}

func safeState(ctx context.Context, root, dir string) (string, string, error) {
	for _, name := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "sequencer", "BISECT_LOG"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return "", "", fmt.Errorf("git operation in progress: %s", name)
		} else if !os.IsNotExist(err) {
			return "", "", err
		}
	}
	pending, err := smith.Log(ctx, root)
	if err != nil {
		return "", "", err
	}
	if len(pending) > 0 {
		return "", "", fmt.Errorf("manual Spor commits await integration")
	}
	return cleanBranch(ctx, root, dir)
}

func cleanBranch(ctx context.Context, root, dir string) (string, string, error) {
	branch, err := git(ctx, root, "", "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("attached branch required: %w", err)
	}
	base, err := git(ctx, root, "", "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", "", err
	}
	if _, err := git(ctx, root, "", "diff-index", "--cached", "--quiet", base, "--"); err != nil {
		return "", "", fmt.Errorf("clean index required: %w", err)
	}
	// Sparse, assume-unchanged and skip-worktree entries cannot be reconciled safely.
	flags, err := git(ctx, root, "", "ls-files", "-v")
	if err != nil {
		return "", "", err
	}
	for line := range strings.SplitSeq(flags, "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "H ") {
			return "", "", fmt.Errorf("ordinary tracked index required")
		}
	}
	return branch, base, repository.EnsureNoPendingIntegration(dir)
}
