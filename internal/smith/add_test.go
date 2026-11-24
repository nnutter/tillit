package smith_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/smith"
	"github.com/nnutter/spor/internal/testutil"
)

func TestAddRenameLeavesTheUserRepoAndWritesTwoCommits(t *testing.T) {
	repo, dir := newRepo(t, "main.go", []byte(renameBefore))
	stage(t, repo, dir, "main.go", []byte(renameAfter))
	keep := readFile(t, filepath.Join(dir, "notes.txt"))
	head, index := snapshot(t, repo)

	result, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)

	require.NotEmpty(t, result.SporID)
	require.True(t, strings.HasPrefix(result.SporID, "S"))
	require.Len(t, result.Commits, 2)
	require.Equal(t, "Refactor: Rename variable x to y in f", result.Commits[0].Subject)
	require.Equal(t, "Apply the remaining added changes", result.Commits[1].Subject)
	require.Contains(t, smith.Format(result), "spor-id: "+result.SporID)
	require.Contains(t, smith.Format(result), result.Ref)

	again := reopen(t, dir)
	require.Equal(t, head, headHash(t, again))
	require.Equal(t, index, indexSnapshot(t, again))
	require.Equal(t, keep, readFile(t, filepath.Join(dir, "notes.txt")))
	require.Equal(t, []byte(renameAfter), readFile(t, filepath.Join(dir, "main.go")))

	commits := loadCommits(t, again, result.Ref)
	require.Len(t, commits, 2)
	require.Equal(t, head, commits[0].ParentHashes[0].String())
	require.Equal(t, commits[0].Hash.String(), commits[1].ParentHashes[0].String())
	require.Equal(t, renamePeeled, fileText(t, commits[0], "main.go"))
	require.Equal(t, renameAfter, fileText(t, commits[1], "main.go"))
	require.Equal(t, "keep\n", fileText(t, commits[1], "notes.txt"))
	require.Contains(t, commits[0].Message, "Spor-Id: "+result.SporID)
	require.Contains(t, commits[1].Message, "Spor-Id: "+result.SporID)
	require.Contains(t, commits[0].Message, "Change-Id: I")
	require.NotEqual(t, changeID(commits[0].Message), changeID(commits[1].Message))

	idRef := strings.TrimSuffix(result.Ref, "/head") + "/" + result.SporID
	ref, err := again.Reference(plumbing.ReferenceName(idRef), true)
	require.NoError(t, err)
	require.Equal(t, commits[1].Hash, ref.Hash())

	status, err := smith.Status(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, result.SporID, status.SporID)
	require.Equal(t, result.Commits, status.Commits)
	_, err = os.Stat(filepath.Join(dir, ".git", "spor", "state.db"))
	require.NoError(t, err)
}

func TestAddExtractWritesOneCommit(t *testing.T) {
	repo, dir := newRepo(t, "main.go", []byte(extractBefore))
	stage(t, repo, dir, "main.go", []byte(extractAfter))

	result, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, result.Commits, 1)
	require.Equal(t, "Refactor: Extract variable sum in f", result.Commits[0].Subject)

	again := reopen(t, dir)
	commits := loadCommits(t, again, result.Ref)
	require.Len(t, commits, 1)
	require.Equal(t, extractAfter, fileText(t, commits[0], "main.go"))
}

func TestAddWithoutARefactorDoesNotMoveTheRef(t *testing.T) {
	repo, dir := newRepo(t, "main.go", []byte(renameBefore))
	stage(t, repo, dir, "main.go", []byte(strings.ReplaceAll(renameBefore, "1", "2")))

	result, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.Empty(t, result.Commits)
	require.Equal(t, "No basic refactors in the staged changes.", result.Message)
	text := smith.Format(result)
	require.Contains(t, text, result.SporID)
	require.NotContains(t, text, "ref:")

	requireNoSporRef(t, dir)

	status, err := smith.Status(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, result.SporID, status.SporID)
	require.Equal(t, result.Message, status.Message)
}

func TestAddWithNoStagedChanges(t *testing.T) {
	_, dir := newRepo(t, "main.go", []byte(renameBefore))

	result, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, "No staged changes.\n", smith.Format(result))

	status, err := smith.Status(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, "No submissions.\n", smith.Format(status))
}

func TestResetWithNoSubmissions(t *testing.T) {
	_, dir := newRepo(t, "main.go", []byte(renameBefore))

	result, err := smith.Reset(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, "No submissions.\n", smith.Format(result))
}

