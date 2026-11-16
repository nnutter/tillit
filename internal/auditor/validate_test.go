package auditor_test

import (
	"encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/repository"
	"github.com/nnutter/spor/internal/smith"
)

func TestValidateChecksOutOutsideGitWorktrees(t *testing.T) {
	_, dir := linkedRepo(t)
	addChange(t, dir, "a.go", pureAfter)
	validatePassed(t, dir)

	checkout := sporCheckout(t, dir)
	require.NotContains(t, filepath.ToSlash(checkout), "/.git/worktrees/")
	require.Contains(t, filepath.ToSlash(readFile(t, filepath.Join(checkout, ".git"))), "/.git/worktrees/")
}

func TestValidateStopsAtTheFirstFailure(t *testing.T) {
	repo, dir := newRepo(t, map[string]string{
		"a.go":      pureBefore,
		"c.go":      pureBefore,
		"notes.txt": "keep\n",
	})
	stage(t, repo, dir, "a.go", pureAfter)
	stage(t, repo, dir, "c.go", pureAfter)
	stage(t, repo, dir, "b.go", "package b\n")
	script, logPath := failScript(t)
	writeConfig(t, dir, []string{"sh", script}, "")
	head := userHead(t, dir)
	files := map[string]string{
		"a.go":      pureAfter,
		"b.go":      "package b\n",
		"c.go":      pureAfter,
		"notes.txt": "keep\n",
	}

	added, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, added.Commits, 3)

	validated, err := auditor.Validate(t.Context(), dir, "")
	require.NoError(t, err)
	require.True(t, validated.Failed)
	require.Equal(t, config.ModeChange, validated.Mode)
	require.Equal(t, []auditor.Outcome{
		{Hash: added.Commits[0].Hash, Passed: true},
		{Hash: added.Commits[1].Hash, Passed: false},
	}, validated.Outcomes)
	require.Equal(t, []string{"a.go", "c.go"}, logLines(t, logPath))
	prefix, err := auditor.ValidatedPrefix(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, []string{added.Commits[0].Hash}, prefix)

	report, err := auditor.Report(t.Context(), dir)
	require.NoError(t, err)
	require.Contains(t, report, "mode: change")
	require.Contains(t, report, added.Commits[0].Hash[:12]+" passed")
	require.Contains(t, report, added.Commits[1].Hash[:12]+" failed")
	require.NotContains(t, report, added.Commits[2].Hash[:12])

	require.Equal(t, head, userHead(t, dir))
	requireFiles(t, dir, files)
	require.Equal(t, added.Commits[1].Hash, worktreeHead(t, dir))

	validated, err = auditor.Validate(t.Context(), dir, "")
	require.NoError(t, err)
	require.True(t, validated.Failed)
	require.Equal(t, []string{"a.go", "c.go", "c.go"}, logLines(t, logPath))
	require.Equal(t, head, userHead(t, dir))
	requireFiles(t, dir, files)
}

