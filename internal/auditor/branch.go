package auditor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/nnutter/commitstack"

	"github.com/nnutter/tillit/internal/config"
	"github.com/nnutter/tillit/internal/repository"
)

type repoType = *git.Repository

// BranchOutcome describes the validation of a commit stack. Hash is the
// stack tip: the commit that contains the stack's original commit and all of
// its attached fixups.
type BranchOutcome struct {
	Hash     string    `json:"commit"`
	Subject  string    `json:"subject"`
	State    string    `json:"validation"`
	Error    string    `json:"error,omitempty"`
	Evidence *Evidence `json:"evidence,omitzero"`
}

// BranchResult ties one outcome per stack to the snapshot that selected them.
type BranchResult struct {
	Snapshot commitstack.Snapshot
	Outcomes []BranchOutcome
}

// ValidateBranch validates a fixed branch snapshot without modifying its commits.
// Callers must serialize it with other operations using the Tillit checkout.
func ValidateBranch(ctx context.Context, repoPath string) (BranchResult, error) {
	snapshot, repo, gitDir, dest, db, err := openValidation(ctx, repoPath)
	if err != nil {
		return BranchResult{}, err
	}
	defer func() { _ = repo.Close() }()
	defer func() { _ = db.Close() }()
	return validateSnapshot(ctx, snapshot, repo, gitDir, dest, db)
}

// ResetFailedValidation clears a cached failed validation for commit hash so
// it returns to pending and will be re-validated on the next pass.
// It returns true if a failed validation record was cleared.
func ResetFailedValidation(ctx context.Context, repoPath, hash string) (bool, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return false, err
	}
	defer func() { _ = repo.Close() }()
	commit, err := repo.CommitObject(plumbing.NewHash(hash))
	if err != nil {
		return false, err
	}
	data, err := historicalConfig(commit)
	if err != nil {
		return false, err
	}
	cfg, err := config.LoadHistorical(gitDir, data)
	if err != nil {
		return false, err
	}
	if len(cfg.Command) == 0 {
		return false, nil
	}
	key, err := branchConfigKey(cfg)
	if err != nil {
		return false, err
	}
	db, ok, err := openExistingState(ctx, gitDir)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	defer func() { _ = db.Close() }()
	return deleteFailedBranchOutcome(ctx, db.DB, commit.Hash.String(), key)
}

func openValidation(ctx context.Context, repoPath string) (commitstack.Snapshot, repoType, string, string, statedb, error) {
	snapshot, err := repository.SnapshotStacks(ctx, repoPath)
	if err != nil {
		return snapshot, nil, "", "", statedb{}, err
	}
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return snapshot, nil, "", "", statedb{}, err
	}
	dest, err := repository.CheckoutDir(gitDir)
	if err != nil {
		_ = repo.Close()
		return snapshot, nil, "", "", statedb{}, err
	}
	db, err := openState(ctx, gitDir)
	if err != nil {
		_ = repo.Close()
		return snapshot, nil, "", "", statedb{}, err
	}
	if err := ensureBranchValidation(ctx, db.DB); err != nil {
		_ = repo.Close()
		_ = db.Close()
		return snapshot, nil, "", "", statedb{}, err
	}
	return snapshot, repo, gitDir, dest, db, nil
}

func validateSnapshot(ctx context.Context, snapshot commitstack.Snapshot, repo repoType, gitDir, dest string, db statedb) (BranchResult, error) {
	result := BranchResult{Snapshot: snapshot}
	for i, stack := range snapshot.Stacks {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		commit, err := repo.CommitObject(plumbing.NewHash(stack.Tip))
		if err != nil {
			return result, err
		}
		outcome := BranchOutcome{Hash: stack.Tip, Subject: stack.Subject}
		outcome, wasCached, err := validateBranchCommit(ctx, db, gitDir, dest, commit, outcome)
		if err != nil {
			return result, err
		}
		result.Outcomes = append(result.Outcomes, outcome)
		if shouldStopOnFailure(wasCached, outcome.State) {
			fillRemainingPending(snapshot, i, &result)
			break
		}
	}
	return result, nil
}

func isFailedState(s string) bool { return s == "failed" }
func isNotCached(b bool) bool     { return !b }
func shouldStopOnFailure(wasCached bool, state string) bool {
	return isNotCached(wasCached) && isFailedState(state)
}

func fillRemainingPending(snapshot commitstack.Snapshot, index int, result *BranchResult) {
	for _, remaining := range snapshot.Stacks[index+1:] {
		result.Outcomes = append(result.Outcomes, BranchOutcome{Hash: remaining.Tip, Subject: remaining.Subject, State: "pending"})
	}
}