func TestResetUndoesLatestAdd(t *testing.T) {
	repo, dir := newRepo(t, "main.go", []byte(renameBefore))
	stage(t, repo, dir, "main.go", []byte(renameAfter))

	first, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, first.Commits, 2)

	second, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.NotEqual(t, first.Commits[0].Hash, second.Commits[0].Hash)

	undone, err := smith.Reset(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, first.SporID, undone.SporID)
	require.Equal(t, first.Commits, undone.Commits)
	require.Equal(t, first.Ref, undone.Ref)

	again := reopen(t, dir)
	require.Equal(t, first.Commits[1].Hash, referenceHash(t, again, first.Ref))
	_, err = again.Reference(plumbing.ReferenceName(sporIDRef(second)), true)
	require.ErrorIs(t, err, plumbing.ErrReferenceNotFound)

	status, err := smith.Status(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, first.SporID, status.SporID)

	empty, err := smith.Reset(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, "No submissions.\n", smith.Format(empty))
	again = reopen(t, dir)
	_, err = again.Reference(plumbing.ReferenceName(first.Ref), true)
	require.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
	_, err = again.Reference(plumbing.ReferenceName(sporIDRef(first)), true)
	require.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
}

func TestLogReturnsNewestFirst(t *testing.T) {
	repo, dir := newRepo(t, "main.go", []byte(renameBefore))
	stage(t, repo, dir, "main.go", []byte(renameAfter))

	added, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, added.Commits, 2)

	commits, err := smith.Log(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, commits, 2)
	require.Equal(t, added.Commits[1], commits[0])
	require.Equal(t, added.Commits[0], commits[1])
}

func TestLogWithNoCommits(t *testing.T) {
	_, dir := newRepo(t, "main.go", []byte(renameBefore))

	commits, err := smith.Log(t.Context(), dir)
	require.NoError(t, err)
	require.Empty(t, commits)
}

func TestLogHidesCommitsOnBranch(t *testing.T) {
	repo, dir := newRepo(t, "main.go", []byte(renameBefore))
	stage(t, repo, dir, "main.go", []byte(renameAfter))

	added, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, added.Commits, 2)

	again := reopen(t, dir)
	head, err := again.Head()
	require.NoError(t, err)
	require.NoError(t, again.Storer.SetReference(plumbing.NewHashReference(head.Name(), plumbing.NewHash(added.Commits[1].Hash))))

	commits, err := smith.Log(t.Context(), dir)
	require.NoError(t, err)
	require.Empty(t, commits)
}

func TestAddRequiresAGitUser(t *testing.T) {
	cases := []struct {
		label   string
		name    string
		email   string
		keyword string
	}{
		{label: "both unset", keyword: "user.name"},
		{label: "email unset", name: "Spor Test", keyword: "user.email"},
		{label: "name unset", email: "spor@example.com", keyword: "user.name"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			repo, dir := newRepo(t, "main.go", []byte(extractBefore))
			cfg := emptyUserConfig(t, repo)
			cfg.User.Name = tc.name
			cfg.User.Email = tc.email
			require.NoError(t, repo.SetConfig(cfg))
			stage(t, repo, dir, "main.go", []byte(extractAfter))

			_, err := smith.Add(t.Context(), dir)
			require.ErrorContains(t, err, tc.keyword)
		})
	}
}

func TestAddRequiresACommit(t *testing.T) {
	dir := t.TempDir()
	testutil.IsolateGit(t)
	_, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	_, err = smith.Add(t.Context(), dir)
	require.ErrorContains(t, err, "commit")
}