func TestValidateUsesCommitPatchAndChange(t *testing.T) {
	repo, dir := newRepo(t, map[string]string{
		"main.go":   extractBefore,
		"notes.txt": "keep\n",
	})
	stage(t, repo, dir, "main.go", extractAfter)
	script, logPath := logScript(t)
	writeConfig(t, dir, []string{"sh", script}, "")
	head := userHead(t, dir)

	added, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, added.Commits, 1)
	original := loadCommit(t, dir, added.Commits[0].Hash)
	change := trailer(t, original.Message, "Change-Id")

	first, err := auditor.Validate(t.Context(), dir, "")
	require.NoError(t, err)
	require.Equal(t, config.ModeChange, first.Mode)
	require.Equal(t, []string{added.Commits[0].Hash}, logLines(t, logPath))

	again, err := auditor.Validate(t.Context(), dir, "")
	require.NoError(t, err)
	require.False(t, again.Failed)
	require.Equal(t, []string{added.Commits[0].Hash}, logLines(t, logPath))

	stored := reopen(t, dir)
	sibling := writeCommit(t, stored, added.Ref, original.ParentHashes[0], original.TreeHash, commitMessage("other", "Sother", "Ibbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	patched, err := auditor.Validate(t.Context(), dir, config.ModePatch)
	require.NoError(t, err)
	require.Equal(t, config.ModePatch, patched.Mode)
	require.False(t, patched.Failed)
	require.Equal(t, []string{added.Commits[0].Hash}, logLines(t, logPath))

	byCommit, err := auditor.Validate(t.Context(), dir, config.ModeCommit)
	require.NoError(t, err)
	require.Equal(t, config.ModeCommit, byCommit.Mode)
	require.Equal(t, []string{added.Commits[0].Hash, sibling.String()}, logLines(t, logPath))

	tree := storeTree(t, stored, storeBlob(t, stored, []byte("package p\nfunc g() {}\n")))
	replaced := writeCommit(t, stored, added.Ref, original.ParentHashes[0], tree, commitMessage("third", "Sthird", change))
	skipped, err := auditor.Validate(t.Context(), dir, "")
	require.NoError(t, err)
	require.Equal(t, config.ModeChange, skipped.Mode)
	require.False(t, skipped.Failed)
	require.Equal(t, []string{added.Commits[0].Hash, sibling.String()}, logLines(t, logPath))

	report, err := auditor.Report(t.Context(), dir)
	require.NoError(t, err)
	require.Contains(t, report, "mode: change")
	require.Contains(t, report, replaced.String()[:12]+" passed")
	require.Equal(t, head, userHead(t, dir))
	require.Equal(t, extractAfter, readFile(t, filepath.Join(dir, "main.go")))
	require.Equal(t, "keep\n", readFile(t, filepath.Join(dir, "notes.txt")))
}

func TestValidateDoesNotTreatBlankChangeIDsAsTheSame(t *testing.T) {
	_, dir := newRepo(t, map[string]string{
		"main.go":   "package p\nfunc f() {}\n",
		"notes.txt": "keep\n",
	})
	script, logPath := logScript(t)
	writeConfig(t, dir, []string{"sh", script}, "")
	repo := reopen(t, dir)
	head, err := repo.Head()
	require.NoError(t, err)
	base, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)
	ref := repository.HeadRef(filepath.Join(dir, ".git"))
	firstTree := storeTree(t, repo, storeBlob(t, repo, []byte("package p\nfunc f() { return }\n")))
	first := writeCommit(t, repo, ref, base.Hash, firstTree, "one\n\nSpor-Id: Sone\n")
	secondTree := storeTree(t, repo, storeBlob(t, repo, []byte("package p\nfunc f() { return 1 }\n")))
	second := writeCommit(t, repo, ref, first, secondTree, "two\n\nSpor-Id: Stwo\n")

	validated, err := auditor.Validate(t.Context(), dir, config.ModeChange)
	require.NoError(t, err)
	require.False(t, validated.Failed)
	require.Equal(t, []string{first.String(), second.String()}, logLines(t, logPath))
}

func TestValidateDoesNotTreatBlankPatchIDsAsTheSame(t *testing.T) {
	_, dir := newRepo(t, map[string]string{
		"main.go":   "package p\nfunc f() {}\n",
		"notes.txt": "keep\n",
	})
	script, logPath := logScript(t)
	writeConfig(t, dir, []string{"sh", script}, "")
	repo := reopen(t, dir)
	head, err := repo.Head()
	require.NoError(t, err)
	base, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)
	ref := repository.HeadRef(filepath.Join(dir, ".git"))
	first := writeCommit(t, repo, ref, base.Hash, base.TreeHash, commitMessage("one", "Sone", "I1111111111111111111111111111111111111111"))
	second := writeCommit(t, repo, ref, first, base.TreeHash, commitMessage("two", "Stwo", "I2222222222222222222222222222222222222222"))

	validated, err := auditor.Validate(t.Context(), dir, config.ModePatch)
	require.NoError(t, err)
	require.False(t, validated.Failed)
	require.Equal(t, []string{first.String(), second.String()}, logLines(t, logPath))
}

func TestValidateRequiresACommand(t *testing.T) {
	_, dir := newRepo(t, map[string]string{"main.go": "package p\n"})

	_, err := auditor.Validate(t.Context(), dir, "")
	require.ErrorIs(t, err, auditor.ErrCommandNotConfigured)
	require.ErrorContains(t, err, "command")
}

