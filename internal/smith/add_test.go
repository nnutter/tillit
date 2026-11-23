package smith_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/testutil"
)

const renameBefore = `package p
import "fmt"
func f() {
	x := 1
	fmt.Println(x)
}
`

const renameAfter = `package p
import "fmt"
func f() {
	y := 1
	fmt.Println(y)
	fmt.Println("extra")
}
`

const renamePeeled = `package p
import "fmt"
func f() {
	y := 1
	fmt.Println(y)
}
`

func newRepo(t *testing.T, name string, body []byte) (*git.Repository, string) {
	t.Helper()
	testutil.IsolateGit(t)
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	setUser(t, repo)
	writeFile(t, filepath.Join(dir, name), body)
	writeFile(t, filepath.Join(dir, "notes.txt"), []byte("keep\n"))
	worktree, err := repo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add(name)
	require.NoError(t, err)
	_, err = worktree.Add("notes.txt")
	require.NoError(t, err)
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	_, err = worktree.Commit("init", &git.CommitOptions{Author: &object.Signature{
		Name:  "Spor Test",
		Email: "spor@example.com",
		When:  when,
	}})
	require.NoError(t, err)
	return repo, dir
}

func stage(t *testing.T, repo *git.Repository, dir, name string, body []byte) {
	t.Helper()
	writeFile(t, filepath.Join(dir, name), body)
	worktree, err := repo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add(name)
	require.NoError(t, err)
}

func setUser(t *testing.T, repo *git.Repository) {
	t.Helper()
	cfg, err := repo.Config()
	require.NoError(t, err)
	cfg.User.Name = "Spor Test"
	cfg.User.Email = "spor@example.com"
	require.NoError(t, repo.SetConfig(cfg))
}

func snapshot(t *testing.T, repo *git.Repository) (string, []indexEntry) {
	t.Helper()
	return headHash(t, repo), indexSnapshot(t, repo)
}

func headHash(t *testing.T, repo *git.Repository) string {
	t.Helper()
	head, err := repo.Head()
	require.NoError(t, err)
	return head.Hash().String()
}

type indexEntry struct {
	name  string
	mode  uint32
	hash  string
	stage int
}

func indexSnapshot(t *testing.T, repo *git.Repository) []indexEntry {
	t.Helper()
	idx, err := repo.Storer.Index()
	require.NoError(t, err)
	out := make([]indexEntry, len(idx.Entries))
	for i, entry := range idx.Entries {
		out[i] = indexEntry{
			name:  entry.Name,
			mode:  uint32(entry.Mode),
			hash:  entry.Hash.String(),
			stage: int(entry.Stage),
		}
	}
	return out
}

func fileText(t *testing.T, commit *object.Commit, name string) string {
	t.Helper()
	file, err := commit.File(name)
	require.NoError(t, err)
	text, err := file.Contents()
	require.NoError(t, err)
	return text
}

func writeFile(t *testing.T, path string, body []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, body, 0o644))
}

func runGit(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}