func validateBranchCommit(ctx context.Context, db statedb, gitDir, dest string, commit *object.Commit, outcome BranchOutcome) (BranchOutcome, bool, error) {
	data, err := historicalConfig(commit)
	if err != nil {
		return outcome, false, err
	}
	cfg, err := config.LoadHistorical(gitDir, data)
	if err != nil {
		outcome.State, outcome.Error = "error", err.Error()
		return outcome, false, nil
	}
	if len(cfg.Command) == 0 {
		outcome.State, outcome.Error = "error", ErrCommandNotConfigured.Error()
		return outcome, false, nil
	}
	key, err := branchConfigKey(cfg)
	if err != nil {
		return outcome, false, err
	}
	outcome, cached, err := cachedBranchOutcome(ctx, db, key, outcome)
	if err != nil {
		return outcome, false, err
	}
	if cached {
		return outcome, true, nil
	}
	// Persist a transient "running" state so tillit status shows progress
	// instead of "pending" while the command executes.
	outcome.State = "running"
	outcome.Error = ""
	outcome.Evidence = nil
	if err := recordBranchOutcome(ctx, db.DB, key, outcome); err != nil {
		return outcome, false, fmt.Errorf("record running validation: %w", err)
	}
	if err := prepareBranchCheckout(ctx, gitDir, dest, outcome.Hash); err != nil {
		outcome.State, outcome.Error = "error", err.Error()
	} else {
		outcome = runBranchCommand(ctx, gitDir, cfg.Command, outcome)
	}
	if ctx.Err() != nil {
		return outcome, false, ctx.Err()
	}
	finished, err := finishBranchValidation(ctx, db, gitDir, dest, key, outcome)
	return finished, false, err
}

func finishBranchValidation(ctx context.Context, db statedb, gitDir, dest, key string, outcome BranchOutcome) (BranchOutcome, error) {
	if err := recordBranchOutcome(ctx, db.DB, key, outcome); err != nil {
		return outcome, fmt.Errorf("record branch validation: %w", err)
	}
	if outcome.State == "failed" {
		if err := recordReproductions(ctx, db, gitDir, dest, key, outcome); err != nil {
			return outcome, err
		}
	}
	outcome.Evidence = nil
	return outcome, nil
}

func recordReproductions(ctx context.Context, db statedb, gitDir, dest, key string, outcome BranchOutcome) error {
	if outcome.Evidence == nil {
		return nil
	}
	if !collectReproductions(ctx, gitDir, dest, outcome.Hash, outcome.Evidence) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := recordBranchOutcome(ctx, db.DB, key, outcome); err != nil {
		return fmt.Errorf("record isolated reproductions: %w", err)
	}
	return nil
}

func isNotError(s string) bool   { return s != "error" }
func isNotRunning(s string) bool { return s != "running" }
func isCachedState(s string) bool {
	return isNotError(s) && isNotRunning(s)
}

func cachedBranchOutcome(ctx context.Context, db statedb, key string, outcome BranchOutcome) (BranchOutcome, bool, error) {
	err := readBranchOutcome(ctx, db.DB, false, key, &outcome)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return outcome, false, err
	}
	if err == nil {
		if isCachedState(outcome.State) {
			return outcome, true, nil
		}
	}
	outcome.Error = ""
	return outcome, false, nil
}

func branchConfigKey(cfg config.Config) (string, error) {
	input, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:]), nil
}

func runBranchCommand(ctx context.Context, gitDir string, command []string, outcome BranchOutcome) BranchOutcome {
	args, goJSON := evidenceCommand(command)
	passed, output, err := runCommand(ctx, gitDir, args, goJSON)
	outcome.Evidence = new(Evidence{
		Command: args, Output: output.Text, OutputTruncated: output.Truncated,
		Failures: []Failure{},
	})
	outcome.State = "passed"
	if err != nil {
		outcome.State, outcome.Error = "error", err.Error()
	} else if !passed {
		outcome.State, outcome.Error = "failed", "validation command failed"
		outcome.Evidence.Failures = evidenceFailures(output)
	}
	return outcome
}

func prepareBranchCheckout(ctx context.Context, gitDir, dest, hash string) error {
	ready, err := worktreeReady(dest)
	if err != nil {
		return err
	}
	if ready {
		if err := runGit(ctx, dest, "clean", "-ffdx"); err != nil {
			return err
		}
	}
	if err := checkout(ctx, gitDir, hash); err != nil {
		return err
	}
	return runGit(ctx, dest, "clean", "-ffdx")
}
