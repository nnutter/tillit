package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/cli"
)

func TestSubmitAndStatusPrintTheSubmission(t *testing.T) {
	dir := repoWithStagedRename(t)

	submitted := execute(t, dir, "submit")
	require.Contains(t, submitted, "spor-id: S")
	require.Contains(t, submitted, "Refactor: Rename variable x to y in f")
	require.Contains(t, submitted, "Apply the remaining submitted changes")

	require.Equal(t, submitted, execute(t, dir, "status"))
}

func TestStatusWithNoSubmission(t *testing.T) {
	require.Equal(t, "No submissions.\n", execute(t, cleanRepo(t), "status"))
}

func TestSubmitRejectsAMissingRepository(t *testing.T) {
	scrubGitEnv(t)
	outside, err := os.MkdirTemp("/tmp", "spor-cli-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	root := cli.New("0.1.0")
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"submit", "--repo", filepath.Join(outside, "missing")})
	require.Error(t, root.ExecuteContext(t.Context()))
}

func TestVersion(t *testing.T) {
	require.Equal(t, "0.1.0", cli.New("0.1.0").Version)
}

func execute(t *testing.T, dir, command string) string {
	t.Helper()
	root := cli.New("0.1.0")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{command, "--repo", dir})
	require.NoError(t, root.ExecuteContext(t.Context()))
	return out.String()
}

func cleanRepo(t *testing.T) string {
	t.Helper()
	return initRepo(t, []byte(before), []byte(before))
}

func repoWithStagedRename(t *testing.T) string {
	t.Helper()
	return initRepo(t, []byte(before), []byte(after))
}

func initRepo(t *testing.T, committed, staged []byte) string {
	t.Helper()
	scrubGitEnv(t)
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	cfg, err := repo.Config()
	require.NoError(t, err)
	cfg.User.Name = "Spor Test"
	cfg.User.Email = "spor@example.com"
	require.NoError(t, repo.SetConfig(cfg))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), committed, 0o644))
	worktree, err := repo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add("main.go")
	require.NoError(t, err)
	_, err = worktree.Commit("init", &git.CommitOptions{Author: &object.Signature{
		Name:  "Spor Test",
		Email: "spor@example.com",
		When:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}})
	require.NoError(t, err)
	if !bytes.Equal(committed, staged) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), staged, 0o644))
		_, err = worktree.Add("main.go")
		require.NoError(t, err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return dir
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

const before = `package p
import "fmt"
func f() {
	x := 1
	fmt.Println(x)
}
`

const after = `package p
import "fmt"
func f() {
	y := 1
	fmt.Println(y)
	fmt.Println("extra")
}
`