func TestAddRejectsAMissingRepository(t *testing.T) {
	testutil.IsolateGit(t)
	dir, err := os.MkdirTemp("/tmp", "spor-smith-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	_, err = smith.Add(t.Context(), dir)
	require.ErrorContains(t, err, "repository")
}

func TestAddAgainStartsFromTheUserHead(t *testing.T) {
	repo, dir := newRepo(t, "main.go", []byte(extractBefore))
	stage(t, repo, dir, "main.go", []byte(extractAfter))
	head, index := snapshot(t, repo)

	first, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, first.Commits, 1)

	second, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, second.Commits, 1)
	require.NotEqual(t, first.SporID, second.SporID)
	require.Equal(t, first.Ref, second.Ref)
	require.Equal(t, first.Commits[0].Subject, second.Commits[0].Subject)
	require.NotEqual(t, first.Commits[0].Hash, second.Commits[0].Hash)

	requireUntouched(t, dir, head, index, map[string][]byte{
		"main.go":   []byte(extractAfter),
		"notes.txt": []byte("keep\n"),
	})

	again := reopen(t, dir)
	require.Equal(t, first.Commits[0].Hash, referenceHash(t, again, sporIDRef(first)))
	require.Equal(t, second.Commits[0].Hash, referenceHash(t, again, second.Ref))
	tip, err := again.CommitObject(plumbing.NewHash(second.Commits[0].Hash))
	require.NoError(t, err)
	require.Equal(t, head, tip.ParentHashes[0].String())

	status, err := smith.Status(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, second.SporID, status.SporID)
	require.Equal(t, second.Commits, status.Commits)
	require.NotContains(t, smith.Format(status), first.SporID)
}

func TestAddRenamesANestedFile(t *testing.T) {
	after := []byte(strings.ReplaceAll(renameBefore, "x", "y"))
	repo, dir := newRepo(t, "pkg/main.go", []byte(renameBefore))
	stage(t, repo, dir, "pkg/main.go", after)
	head, index := snapshot(t, repo)

	result, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, result.Commits, 1)
	require.Contains(t, result.Commits[0].Subject, "x")
	require.Contains(t, result.Commits[0].Subject, "y")

	requireUntouched(t, dir, head, index, map[string][]byte{
		"pkg/main.go": after,
		"notes.txt":   []byte("keep\n"),
	})
	again := reopen(t, dir)
	commits := loadCommits(t, again, result.Ref)
	require.Len(t, commits, 1)
	require.Equal(t, string(after), fileText(t, commits[0], "pkg/main.go"))
	require.Equal(t, "keep\n", fileText(t, commits[0], "notes.txt"))
	require.Equal(t, "Spor Test", commits[0].Author.Name)
	require.Equal(t, "spor@example.com", commits[0].Author.Email)
}

func TestAddFromASubdirectory(t *testing.T) {
	repo, dir := newRepo(t, "main.go", []byte(extractBefore))
	stage(t, repo, dir, "main.go", []byte(extractAfter))
	head, index := snapshot(t, repo)
	sub := filepath.Join(dir, "pkg")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	result, err := smith.Add(t.Context(), sub)
	require.NoError(t, err)
	require.Len(t, result.Commits, 1)
	requireUntouched(t, dir, head, index, map[string][]byte{
		"main.go":   []byte(extractAfter),
		"notes.txt": []byte("keep\n"),
	})

	_, err = os.Stat(filepath.Join(dir, ".git", "spor", "state.db"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(sub, ".git"))
	require.ErrorIs(t, err, os.ErrNotExist)

	status, err := smith.Status(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, result.SporID, status.SporID)
	require.Equal(t, result.Commits, status.Commits)
}

func TestAddKeepsAddedAndDeletedFilesOutOfTheRefactorCommit(t *testing.T) {
	repo, dir := newRepo(t, "main.go", []byte(renameBefore))
	commitPath(t, repo, dir, "gone.txt", "gone\n")
	stage(t, repo, dir, "main.go", []byte(renameAfter))
	removePath(t, repo, "gone.txt")
	stage(t, repo, dir, "new.txt", []byte("new\n"))
	head, index := snapshot(t, repo)

	result, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, result.Commits, 2)
	require.Equal(t, "Apply the remaining added changes", result.Commits[1].Subject)

	requireUntouched(t, dir, head, index, map[string][]byte{
		"main.go":   []byte(renameAfter),
		"notes.txt": []byte("keep\n"),
		"new.txt":   []byte("new\n"),
	})
	_, err = os.Stat(filepath.Join(dir, "gone.txt"))
	require.ErrorIs(t, err, os.ErrNotExist)

	again := reopen(t, dir)
	commits := loadCommits(t, again, result.Ref)
	require.Len(t, commits, 2)
	require.Equal(t, renamePeeled, fileText(t, commits[0], "main.go"))
	require.Equal(t, "gone\n", fileText(t, commits[0], "gone.txt"))
	require.Equal(t, []string{"gone.txt", "main.go", "notes.txt"}, commitPaths(t, commits[0]))
	require.Equal(t, renameAfter, fileText(t, commits[1], "main.go"))
	require.Equal(t, "new\n", fileText(t, commits[1], "new.txt"))
	require.Equal(t, []string{"main.go", "new.txt", "notes.txt"}, commitPaths(t, commits[1]))
}

