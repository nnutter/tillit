package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/cli"
)

func TestAddAndStatusPrintTheSubmission(t *testing.T) {
	dir := repoWithStagedRename(t)

	added := execute(t, dir, "add")
	require.Contains(t, added, "spor-id: S")
	require.Contains(t, added, "Refactor: Rename variable x to y in f")
	require.Contains(t, added, "Apply the remaining added changes")

	require.Equal(t, added, execute(t, dir, "status"))
}

func TestStatusWithNoSubmission(t *testing.T) {
	require.Equal(t, "No submissions.\n", execute(t, cleanRepo(t), "status"))
}

func TestAddRejectsAMissingRepository(t *testing.T) {
	scrubGitEnv(t)
	t.Setenv("HOME", t.TempDir())
	outside, err := os.MkdirTemp("/tmp", "spor-cli-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	root := cli.New("0.1.0")
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"add", "--repo", filepath.Join(outside, "missing")})
	require.Error(t, root.ExecuteContext(t.Context()))
}

func TestVersion(t *testing.T) {
	require.Equal(t, "0.1.0", cli.New("0.1.0").Version)
}

func TestAddAndStatusShowAPassingVerification(t *testing.T) {
	dir := repoWithStagedRename(t)
	writeConfig(t, dir, []string{"true"}, "")

	added := execute(t, dir, "add")
	require.Contains(t, added, "mode: change")
	require.Equal(t, 2, strings.Count(added, " passed\n"))
	require.NotContains(t, added, "failed")
	require.Equal(t, added, execute(t, dir, "status"))
}

func TestAddStopsVerificationAtTheFirstFailure(t *testing.T) {
	dir := repoWithStagedRename(t)
	script, logPath := failScript(t)
	writeConfig(t, dir, []string{"sh", script}, "")

	added, err := runCLI(t, dir, "add")
	require.ErrorContains(t, err, "verification")
	require.Equal(t, 1, strings.Count(added, " failed\n"))
	require.NotContains(t, added, " passed\n")
	require.Len(t, logLines(t, logPath), 1)
	require.Equal(t, added, execute(t, dir, "status"))
}

func TestVerifyModeOverridesTheConfiguredMode(t *testing.T) {
	dir := repoWithStagedRename(t)
	writeConfig(t, dir, []string{"true"}, "commit")
	execute(t, dir, "add")

	verified := execute(t, dir, "verify", "--mode", "patch")
	require.Contains(t, verified, "mode: patch")
	require.Contains(t, verified, " passed\n")
	require.Contains(t, execute(t, dir, "status"), "mode: commit")
}

func TestVerifyRejectsAnUnknownMode(t *testing.T) {
	dir := cleanRepo(t)

	_, err := runCLI(t, dir, "verify", "--mode", "nope")
	require.ErrorContains(t, err, "mode")
	require.ErrorContains(t, err, "nope")
}

func TestVerifyWithNothingToCheck(t *testing.T) {
	dir := cleanRepo(t)
	writeConfig(t, dir, []string{"true"}, "")

	require.Equal(t, "No commits to verify.\n", execute(t, dir, "verify"))
	require.Equal(t, "No staged changes.\n", execute(t, dir, "add"))
}

func TestVerifyRequiresACommand(t *testing.T) {
	_, err := runCLI(t, cleanRepo(t), "verify")
	require.ErrorContains(t, err, "command")
}

func TestStageIsAnAliasForAdd(t *testing.T) {
	dir := repoWithStagedRename(t)

	added := execute(t, dir, "add")
	require.Equal(t, added, execute(t, dir, "status"))

	other := repoWithStagedRename(t)
	staged := execute(t, other, "stage")
	require.Contains(t, staged, "spor-id: S")
	require.Contains(t, staged, "Refactor: Rename variable x to y in f")
	require.Contains(t, staged, "Apply the remaining added changes")
	require.Equal(t, staged, execute(t, other, "status"))
}

