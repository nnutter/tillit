package auditor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/nnutter/tillit/internal/config"
	"github.com/nnutter/tillit/internal/repository"
)

// BranchOutcome describes the validation of an original branch commit.
type BranchOutcome struct {
	Hash    string
	Subject string
	State   string
	Error   string
}

// BranchResult ties outcomes to the branch snapshot that selected them.
type BranchResult struct {
	Snapshot repository.BranchSnapshot
	Outcomes []BranchOutcome
}

// ValidateBranch validates a fixed branch snapshot without modifying its commits.
// Callers must serialize it with other operations using the Tillit checkout.
func ValidateBranch(ctx context.Context, repoPath string) (BranchResult, error) {
	snapshot, err := repository.SnapshotBranch(ctx, repoPath)
	if err != nil {
		return BranchResult{}, err
	}
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return BranchResult{}, err
	}
	defer func() { _ = repo.Close() }()
	dest, err := repository.CheckoutDir(gitDir)
	if err != nil {
		return BranchResult{}, err
	}
	db, err := openState(ctx, gitDir)
	if err != nil {
		return BranchResult{}, err
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS branch_validation (commit_hash TEXT NOT NULL, input TEXT NOT NULL, state TEXT NOT NULL, error TEXT NOT NULL, PRIMARY KEY (commit_hash, input))`); err != nil {
		return BranchResult{}, err
	}
	result := BranchResult{Snapshot: snapshot}
	for _, hash := range snapshot.Commits {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		commit, err := repo.CommitObject(plumbing.NewHash(hash))
		if err != nil {
			return result, err
		}
		outcome := BranchOutcome{Hash: hash, Subject: subjectOf(commit.Message)}
		outcome, err = validateBranchCommit(ctx, db, gitDir, dest, commit, outcome)
		if err != nil {
			return result, err
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	return result, nil
}

func validateBranchCommit(ctx context.Context, db statedb, gitDir, dest string, commit *object.Commit, outcome BranchOutcome) (BranchOutcome, error) {
	data, err := historicalConfig(commit)
	if err != nil {
		return outcome, err
	}
	cfg, err := config.LoadHistorical(gitDir, data)
	if err != nil {
		outcome.State, outcome.Error = "error", err.Error()
		return outcome, nil
	}
	if len(cfg.Command) == 0 {
		outcome.State, outcome.Error = "error", ErrCommandNotConfigured.Error()
		return outcome, nil
	}
	key, err := branchConfigKey(cfg)
	if err != nil {
		return outcome, err
	}
	outcome, cached, err := cachedBranchOutcome(ctx, db, key, outcome)
	if err != nil {
		return outcome, err
	}
	if cached {
		return outcome, nil
	}
	if err := prepareBranchCheckout(ctx, gitDir, dest, outcome.Hash); err != nil {
		outcome.State, outcome.Error = "error", err.Error()
	} else {
		outcome = runBranchCommand(ctx, gitDir, cfg.Command, outcome)
	}
	if ctx.Err() != nil {
		return outcome, ctx.Err()
	}
	_, err = db.ExecContext(ctx, `INSERT INTO branch_validation (commit_hash, input, state, error) VALUES (?, ?, ?, ?) ON CONFLICT (commit_hash, input) DO UPDATE SET state = excluded.state, error = excluded.error`, outcome.Hash, key, outcome.State, outcome.Error)
	if err != nil {
		return outcome, fmt.Errorf("record branch validation: %w", err)
	}
	return outcome, nil
}

func cachedBranchOutcome(ctx context.Context, db statedb, key string, outcome BranchOutcome) (BranchOutcome, bool, error) {
	err := db.QueryRowContext(ctx, `SELECT state, error FROM branch_validation WHERE commit_hash = ? AND input = ?`, outcome.Hash, key).Scan(&outcome.State, &outcome.Error)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return outcome, false, err
	}
	if err == nil {
		if outcome.State != "error" {
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
	passed, err := runCommand(ctx, gitDir, command)
	outcome.State = "passed"
	if err != nil {
		outcome.State, outcome.Error = "error", err.Error()
	} else if !passed {
		outcome.State, outcome.Error = "failed", "validation command failed"
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
