package verifier

import (
	"context"
	"database/sql"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/nnutter/tillit/internal/repository"
)

// BranchOutcome is one independent static verification result. A nil Result
// means no result is recorded; Error describes an analysis failure, not a proof.
type BranchOutcome struct {
	Hash   string
	Result *Result
	Error  string
}

// BranchResult associates static outcomes with the selected immutable commits.
type BranchResult struct {
	Snapshot repository.BranchSnapshot
	Outcomes []BranchOutcome
}

// VerifyBranch analyzes uncached commits and persists results, including
// unsupported results. Recorded transient errors are retried on the next run.
func VerifyBranch(ctx context.Context, repoPath string) (BranchResult, error) {
	return branch(ctx, repoPath, true)
}

// ReportBranch reads cached results without running analysis or changing state.
func ReportBranch(ctx context.Context, repoPath string) (BranchResult, error) {
	return branch(ctx, repoPath, false)
}

func branch(ctx context.Context, repoPath string, verify bool) (BranchResult, error) {
	snapshot, err := repository.SnapshotBranch(ctx, repoPath)
	if err != nil {
		return BranchResult{}, err
	}
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return BranchResult{}, err
	}
	defer func() { _ = repo.Close() }()
	db, err := openVerificationState(ctx, filepath.Join(gitDir, "tillit", "state.db"), verify)
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
		outcome, err := branchCommit(ctx, db, repoPath, hash, verify)
		if err != nil {
			return result, err
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	return result, nil
}

func openVerificationState(ctx context.Context, path string, write bool) (*sql.DB, error) {
	if !write {
		return openVerificationReport(ctx, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=busy_timeout(5000)"}).String())
	if err != nil {
		return nil, fmt.Errorf("open verification state: %w", err)
	}
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS semantic_verification (commit_hash TEXT NOT NULL, analyzer TEXT NOT NULL, language TEXT NOT NULL, runtime TEXT NOT NULL, result TEXT NOT NULL, PRIMARY KEY (commit_hash, analyzer, language, runtime))`)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create verification state: %w", err)
	}
	return db, nil
}

func openVerificationReport(ctx context.Context, path string) (*sql.DB, error) {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat verification state: %w", err)
	}
	u := &url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open verification state: %w", err)
	}
	var exists int
	err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='semantic_verification')`).Scan(&exists)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("read verification schema: %w", err)
	}
	if exists == 0 {
		_ = db.Close()
		return nil, nil
	}
	return db, nil
}

func branchCommit(ctx context.Context, db *sql.DB, repo, hash string, verify bool) (BranchOutcome, error) {
	outcome := BranchOutcome{Hash: hash}
	if db == nil {
		return outcome, nil
	}
	cached, lastError, err := readVerification(ctx, db, hash)
	if err != nil {
		return outcome, err
	}
	outcome.Result, outcome.Error = cached, lastError
	if !verify {
		return outcome, nil
	}
	if cached != nil {
		return outcome, nil
	}
	checked, err := VerifyCommit(ctx, repo, hash)
	if err != nil {
		if ctx.Err() != nil {
			return outcome, ctx.Err()
		}
		outcome.Error = err.Error()
		if err := recordVerificationValue(ctx, db, hash, "error:"+outcome.Error); err != nil {
			return outcome, err
		}
		return outcome, nil
	}
	if err := recordVerification(ctx, db, hash, checked); err != nil {
		return outcome, err
	}
	outcome.Result, outcome.Error = &checked, ""
	return outcome, nil
}

func readVerification(ctx context.Context, db *sql.DB, hash string) (*Result, string, error) {
	var raw string
	err := db.QueryRowContext(ctx, `SELECT result FROM semantic_verification WHERE commit_hash=? AND analyzer=? AND language=? AND runtime=?`, hash, AnalyzerVersion, languageVersion, runtime.Version()).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("read verification for %s: %w", hash, err)
	}
	if lastError, ok := strings.CutPrefix(raw, "error:"); ok {
		return nil, lastError, nil
	}
	var cached Result
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		return nil, "", fmt.Errorf("decode verification for %s: %w", hash, err)
	}
	return &cached, "", nil
}

func recordVerification(ctx context.Context, db *sql.DB, hash string, checked Result) error {
	raw, err := json.Marshal(checked)
	if err != nil {
		return fmt.Errorf("encode verification for %s: %w", hash, err)
	}
	return recordVerificationValue(ctx, db, hash, string(raw))
}

func recordVerificationValue(ctx context.Context, db *sql.DB, hash, value string) error {
	_, err := db.ExecContext(ctx, `INSERT OR REPLACE INTO semantic_verification (commit_hash, analyzer, language, runtime, result) VALUES (?, ?, ?, ?, ?)`, hash, AnalyzerVersion, languageVersion, runtime.Version(), value)
	if err != nil {
		return fmt.Errorf("record verification for %s: %w", hash, err)
	}
	return nil
}
