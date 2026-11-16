package verifier_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/nnutter/spor/internal/verifier"
)

func TestBranchVerificationRetriesReportedError(t *testing.T) {
	repo := t.TempDir()
	gitTest(t, repo, "init", "-q")
	writeTestFile(t, repo, "a.go", "package p\nfunc f() int { var x = 1; return x }\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-qm", "base")
	gitTest(t, repo, "branch", "base")
	gitTest(t, repo, "config", "push.default", "current")
	branch := gitTest(t, repo, "branch", "--show-current")
	gitTest(t, repo, "config", "branch."+branch+".remote", ".")
	gitTest(t, repo, "config", "branch."+branch+".merge", "refs/heads/base")
	writeTestFile(t, repo, "a.go", "package p\nfunc f() int { var value = 1; return value }\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-qm", "rename")
	blob := gitTest(t, repo, "rev-parse", "HEAD:a.go")
	object := filepath.Join(repo, ".git", "objects", blob[:2], blob[2:])
	hidden := filepath.Join(t.TempDir(), "blob")
	require.NoError(t, os.Rename(object, hidden))

	first, err := verifier.VerifyBranch(t.Context(), repo)
	require.NoError(t, err)
	require.Len(t, first.Outcomes, 1)
	require.Nil(t, first.Outcomes[0].Result)
	require.Contains(t, first.Outcomes[0].Error, "git command failed")
	report, err := verifier.ReportBranch(t.Context(), repo)
	require.NoError(t, err)
	require.Equal(t, first, report)

	require.NoError(t, os.Rename(hidden, object))
	retried, err := verifier.VerifyBranch(t.Context(), repo)
	require.NoError(t, err)
	require.Empty(t, retried.Outcomes[0].Error)
	require.Equal(t, verifier.Verified, retried.Outcomes[0].Result.Status)
	report, err = verifier.ReportBranch(t.Context(), repo)
	require.NoError(t, err)
	require.Equal(t, retried, report)
}

func TestBranchVerificationPersistence(t *testing.T) {
	repo := t.TempDir()
	gitTest(t, repo, "init", "-q")
	writeTestFile(t, repo, "a.go", "package p\nfunc f() int { var x = 1; return x }\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-qm", "base")
	gitTest(t, repo, "branch", "base")
	gitTest(t, repo, "config", "push.default", "current")
	branch := gitTest(t, repo, "branch", "--show-current")
	gitTest(t, repo, "config", "branch."+branch+".remote", ".")
	gitTest(t, repo, "config", "branch."+branch+".merge", "refs/heads/base")
	writeTestFile(t, repo, "a.go", "package p\nfunc f() int { var value = 1; return value }\n")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-qm", "rename")
	writeTestFile(t, repo, "note", "change")
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-qm", "unsupported")
	path := filepath.Join(repo, ".git", "spor", "state.db")
	before, err := verifier.ReportBranch(t.Context(), repo)
	require.NoError(t, err)
	require.Len(t, before.Outcomes, 2)
	require.Nil(t, before.Outcomes[0].Result)
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	// An old validation table used this name; semantic results must not touch it.
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	legacy, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = legacy.ExecContext(t.Context(), `CREATE TABLE verification (commit_hash TEXT NOT NULL, patch_id TEXT NOT NULL, change_id TEXT NOT NULL, passed INTEGER NOT NULL, created TEXT NOT NULL); INSERT INTO verification VALUES ('legacy', '', '', 1, 'then')`)
	require.NoError(t, err)
	require.NoError(t, legacy.Close())
	first, err := verifier.VerifyBranch(t.Context(), repo)
	require.NoError(t, err)
	require.Len(t, first.Outcomes, 2)
	require.Equal(t, verifier.Verified, first.Outcomes[0].Result.Status)
	require.Equal(t, verifier.Unsupported, first.Outcomes[1].Result.Status)
	report, err := verifier.ReportBranch(t.Context(), repo)
	require.NoError(t, err)
	require.Equal(t, first, report)
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var legacyCount int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM verification WHERE passed=1`).Scan(&legacyCount))
	require.Equal(t, 1, legacyCount)
	_, err = db.ExecContext(t.Context(), `UPDATE semantic_verification SET analyzer='old' WHERE commit_hash=?`, first.Outcomes[0].Hash)
	require.NoError(t, err)
	report, err = verifier.ReportBranch(t.Context(), repo)
	require.NoError(t, err)
	require.Nil(t, report.Outcomes[0].Result)
	require.Equal(t, verifier.Unsupported, report.Outcomes[1].Result.Status)
	again, err := verifier.VerifyBranch(t.Context(), repo)
	require.NoError(t, err)
	require.Equal(t, first, again)
}