func TestValidateRejectsAnUnknownMode(t *testing.T) {
	_, dir := newRepo(t, map[string]string{"main.go": "package p\n"})

	_, err := auditor.Validate(t.Context(), dir, "nope")
	require.ErrorContains(t, err, "mode")
	require.ErrorContains(t, err, "nope")
}

func TestValidateWithNothingToCheck(t *testing.T) {
	_, dir := newRepo(t, map[string]string{"main.go": "package p\n"})
	script, logPath := logScript(t)
	writeConfig(t, dir, []string{"sh", script}, "")

	validated, err := auditor.Validate(t.Context(), dir, "")
	require.NoError(t, err)
	require.Equal(t, config.ModeChange, validated.Mode)
	require.Empty(t, validated.Outcomes)
	require.False(t, validated.Failed)
	require.Empty(t, logLines(t, logPath))

	report, err := auditor.Report(t.Context(), dir)
	require.NoError(t, err)
	require.Empty(t, report)
}

func TestValidateReportsAMissingCommand(t *testing.T) {
	repo, dir := newRepo(t, map[string]string{
		"main.go":   extractBefore,
		"notes.txt": "keep\n",
	})
	stage(t, repo, dir, "main.go", extractAfter)
	writeConfig(t, dir, []string{"spor-no-such-validator"}, "")
	added, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.NotEmpty(t, added.Commits)

	_, err = auditor.Validate(t.Context(), dir, "")
	require.Error(t, err)
	require.ErrorContains(t, err, "spor-no-such-validator")
	require.NotErrorIs(t, err, auditor.ErrCommandNotConfigured)

	report, err := auditor.Report(t.Context(), dir)
	require.NoError(t, err)
	require.Empty(t, report)
}

