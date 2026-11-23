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

	"github.com/go-git/go-git/v6/plumbing"

	"github.com/nnutter/spor/internal/repository"
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
	ref := repository.HeadRef(dir)
	refs, err := git(ctx, root, "", "for-each-ref", "--format=%(refname)", ref)
	if err != nil {
		return "", "", err
	}
	for name := range strings.SplitSeq(refs, "\n") {
		if name != ref {
			continue
		}
		pending, err := pendingManual(ctx, root, dir, ref)
		if err != nil {
			return "", "", err
		}
		if pending {
			return "", "", fmt.Errorf("manual Spor commits await integration; use a previous Spor version or resolve them safely before continuing")
		}
	}
	return cleanBranch(ctx, root, dir)
}

// pendingManual follows the legacy Spor chain up to its recorded integration
// boundary. A rebased integration need not leave its original hashes on HEAD.
func pendingManual(ctx context.Context, root, dir, ref string) (bool, error) {
	boundary, err := repository.Boundary(dir)
	if err != nil {
		return false, err
	}
	ahead, err := git(ctx, root, "", "rev-list", ref, "--not", "HEAD")
	if err != nil {
		return false, err
	}
	unreachable := hashSet(ahead)
	repo, _, err := repository.Open(root)
	if err != nil {
		return false, err
	}
	defer func() { _ = repo.Close() }()
	legacy, err := repo.Reference(plumbing.ReferenceName(ref), true)
	if err != nil {
		return false, err
	}
	hash := legacy.Hash()
	for hash.String() != boundary {
		commit, err := repo.CommitObject(hash)
		if err != nil {
			return false, err
		}
		if !hasSporID(commit.Message) {
			break
		}
		if unreachable[hash.String()] {
			return true, nil
		}
		if len(commit.ParentHashes) == 0 {
			break
		}
		hash = commit.ParentHashes[0]
	}
	return false, nil
}

func hashSet(lines string) map[string]bool {
	set := make(map[string]bool)
	for hash := range strings.SplitSeq(lines, "\n") {
		set[hash] = true
	}
	return set
}

func hasSporID(message string) bool {
	for line := range strings.SplitSeq(message, "\n") {
		if strings.HasPrefix(line, "Spor-Id: ") {
			return true
		}
	}
	return false
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
