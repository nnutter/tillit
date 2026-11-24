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

	"github.com/nnutter/tillit/internal/config"
	"github.com/nnutter/tillit/internal/repository"
)

// ReportBranch reports cached validation for every commit in a branch snapshot.
// It neither runs validation nor changes the checkout or validation records.
func ReportBranch(ctx context.Context, repoPath string) (BranchResult, error) {
	return reportBranch(ctx, repoPath, false)
}

// ReportEvidence includes the recorded commands and output in the branch report.
func ReportEvidence(ctx context.Context, repoPath string) (BranchResult, error) {
	return reportBranch(ctx, repoPath, true)
}

func reportBranch(ctx context.Context, repoPath string, includeEvidence bool) (BranchResult, error) {
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
	hasEvidence, err := reportEvidenceColumn(ctx, db, includeEvidence)
	if err != nil {
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
		outcome, err := branchReportOutcome(ctx, db, hasEvidence, gitDir, commit)
		if err != nil {
			return result, err
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	return result, nil
}

func reportEvidenceColumn(ctx context.Context, db *sql.DB, includeEvidence bool) (bool, error) {
	if !includeEvidence {
		return false, nil
	}
	if db == nil {
		return false, nil
	}
	return branchEvidenceColumn(ctx, db)
}

func openBranchReport(ctx context.Context, gitDir string) (*sql.DB, error) {
	path := filepath.Join(gitDir, "tillit", "state.db")
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

func branchReportOutcome(ctx context.Context, db *sql.DB, hasEvidence bool, gitDir string, commit *object.Commit) (BranchOutcome, error) {
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
	err = readBranchOutcome(ctx, db, hasEvidence, key, &out)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, fmt.Errorf("read branch validation: %w", err)
	}
	return out, nil
}

func historicalConfig(commit *object.Commit) ([]byte, error) {
	file, err := commit.File(".tillit/config.json")
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
