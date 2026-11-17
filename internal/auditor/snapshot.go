package auditor

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/repository"
)

// ValidateSnapshot checks exact auto-submission commits in the daemon checkout.
// It never updates manual Spor history or invokes fixup agents. Callers must hold
// the worktree writer lock and serialize use of the daemon checkout.
func ValidateSnapshot(ctx context.Context, start string, hashes []string) (bool, error) {
	repo, gitDir, err := repository.Open(start)
	if err != nil {
		return false, err
	}
	defer func() { _ = repo.Close() }()
	db, err := openState(ctx, gitDir)
	if err != nil {
		return false, err
	}
	defer func() { _ = db.Close() }()
	root, err := worktreeRoot(ctx, start)
	if err != nil {
		return false, err
	}
	dest, err := repository.CheckoutDir(gitDir)
	if err != nil {
		return false, err
	}
	for _, hash := range hashes {
		commit, err := repo.CommitObject(plumbing.NewHash(hash))
		if err != nil {
			return false, err
		}
		outcome, err := validateSnapshotCommit(ctx, db, root, gitDir, dest, commit)
		if err != nil {
			return false, err
		}
		if !outcome.Passed {
			return false, nil
		}
	}
	return len(hashes) > 0, nil
}

func validateSnapshotCommit(ctx context.Context, db statedb, root, gitDir, dest string, commit *object.Commit) (Outcome, error) {
	data, err := historicalConfig(commit)
	if err != nil {
		return Outcome{}, err
	}
	cfg, err := config.LoadHistorical(gitDir, data)
	if err != nil {
		return Outcome{}, err
	}
	if err := prepareBranchCheckout(ctx, gitDir, dest, commit.Hash.String()); err != nil {
		return Outcome{}, err
	}
	outcome, _, err := validateCommit(ctx, db, root, gitDir, cfg.Command, config.ModeCommit, commit)
	return outcome, err
}