func newRepo(t *testing.T, files map[string]string) (*git.Repository, string) {
	t.Helper()
	scrubGitEnv(t)
	isolateMise(t)
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	setUser(t, repo)
	worktree, err := repo.Worktree()
	require.NoError(t, err)
	for name, body := range files {
		writeFile(t, filepath.Join(dir, name), body)
		_, err = worktree.Add(name)
		require.NoError(t, err)
	}
	_, err = worktree.Commit("init", &git.CommitOptions{Author: signature()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	return repo, dir
}

func stage(t *testing.T, repo *git.Repository, dir, name, body string) {
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

func signature() *object.Signature {
	return &object.Signature{
		Name:  "Spor Test",
		Email: "spor@example.com",
		When:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func writeConfig(t *testing.T, dir string, command []string, mode string) {
	t.Helper()
	body, err := json.Marshal(validateDocument{Auditor: auditorDocument{Validate: validateSettings{Command: command, Mode: mode}}})
	require.NoError(t, err)
	path := filepath.Join(dir, ".git", "spor", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, body, 0o644))
}

type validateDocument struct {
	Auditor auditorDocument `json:"auditor"`
}

type auditorDocument struct {
	Validate validateSettings `json:"validate"`
	Fixup    fixupSettings    `json:"fixup"`
}

type fixupSettings struct {
	Model fixupModelSettings `json:"model"`
}

type fixupModelSettings struct {
	Provider string `json:"provider,omitempty"`
	Version  string `json:"version,omitempty"`
	Effort   string `json:"effort,omitempty"`
}

type validateSettings struct {
	Command []string `json:"command"`
	Mode    string   `json:"mode,omitempty"`
}

func logScript(t *testing.T) (string, string) {
	t.Helper()
	return writeScript(t, "#!/bin/sh\ngit rev-parse HEAD >> \"$VALIDATE_LOG\"\n")
}

func failScript(t *testing.T) (string, string) {
	t.Helper()
	return writeScript(t, `#!/bin/sh
names=$(git diff-tree --no-commit-id --name-only -r HEAD)
printf '%s\n' "$names" >> "$VALIDATE_LOG"
if [ "$names" = "c.go" ]; then
  exit 1
fi
exit 0
`)
}

func writeScript(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	script := filepath.Join(dir, "validate.sh")
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("VALIDATE_LOG", logPath)
	return script, logPath
}

func logLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func userHead(t *testing.T, dir string) string {
	t.Helper()
	head, err := reopen(t, dir).Head()
	require.NoError(t, err)
	return head.Hash().String()
}

func worktreeHead(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(runGit(t, sporCheckout(t, dir), "rev-parse", "HEAD"))
}

func sporCheckout(t *testing.T, dir string) string {
	t.Helper()
	dir, err := filepath.Abs(dir)
	require.NoError(t, err)
	paths := worktreePaths(t, dir)
	require.NotEmpty(t, paths)
	var extras []string
	for i, path := range paths {
		if i == 0 {
			continue
		}
		if path == dir {
			continue
		}
		extras = append(extras, path)
	}
	require.Len(t, extras, 1)
	return extras[0]
}

func worktreePaths(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	for line := range strings.SplitSeq(runGit(t, dir, "worktree", "list", "--porcelain"), "\n") {
		path, ok := strings.CutPrefix(line, "worktree ")
		if !ok {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func requireFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		require.Equal(t, body, readFile(t, filepath.Join(dir, name)))
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func reopen(t *testing.T, dir string) *git.Repository {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func loadCommit(t *testing.T, dir, hash string) *object.Commit {
	t.Helper()
	commit, err := reopen(t, dir).CommitObject(plumbing.NewHash(hash))
	require.NoError(t, err)
	return commit
}

func trailer(t *testing.T, message, key string) string {
	t.Helper()
	value := trailerValue(message, key)
	require.NotEmpty(t, value)
	return value
}

func trailerValue(message, key string) string {
	prefix := key + ": "
	for line := range strings.SplitSeq(message, "\n") {
		value, ok := strings.CutPrefix(line, prefix)
		if ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func commitMessage(subject, sporID, change string) string {
	return subject + "\n\nSpor-Id: " + sporID + "\nChange-Id: " + change + "\n"
}

func writeCommit(t *testing.T, repo *git.Repository, ref string, parent, tree plumbing.Hash, message string) plumbing.Hash {
	t.Helper()
	commit := &object.Commit{
		Author:       *signature(),
		Committer:    *signature(),
		Message:      message,
		TreeHash:     tree,
		ParentHashes: []plumbing.Hash{parent},
	}
	obj := repo.Storer.NewEncodedObject()
	require.NoError(t, commit.Encode(obj))
	hash, err := repo.Storer.SetEncodedObject(obj)
	require.NoError(t, err)
	require.NoError(t, repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(ref), hash)))
	return hash
}

func storeBlob(t *testing.T, repo *git.Repository, body []byte) plumbing.Hash {
	t.Helper()
	obj := repo.Storer.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	writer, err := obj.Writer()
	require.NoError(t, err)
	_, err = writer.Write(body)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	hash, err := repo.Storer.SetEncodedObject(obj)
	require.NoError(t, err)
	return hash
}

func storeTree(t *testing.T, repo *git.Repository, blob plumbing.Hash) plumbing.Hash {
	t.Helper()
	tree := &object.Tree{Entries: []object.TreeEntry{{
		Name: "main.go",
		Mode: filemode.Regular,
		Hash: blob,
	}}}
	obj := repo.Storer.NewEncodedObject()
	require.NoError(t, tree.Encode(obj))
	hash, err := repo.Storer.SetEncodedObject(obj)
	require.NoError(t, err)
	return hash
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = withoutGitEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

func withoutGitEnv(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if gitEnvKey(key) {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

func gitEnvKey(key string) bool {
	switch key {
	case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR":
		return true
	case "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES":
		return true
	case "GIT_PREFIX", "GIT_EXEC_PATH", "GIT_SEQUENCE_EDITOR":
		return true
	default:
		return false
	}
}

func scrubGitEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GIT_DIR",
		"GIT_WORK_TREE",
		"GIT_INDEX_FILE",
		"GIT_COMMON_DIR",
		"GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_PREFIX",
		"GIT_EXEC_PATH",
		"GIT_SEQUENCE_EDITOR",
	} {
		t.Setenv(key, "")
	}
}

const pureBefore = `package p
import "fmt"
func f() {
	x := 1
	fmt.Println(x)
}
`

const pureAfter = `package p
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
