// Package mender incorporates verified Spor commits into the user's branch.
package mender

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/repository"
)

// Rebase prepares verified commits in an isolated worktree and publishes them
// only if the requested branch and its worktree are still unchanged.
func Rebase(ctx context.Context, repoPath, branch string) (string, error) {
	if !strings.HasPrefix(branch, "refs/heads/") {
		return "", fmt.Errorf("rebase requires an attached target branch")
	}
	if err := checkWorktree(ctx, repoPath, branch, ""); err != nil {
		return "", err
	}
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return "", err
	}
	_ = repo.Close()
	if err := os.MkdirAll(filepath.Join(gitDir, "spor"), 0o755); err != nil {
		return "", err
	}
	db, err := state(ctx, gitDir)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	op, err := pending(ctx, db)
	if err != nil {
		return "", err
	}
	if op.branch != "" {
		return recoverOperation(ctx, repoPath, gitDir, db, op)
	}
	return prepareOperation(ctx, repoPath, gitDir, branch, db)
}

func prepareOperation(ctx context.Context, repoPath, gitDir, branch string, db *sql.DB) (string, error) {
	head, err := git(ctx, repoPath, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	boundary, err := repository.Boundary(gitDir)
	if err != nil {
		return "", err
	}
	if err := checkBoundary(ctx, repoPath, boundary, head); err != nil {
		return "", err
	}
	prefix, commits, err := auditor.Pending(ctx, repoPath)
	if err != nil {
		return "", err
	}
	if len(prefix) == 0 {
		return "No verified commits to integrate.", nil
	}
	oldTip, err := git(ctx, repoPath, "rev-parse", repository.HeadRef(gitDir))
	if err != nil {
		return "", err
	}
	cfg, err := config.Load(gitDir)
	if err != nil {
		return "", err
	}
	op := operation{branch: branch, oldHead: head, oldTip: oldTip, oldBoundary: boundary}
	return prepareWorktree(ctx, repoPath, gitDir, db, op, commits, len(prefix), cfg.Mender)
}

func prepareWorktree(ctx context.Context, repoPath, gitDir string, db *sql.DB, op operation, commits []string, prefixLen int, model config.Auditor) (string, error) {
	dir, err := os.MkdirTemp("", "spor-mender-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if _, err := git(ctx, repoPath, "worktree", "add", "--detach", dir, op.oldHead); err != nil {
		return "", err
	}
	defer func() { _, _ = git(context.WithoutCancel(ctx), repoPath, "worktree", "remove", "--force", dir) }()
	if err := prepareCommits(ctx, dir, commits, prefixLen, model, &op); err != nil {
		return "", err
	}
	if err := checkWorktree(ctx, repoPath, op.branch, op.oldHead); err != nil {
		return "", err
	}
	return recordOperation(ctx, repoPath, gitDir, db, op)
}

func checkBoundary(ctx context.Context, dir, boundary, head string) error {
	if boundary == "" {
		return nil
	}
	if _, err := git(ctx, dir, "merge-base", "--is-ancestor", boundary, head); err != nil {
		return fmt.Errorf("integrated boundary is not an ancestor of branch: %w", err)
	}
	return nil
}

func prepareCommits(ctx context.Context, dir string, commits []string, prefixLen int, model config.Auditor, op *operation) error {
	for i, hash := range commits {
		if _, err := git(ctx, dir, "cherry-pick", "--allow-empty", "--empty=keep", hash); err != nil {
			if err := resolve(ctx, dir, model); err != nil {
				return err
			}
		}
		if i+1 == prefixLen {
			prefix, err := git(ctx, dir, "rev-parse", "HEAD")
			if err != nil {
				return err
			}
			op.prefix = prefix
		}
	}
	tip, err := git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	op.tip = tip
	return nil
}

func recordOperation(ctx context.Context, repoPath, gitDir string, db *sql.DB, op operation) (string, error) {
	current, err := git(ctx, repoPath, "rev-parse", repository.HeadRef(gitDir))
	if err != nil {
		return "", fmt.Errorf("spor history changed during preparation")
	}
	if current != op.oldTip {
		return "", fmt.Errorf("spor history changed during preparation")
	}
	if _, err := git(ctx, repoPath, "update-ref", repository.HeadRef(gitDir)+"-prepared", op.tip); err != nil {
		return "", err
	}
	if err := save(ctx, db, op); err != nil {
		_, _ = git(context.WithoutCancel(ctx), repoPath, "update-ref", "-d", repository.HeadRef(gitDir)+"-prepared", op.tip)
		return "", err
	}
	return recoverOperation(ctx, repoPath, gitDir, db, op)
}

func recoverOperation(ctx context.Context, dir, gitDir string, db *sql.DB, op operation) (string, error) {
	if err := checkWorktree(ctx, dir, op.branch, ""); err != nil {
		return "", err
	}
	head, err := git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	boundary, err := repository.Boundary(gitDir)
	if err != nil {
		return "", err
	}
	if boundary != op.oldBoundary {
		return "", fmt.Errorf("spor boundary changed during integration")
	}
	ref := repository.HeadRef(gitDir)
	tip, err := git(ctx, dir, "rev-parse", ref)
	if err != nil {
		return "", err
	}
	if !operationHasTip(op, tip) {
		return "", fmt.Errorf("spor history changed during integration")
	}
	if err := publishPrefix(ctx, dir, op, head, tip); err != nil {
		return "", err
	}
	if _, err := git(ctx, dir, "merge-base", "--is-ancestor", op.prefix, "HEAD"); err != nil {
		return "", fmt.Errorf("cannot confirm published prefix: %w", err)
	}
	if err := reconcileOperation(ctx, dir, db, op, ref, tip); err != nil {
		return "", err
	}
	return "Integrated verified commits.", nil
}

func reconcileOperation(ctx context.Context, dir string, db *sql.DB, op operation, ref, tip string) error {
	if tip == op.oldTip {
		if _, err := git(ctx, dir, "update-ref", ref, op.tip, op.oldTip); err != nil {
			return fmt.Errorf("reconcile Spor history: %w", err)
		}
	}
	if _, err := git(ctx, dir, "update-ref", "-d", ref+"-prepared", op.tip); err != nil {
		return err
	}
	return finish(ctx, db, op)
}

func operationHasTip(op operation, tip string) bool {
	if tip == op.oldTip {
		return true
	}
	return tip == op.tip
}

func publishPrefix(ctx context.Context, dir string, op operation, head, tip string) error {
	if head != op.oldHead {
		return nil
	}
	if tip != op.oldTip {
		return fmt.Errorf("spor history was updated before publication")
	}
	if err := checkWorktree(ctx, dir, op.branch, op.oldHead); err != nil {
		return err
	}
	if _, err := git(ctx, dir, "merge", "--ff-only", op.prefix); err != nil {
		return fmt.Errorf("publish verified commits: %w", err)
	}
	return nil
}

func checkWorktree(ctx context.Context, dir, branch, expected string) error {
	current, err := git(ctx, dir, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return fmt.Errorf("rebase requires an attached branch")
	}
	if current == "" {
		return fmt.Errorf("rebase requires an attached branch")
	}
	if branch != "" {
		if current != branch {
			return fmt.Errorf("branch changed: expected %s, found %s", branch, current)
		}
	}
	if expected != "" {
		head, err := git(ctx, dir, "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		if head != expected {
			return fmt.Errorf("branch HEAD changed during rebase")
		}
	}
	status, err := git(ctx, dir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("rebase requires a clean worktree and index")
	}
	return checkGitOperations(ctx, dir)
}

func checkGitOperations(ctx context.Context, dir string) error {
	for _, name := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "sequencer"} {
		path, err := git(ctx, dir, "rev-parse", "--git-path", name)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("rebase refused during an active Git operation")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func resolve(ctx context.Context, dir string, model config.Auditor) error {
	conflicts, err := git(ctx, dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return fmt.Errorf("cherry-pick failed without resolvable conflicts")
	}
	if conflicts == "" {
		return fmt.Errorf("cherry-pick failed without resolvable conflicts")
	}
	head, err := git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	picked, err := git(ctx, dir, "rev-parse", "CHERRY_PICK_HEAD")
	if err != nil {
		return err
	}
	paths, err := git(ctx, dir, "diff", "--name-only", "HEAD")
	if err != nil {
		return err
	}
	allowed := make(map[string]bool)
	for path := range strings.SplitSeq(paths, "\n") {
		allowed[path] = true
	}
	if !configuredModel(model) {
		return fmt.Errorf("conflict requires mender.conflict.model.provider and version")
	}
	if err := runConflictAgent(ctx, dir, model); err != nil {
		return err
	}
	return finishConflict(ctx, dir, head, picked, allowed)
}

func configuredModel(model config.Auditor) bool {
	if model.Provider == "" {
		return false
	}
	return model.Model != ""
}

func runConflictAgent(ctx context.Context, dir string, model config.Auditor) error {
	thinking := "low"
	if model.Thinking != nil {
		thinking = model.Thinking.String()
	}
	prompt := "Resolve the current Git cherry-pick conflicts in this checkout. Preserve the intended changes, stage the resolutions, and do not commit."
	agentCtx, cancel := context.WithTimeoutCause(ctx, 10*time.Minute, fmt.Errorf("conflict agent timed out"))
	defer cancel()
	cmd := exec.CommandContext(agentCtx, "pi", "-p", "--provider", model.Provider, "--model", model.Model, "--thinking", thinking, "--", prompt)
	cmd.Dir = dir
	cmd.Env = cleanEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		if cause := context.Cause(agentCtx); cause != nil {
			return fmt.Errorf("resolve conflict with pi: %w", cause)
		}
		return fmt.Errorf("resolve conflict with pi: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func finishConflict(ctx context.Context, dir, head, picked string, allowed map[string]bool) error {
	after, err := git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("pi changed the preparation HEAD")
	}
	if after != head {
		return fmt.Errorf("pi changed the preparation HEAD")
	}
	branch, err := git(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("pi changed the preparation branch")
	}
	if branch != "HEAD" {
		return fmt.Errorf("pi changed the preparation branch")
	}
	currentPick, err := git(ctx, dir, "rev-parse", "CHERRY_PICK_HEAD")
	if err != nil {
		return fmt.Errorf("pi changed the cherry-pick operation")
	}
	if currentPick != picked {
		return fmt.Errorf("pi changed the cherry-pick operation")
	}
	return stageConflict(ctx, dir, allowed)
}

func stageConflict(ctx context.Context, dir string, allowed map[string]bool) error {
	if _, err := git(ctx, dir, "add", "-A"); err != nil {
		return err
	}
	unmerged, err := git(ctx, dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return err
	}
	if unmerged != "" {
		return fmt.Errorf("pi left unresolved conflicts")
	}
	changed, err := git(ctx, dir, "diff", "--cached", "--name-only")
	if err != nil {
		return err
	}
	for path := range strings.SplitSeq(changed, "\n") {
		if path != "" {
			if !allowed[path] {
				return fmt.Errorf("pi changed unexpected path %s", path)
			}
		}
	}
	if _, err := git(ctx, dir, "-c", "core.editor=true", "cherry-pick", "--continue"); err != nil {
		return fmt.Errorf("continue after conflict resolution: %w", err)
	}
	return nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	args = append([]string{"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = cleanEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func cleanEnv() []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_PREFIX", "GIT_EXEC_PATH", "GIT_SEQUENCE_EDITOR":
			continue
		}
		env = append(env, entry)
	}
	return env
}