func TestAddRejectsUnparseableGo(t *testing.T) {
	repo, dir := newRepo(t, "main.go", []byte(renameBefore))
	broken := []byte("package p\nfunc (\n")
	stage(t, repo, dir, "main.go", broken)
	head, index := snapshot(t, repo)

	_, err := smith.Add(t.Context(), dir)
	require.ErrorContains(t, err, "main.go")
	require.ErrorContains(t, err, "expected")
	requireUntouched(t, dir, head, index, map[string][]byte{
		"main.go":   broken,
		"notes.txt": []byte("keep\n"),
	})
	requireNoSporRef(t, dir)

	status, err := smith.Status(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, "No submissions.\n", smith.Format(status))
}

func TestAddRejectsAConflictedIndex(t *testing.T) {
	repo, dir := newRepo(t, "main.go", []byte(renameBefore))
	stage(t, repo, dir, "main.go", []byte(renameAfter))
	markConflict(t, repo, "main.go")
	head, index := snapshot(t, repo)

	_, err := smith.Add(t.Context(), dir)
	require.ErrorContains(t, err, "conflict")
	require.ErrorContains(t, err, "main.go")
	requireUntouched(t, dir, head, index, map[string][]byte{
		"main.go":   []byte(renameAfter),
		"notes.txt": []byte("keep\n"),
	})
	requireNoSporRef(t, dir)

	status, err := smith.Status(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, "No submissions.\n", smith.Format(status))
}

