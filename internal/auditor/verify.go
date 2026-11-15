// Package auditor verifies commits on the spor ref.
package auditor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/repository"
)

// ErrCommandNotConfigured means verification has no command to run.
var ErrCommandNotConfigured = errors.New("verify command is not configured")

// Outcome is the verification state of one commit.
type Outcome struct {
	Hash   string
	Passed bool
}

// Result is one verification walk.
type Result struct {
	Mode     config.Mode
	Outcomes []Outcome
	Failed   bool
}

// Verify checks unverified spor commits from oldest to newest.
// An empty mode uses the configured mode. The walk stops at the first failure.
func Verify(ctx context.Context, repoPath string, mode config.Mode) (Result, error) {
	repo, gitDir, cfg, mode, err := openVerify(repoPath, mode)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = repo.Close() }()
	if len(cfg.Command) == 0 {
		return Result{}, ErrCommandNotConfigured
	}
	commits, err := sporCommits(repo, gitDir)
	if err != nil {
		return Result{}, err
	}
	return walk(ctx, repoPath, gitDir, cfg.Command, mode, commits)
}

// Report describes the recorded state of each spor commit.
// Commits that have not been checked are omitted.
func Report(ctx context.Context, repoPath string) (string, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = repo.Close() }()
	cfg, err := config.Load(gitDir)
	if err != nil {
		return "", err
	}
	commits, err := sporCommits(repo, gitDir)
	if err != nil {
		return "", err
	}
	outcomes, err := recorded(ctx, gitDir, cfg.Mode, commits)
	if err != nil {
		return "", err
	}
	return Format(cfg.Mode, outcomes), nil
}

// Statuses reports a verification word for each hash under the configured mode.
// Words are passed, failed, or unverified, in the same order as hashes.
func Statuses(ctx context.Context, repoPath string, hashes []string) ([]string, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = repo.Close() }()
	cfg, err := config.Load(gitDir)
	if err != nil {
		return nil, err
	}
	db, err := openState(ctx, gitDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	words := make([]string, len(hashes))
	for i, hash := range hashes {
		commit, err := repo.CommitObject(plumbing.NewHash(hash))
		if err != nil {
			return nil, err
		}
		item, err := identify(ctx, gitDir, commit)
		if err != nil {
			return nil, err
		}
		outcome, ok, err := recordedOutcome(ctx, db, cfg.Mode, item)
		if err != nil {
			return nil, err
		}
		if !ok {
			words[i] = "unverified"
			continue
		}
		words[i] = outcomeWord(outcome.Passed)
	}
	return words, nil
}

// Format renders a walk as stable plain text.
func Format(mode config.Mode, outcomes []Outcome) string {
	if len(outcomes) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "mode: %s\n", mode)
	for _, outcome := range outcomes {
		fmt.Fprintf(&b, "%s %s\n", shortHash(outcome.Hash), outcomeWord(outcome.Passed))
	}
	return b.String()
}

func openVerify(repoPath string, mode config.Mode) (*git.Repository, string, config.Config, config.Mode, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return nil, "", config.Config{}, "", err
	}
	cfg, err := config.Load(gitDir)
	if err != nil {
		_ = repo.Close()
		return nil, "", config.Config{}, "", err
	}
	if mode == "" {
		mode = cfg.Mode
	}
	if _, err := config.ParseMode(string(mode)); err != nil {
		_ = repo.Close()
		return nil, "", config.Config{}, "", err
	}
	return repo, gitDir, cfg, mode, nil
}

func walk(ctx context.Context, repoPath, gitDir string, command []string, mode config.Mode, commits []*object.Commit) (Result, error) {
	result := Result{Mode: mode}
	if len(commits) == 0 {
		return result, nil
	}
	root, err := worktreeRoot(ctx, repoPath)
	if err != nil {
		return Result{}, err
	}
	db, err := openState(ctx, gitDir)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = db.Close() }()
	for _, commit := range commits {
		outcome, stop, err := verifyCommit(ctx, db, root, gitDir, command, mode, commit)
		if err != nil {
			return Result{}, err
		}
		result.Outcomes = append(result.Outcomes, outcome)
		if stop {
			result.Failed = true
			return result, nil
		}
	}
	return result, nil
}

func verifyCommit(ctx context.Context, db statedb, root, gitDir string, command []string, mode config.Mode, commit *object.Commit) (Outcome, bool, error) {
	item, err := identify(ctx, gitDir, commit)
	if err != nil {
		return Outcome{}, false, err
	}
	passed, err := alreadyPassed(ctx, db, mode, item)
	if err != nil {
		return Outcome{}, false, err
	}
	if passed {
		return Outcome{Hash: item.hash, Passed: true}, false, nil
	}
	if err := prepareCheckout(ctx, root, gitDir, item.hash); err != nil {
		return Outcome{}, false, err
	}
	ok, err := runCommand(ctx, gitDir, command)
	if err != nil {
		return Outcome{}, false, err
	}
	if err := record(ctx, db, item, ok); err != nil {
		return Outcome{}, false, err
	}
	return Outcome{Hash: item.hash, Passed: ok}, !ok, nil
}

func prepareCheckout(ctx context.Context, root, gitDir, hash string) error {
	dest, err := repository.CheckoutDir(gitDir)
	if err != nil {
		return err
	}
	ready, err := worktreeReady(dest)
	if err != nil {
		return err
	}
	if ready {
		// The checkout can load mise.toml that is already in the tree.
		if err := syncMiseTrust(ctx, root, dest); err != nil {
			return err
		}
	}
	if err := checkout(ctx, gitDir, hash); err != nil {
		return err
	}
	// Again, so content-hashed trust matches the commit just checked out.
	return syncMiseTrust(ctx, root, dest)
}

func recorded(ctx context.Context, gitDir string, mode config.Mode, commits []*object.Commit) ([]Outcome, error) {
	if len(commits) == 0 {
		return nil, nil
	}
	db, err := openState(ctx, gitDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	var outcomes []Outcome
	for _, commit := range commits {
		item, err := identify(ctx, gitDir, commit)
		if err != nil {
			return nil, err
		}
		outcome, ok, err := recordedOutcome(ctx, db, mode, item)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		outcomes = append(outcomes, outcome)
		if !outcome.Passed {
			break
		}
	}
	return outcomes, nil
}

func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func outcomeWord(passed bool) string {
	if passed {
		return "passed"
	}
	return "failed"
}