func TestSubmitIsNotFound(t *testing.T) {
	out, err := runCLI(t, cleanRepo(t), "submit")
	require.NoError(t, err)
	require.Contains(t, out, "Available Commands")
	require.Contains(t, out, "add")
	require.NotContains(t, out, "spor-id")
	require.NotContains(t, out, "No staged changes")
}

func TestResetUndoesLatestAdd(t *testing.T) {
	dir := repoWithStagedRename(t)

	first := execute(t, dir, "add")
	second := execute(t, dir, "add")
	require.NotEqual(t, first, second)

	reset := execute(t, dir, "reset")
	require.Equal(t, first, reset)
	require.Equal(t, first, execute(t, dir, "status"))
	require.NotContains(t, reset, second[:40])

	again := execute(t, dir, "reset")
	require.Equal(t, "No submissions.\n", again)
	require.Equal(t, "No submissions.\n", execute(t, dir, "status"))
	require.Equal(t, "No submissions.\n", execute(t, dir, "reset"))
}

func TestResetWithNoAdd(t *testing.T) {
	require.Equal(t, "No submissions.\n", execute(t, cleanRepo(t), "reset"))
}

func TestLogShowsNewestFirstWithUnverified(t *testing.T) {
	dir := repoWithStagedRename(t)
	execute(t, dir, "add")

	out := execute(t, dir, "log")
	require.Equal(t, 2, strings.Count(out, "(unverified)\n"))
	rename := strings.Index(out, "Refactor: Rename variable x to y in f")
	remainder := strings.Index(out, "Apply the remaining added changes")
	require.Greater(t, rename, remainder)
}

func TestLogWithNoCommits(t *testing.T) {
	require.Equal(t, "No commits to show.\n", execute(t, cleanRepo(t), "log"))
}

func TestLogShowsPassedAfterVerify(t *testing.T) {
	dir := repoWithStagedRename(t)
	writeConfig(t, dir, []string{"true"}, "")
	execute(t, dir, "add")

	out := execute(t, dir, "log")
	require.Equal(t, 2, strings.Count(out, "(passed)\n"))
	require.NotContains(t, out, "unverified")
}

func TestLogShowsFailedAfterVerify(t *testing.T) {
	dir := repoWithStagedRename(t)
	script, _ := failScript(t)
	writeConfig(t, dir, []string{"sh", script}, "")
	_, err := runCLI(t, dir, "add")
	require.ErrorContains(t, err, "verification")

	out := execute(t, dir, "log")
	require.Equal(t, 1, strings.Count(out, "(failed)\n"))
	require.Equal(t, 1, strings.Count(out, "(unverified)\n"))
}

func execute(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runCLI(t, dir, args...)
	require.NoError(t, err)
	return out
}

func runCLI(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	root := cli.New("0.1.0")
	// Match Fang, which silences Cobra's usage and error text.
	root.SilenceUsage = true
	root.SilenceErrors = true
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	// Keep --repo ahead of the subcommand so a later value may start with '-'.
	root.SetArgs(append([]string{"--repo", dir}, args...))
	err := root.ExecuteContext(t.Context())
	return out.String(), err
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
	t.Setenv("HOME", t.TempDir())
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

func writeConfig(t *testing.T, dir string, command []string, mode string) {
	t.Helper()
	body, err := json.Marshal(verifyDocument{Verify: verifySettings{Command: command, Mode: mode}})
	require.NoError(t, err)
	path := filepath.Join(dir, ".git", "spor", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, body, 0o644))
}

type verifyDocument struct {
	Verify verifySettings `json:"verify"`
}

type verifySettings struct {
	Command []string `json:"command"`
	Mode    string   `json:"mode,omitempty"`
}

func failScript(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	script := filepath.Join(dir, "verify.sh")
	body := "#!/bin/sh\ngit rev-parse HEAD >> \"$VERIFY_LOG\"\nexit 1\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("VERIFY_LOG", logPath)
	return script, logPath
}

func logLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
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
