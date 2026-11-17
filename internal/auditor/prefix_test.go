package auditor_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/repository"
)

func TestValidatedPrefixIncludesRepairedStacksOnly(t *testing.T) {
	_, dir := newRepo(t, map[string]string{"main.go": "package p\n"})
	repo := reopen(t, dir)
	head, err := repo.Head()
	require.NoError(t, err)
	base, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)
	gitDir := filepath.Join(dir, ".git")
	ref := repository.HeadRef(gitDir)
	makeCommit := func(parent, subject, change, fixup string) string {
		t.Helper()
		message := subject + "\n\nSpor-Id: S1\nChange-Id: " + change + "\n"
		if fixup != "" {
			message += "Fixup-Id: " + fixup + "\n"
		}
		return writeCommit(t, repo, ref, plumbing.NewHash(parent), base.TreeHash, message).String()
	}
	a := makeCommit(base.Hash.String(), "A", "IA", "")
	af := makeCommit(a, "A fixup", "IAF", "IA-1")
	af2 := makeCommit(af, "A second fixup", "IAF2", "IAF-2")
	b := makeCommit(af2, "B", "IB", "")
	c := makeCommit(b, "C", "IC", "")
	cf := makeCommit(c, "C fixup", "ICF", "IC-1")
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "auditor.validate.mode", []string{"commit"}))
	db, err := sql.Open("sqlite", "file:"+filepath.Join(gitDir, "spor", "state.db"))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`CREATE TABLE verification (commit_hash TEXT NOT NULL, patch_id TEXT NOT NULL, change_id TEXT NOT NULL, passed INTEGER NOT NULL, created TEXT NOT NULL)`)
	require.NoError(t, err)
	for _, item := range []struct {
		hash   string
		passed int
	}{{a, 0}, {af, 0}, {af2, 1}, {b, 1}, {c, 0}} {
		_, err := db.Exec(`INSERT INTO verification VALUES (?, '', '', ?, '1')`, item.hash, item.passed)
		require.NoError(t, err)
	}
	prefix, err := auditor.ValidatedPrefix(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, []string{a, af, af2, b}, prefix)
	var legacy int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'verification'`).Scan(&legacy))
	require.Zero(t, legacy)
	_, err = db.Exec(`INSERT INTO validation VALUES (?, '', '', 1, '2')`, cf)
	require.NoError(t, err)
	prefix, err = auditor.ValidatedPrefix(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, []string{a, af, af2, b, c, cf}, prefix)
}
