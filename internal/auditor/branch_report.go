package auditor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/repository"
)

// ReportBranch reports cached validation for every commit in a branch snapshot.
// It neither runs validation nor changes the checkout or validation records.
func ReportBranch(ctx context.Context, repoPath string) (BranchResult, error) {
	snapshot, err := repository.SnapshotBranch(ctx, repoPath)
	if err != nil {
		return BranchResult{}, err
	}
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return BranchResult{}, err
	}
	defer func() { _ = repo.Close() }()
	db, err := openBranchReport(ctx, gitDir)
	if err != nil {
		return BranchResult{}, err
	}
	if db != nil {
		defer func() { _ = db.Close() }()
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
		outcome, err := branchReportOutcome(ctx, db, gitDir, commit)
		if err != nil {
			return result, err
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	return result, nil
}

func openBranchReport(ctx context.Context, gitDir string) (*sql.DB, error) {
	path := filepath.Join(gitDir, "spor", "state.db")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	var exists int
	err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='branch_validation'`).Scan(&exists)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if exists == 0 {
		_ = db.Close()
		return nil, nil
	}
	return db, nil
}

func branchReportOutcome(ctx context.Context, db *sql.DB, gitDir string, commit *object.Commit) (BranchOutcome, error) {
	out := BranchOutcome{Hash: commit.Hash.String(), Subject: subjectOf(commit.Message), State: "pending"}
	data, err := historicalConfig(commit)
	if err != nil {
		return out, err
	}
	cfg, err := config.LoadHistorical(gitDir, data)
	if err != nil {
		out.State, out.Error = "error", err.Error()
		return out, nil
	}
	if len(cfg.Command) == 0 {
		out.State, out.Error = "error", ErrCommandNotConfigured.Error()
		return out, nil
	}
	if db == nil {
		return out, nil
	}
	key, err := branchConfigKey(cfg)
	if err != nil {
		return out, err
	}
	err = db.QueryRowContext(ctx, `SELECT state, error FROM branch_validation WHERE commit_hash = ? AND input = ?`, out.Hash, key).Scan(&out.State, &out.Error)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, fmt.Errorf("read branch validation: %w", err)
	}
	return out, nil
}