func TestAddInALinkedWorktreeKeepsStateThere(t *testing.T) {
	testutil.IsolateGit(t)
	base := t.TempDir()
	mainDir := filepath.Join(base, "main")
	linkedDir := filepath.Join(base, "linked")
	runGit(t, "init", mainDir)
	runGit(t, "-C", mainDir, "config", "user.name", "Spor Test")
	runGit(t, "-C", mainDir, "config", "user.email", "spor@example.com")
	writeFile(t, filepath.Join(mainDir, "main.go"), []byte(extractBefore))
	runGit(t, "-C", mainDir, "add", "main.go")
	runGit(t, "-C", mainDir, "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "-m", "init")
	runGit(t, "-C", mainDir, "worktree", "add", "--detach", linkedDir)

	mainHead := runGit(t, "-C", mainDir, "rev-parse", "HEAD")
	linkedHead := runGit(t, "-C", linkedDir, "rev-parse", "HEAD")
	mainBefore := readFile(t, filepath.Join(mainDir, "main.go"))
	writeFile(t, filepath.Join(linkedDir, "main.go"), []byte(extractAfter))
	runGit(t, "-C", linkedDir, "add", "main.go")

	result, err := smith.Add(t.Context(), linkedDir)
	require.NoError(t, err)
	require.Len(t, result.Commits, 1)

	gitDir := linkedGitDir(t, linkedDir)
	_, err = os.Stat(filepath.Join(gitDir, "spor", "state.db"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(mainDir, ".git", "spor", "state.db"))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Equal(t, mainBefore, readFile(t, filepath.Join(mainDir, "main.go")))
	require.Equal(t, []byte(extractAfter), readFile(t, filepath.Join(linkedDir, "main.go")))
	require.Equal(t, mainHead, runGit(t, "-C", mainDir, "rev-parse", "HEAD"))
	require.Equal(t, linkedHead, runGit(t, "-C", linkedDir, "rev-parse", "HEAD"))

	status, err := smith.Status(t.Context(), linkedDir)
	require.NoError(t, err)
	require.Equal(t, result.SporID, status.SporID)
	require.Equal(t, result.Commits, status.Commits)

	mainStatus, err := smith.Status(t.Context(), mainDir)
	require.NoError(t, err)
	require.Equal(t, "No submissions.\n", smith.Format(mainStatus))
}

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

const extractBefore = `package p
import "fmt"
func f(a, b int) {
	fmt.Println(a + b)
}
`

const extractAfter = `package p
import "fmt"
func f(a, b int) {
	sum := a + b
	fmt.Println(sum)
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

func emptyUserConfig(t *testing.T, repo *git.Repository) *config.Config {
	t.Helper()
	cfg, err := repo.Config()
	require.NoError(t, err)
	cfg.User.Name = ""
	cfg.User.Email = ""
	cfg.Raw = nil
	return cfg
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

func reopen(t *testing.T, dir string) *git.Repository {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func loadCommits(t *testing.T, repo *git.Repository, refName string) []*object.Commit {
	t.Helper()
	ref, err := repo.Reference(plumbing.ReferenceName(refName), true)
	require.NoError(t, err)
	var commits []*object.Commit
	hash := ref.Hash()
	for {
		commit, err := repo.CommitObject(hash)
		require.NoError(t, err)
		commits = append(commits, commit)
		if len(commit.ParentHashes) == 0 {
			break
		}
		parent := commit.ParentHashes[0]
		head, err := repo.Head()
		require.NoError(t, err)
		if parent == head.Hash() {
			break
		}
		hash = parent
	}
	slices.Reverse(commits)
	return commits
}

func eachRef(repo *git.Repository, visit func(*plumbing.Reference) error) error {
	refs, err := repo.References()
	if err != nil {
		return err
	}
	return refs.ForEach(visit)
}

func fileText(t *testing.T, commit *object.Commit, name string) string {
	t.Helper()
	file, err := commit.File(name)
	require.NoError(t, err)
	text, err := file.Contents()
	require.NoError(t, err)
	return text
}

func changeID(message string) string {
	_, id, _ := strings.Cut(message, "Change-Id: ")
	id, _, _ = strings.Cut(id, "\n")
	return id
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return body
}

func writeFile(t *testing.T, path string, body []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, body, 0o644))
}

func requireUntouched(t *testing.T, dir, head string, index []indexEntry, files map[string][]byte) {
	t.Helper()
	again := reopen(t, dir)
	require.Equal(t, head, headHash(t, again))
	require.Equal(t, index, indexSnapshot(t, again))
	for name, body := range files {
		require.Equal(t, body, readFile(t, filepath.Join(dir, name)))
	}
}

func requireNoSporRef(t *testing.T, dir string) {
	t.Helper()
	again := reopen(t, dir)
	require.NoError(t, eachRef(again, func(ref *plumbing.Reference) error {
		if strings.HasPrefix(ref.Name().String(), "refs/spor/") {
			return errors.New("spor ref was created")
		}
		return nil
	}))
}

func sporIDRef(result smith.Result) string {
	return strings.TrimSuffix(result.Ref, "/head") + "/" + result.SporID
}

func referenceHash(t *testing.T, repo *git.Repository, name string) string {
	t.Helper()
	ref, err := repo.Reference(plumbing.ReferenceName(name), true)
	require.NoError(t, err)
	return ref.Hash().String()
}

func commitPath(t *testing.T, repo *git.Repository, dir, name, body string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, name), []byte(body))
	worktree, err := repo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add(name)
	require.NoError(t, err)
	_, err = worktree.Commit("add "+name, &git.CommitOptions{Author: &object.Signature{
		Name:  "Spor Test",
		Email: "spor@example.com",
		When:  time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC),
	}})
	require.NoError(t, err)
}

func removePath(t *testing.T, repo *git.Repository, name string) {
	t.Helper()
	worktree, err := repo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Remove(name)
	require.NoError(t, err)
}

func markConflict(t *testing.T, repo *git.Repository, name string) {
	t.Helper()
	idx, err := repo.Storer.Index()
	require.NoError(t, err)
	var found bool
	for _, entry := range idx.Entries {
		if entry.Name != name {
			continue
		}
		extra := *entry
		extra.Stage = 2
		idx.Entries = append(idx.Entries, &extra)
		found = true
		break
	}
	require.True(t, found)
	require.NoError(t, repo.Storer.SetIndex(idx))
}

func commitPaths(t *testing.T, commit *object.Commit) []string {
	t.Helper()
	files, err := commit.Files()
	require.NoError(t, err)
	var names []string
	require.NoError(t, files.ForEach(func(file *object.File) error {
		names = append(names, file.Name)
		return nil
	}))
	slices.Sort(names)
	return names
}

func linkedGitDir(t *testing.T, dir string) string {
	t.Helper()
	line := strings.TrimSpace(string(readFile(t, filepath.Join(dir, ".git"))))
	gitDir, ok := strings.CutPrefix(line, "gitdir: ")
	require.True(t, ok)
	return strings.TrimSpace(gitDir)
}

func runGit(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}
