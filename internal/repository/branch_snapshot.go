package repository

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// BranchSnapshot identifies a fixed selection of commits on a user's branch.
// Commits are ordered with parents before children.
type BranchSnapshot struct {
	Ref      string
	Tip      string
	Boundary string
	Commits  []string
}

// SnapshotBranch selects commits reachable from the current branch tip but not
// its boundary. start must be in the user's worktree, not Tillit's checkout.
func SnapshotBranch(ctx context.Context, start string) (BranchSnapshot, error) {
	var snapshot BranchSnapshot
	ref, err := branchGit(ctx, start, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return snapshot, fmt.Errorf("select branch: check out a branch (detached or missing HEAD): %w", err)
	}
	snapshot.Ref = ref
	snapshot.Tip, err = branchGit(ctx, start, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return BranchSnapshot{}, fmt.Errorf("resolve branch tip: %w", err)
	}
	snapshot.Boundary, err = branchBoundary(ctx, start, ref, snapshot.Tip)
	if err != nil {
		return BranchSnapshot{}, err
	}
	commits, err := branchGit(ctx, start, "rev-list", "--reverse", "--topo-order", snapshot.Tip, "^"+snapshot.Boundary)
	if err != nil {
		return BranchSnapshot{}, fmt.Errorf("select branch commits: %w", err)
	}
	snapshot.Commits = strings.Fields(commits)
	return snapshot, nil
}

func branchBoundary(ctx context.Context, dir, ref, tip string) (string, error) {
	mode, err := branchGit(ctx, dir, "config", "--default", "", "--get", "push.default")
	if err != nil {
		return "", fmt.Errorf("read push.default: %w", err)
	}
	if mode == "current" {
		upstream, err := branchGit(ctx, dir, "for-each-ref", "--format=%(upstream)%00%(upstream:remoteref)", ref)
		if err != nil {
			return "", fmt.Errorf("read branch upstream: %w", err)
		}
		tracking, remoteRef, _ := strings.Cut(upstream, "\x00")
		if distinctUpstream(tracking, remoteRef, ref) {
			boundary, err := branchGit(ctx, dir, "rev-parse", "--verify", tracking+"^{commit}")
			if err != nil {
				return "", fmt.Errorf("resolve upstream boundary %s: %w", tracking, err)
			}
			return boundary, nil
		}
	}
	boundary, err := branchGit(ctx, dir, "merge-base", "--fork-point", "origin/HEAD", tip)
	if err != nil {
		return "", fmt.Errorf("find fork-point from origin/HEAD; fetch origin and set its default branch, or configure a distinct upstream with push.default=current: %w", err)
	}
	return boundary, nil
}

func distinctUpstream(tracking, remoteRef, ref string) bool {
	if tracking == "" {
		return false
	}
	return remoteRef != ref
}

func branchGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_PREFIX", "GIT_EXEC_PATH":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
