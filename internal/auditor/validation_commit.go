package auditor

import (
	"context"
	"errors"

	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/repository"
)

// ErrCommandNotConfigured means validation has no command to run.
var ErrCommandNotConfigured = errors.New("validate command is not configured")

// Outcome is the validation state of one commit.
type Outcome struct {
	Hash   string
	Passed bool
}

func historicalConfig(commit *object.Commit) ([]byte, error) {
	file, err := commit.File(".spor/config.json")
	if errors.Is(err, object.ErrFileNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	content, err := file.Contents()
	if err != nil {
		return nil, err
	}
	return []byte(content), nil
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

func validateCommit(ctx context.Context, db statedb, root, gitDir string, command []string, mode config.Mode, commit *object.Commit) (Outcome, bool, error) {
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
