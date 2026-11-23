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
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/cli"
	"github.com/nnutter/spor/internal/daemon"
	"github.com/nnutter/spor/internal/testutil"
)

func TestStatusWithNoSnapshot(t *testing.T) {
	require.Equal(t, "watcher: stopped\nsnapshot: none\n", execute(t, cleanRepo(t), "status"))
}

func TestAddRejectsAMissingRepository(t *testing.T) {
	testutil.IsolateGit(t)
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

func TestAddStopsValidationAtTheFirstFailure(t *testing.T) {
	dir := repoWithStagedRename(t)
	script, logPath := failScript(t)
	writeConfig(t, dir, []string{"sh", script}, "")

	require.Equal(t, "Queued.\n", execute(t, dir, "add"))
	drain(t, dir)
	status := execute(t, dir, "log")
	require.Equal(t, 1, strings.Count(status, "(failed)\n"))
	require.NotContains(t, status, "(passed)\n")
	require.Len(t, logLines(t, logPath), 1)
}

func TestValidateIsTheCanonicalCommand(t *testing.T) {
	root := cli.New("test")
	for _, command := range root.Commands() {
		require.NotEqual(t, "verify", command.Name())
	}
	command, _, err := root.Find([]string{"validate"})
	require.NoError(t, err)
	require.Equal(t, "validate", command.Name())
}

func TestValidateRejectsAnUnknownMode(t *testing.T) {
	dir := cleanRepo(t)

	_, err := runCLI(t, dir, "validate", "--mode", "nope")
	require.ErrorContains(t, err, "mode")
	require.ErrorContains(t, err, "nope")
}

func TestValidateWithNothingToCheck(t *testing.T) {
	dir := cleanRepo(t)
	writeConfig(t, dir, []string{"true"}, "")

	require.Equal(t, "Queued.\n", execute(t, dir, "validate"))
	drain(t, dir)
	require.Equal(t, "watcher: stopped\nsnapshot: none\n", execute(t, dir, "status"))
	require.Equal(t, "Queued.\n", execute(t, dir, "add"))
	drain(t, dir)
	require.Equal(t, "watcher: stopped\nsnapshot: none\n", execute(t, dir, "status"))
}

func TestAddFixesAFailingValidation(t *testing.T) {
	dir := repoWithStagedRename(t)
	script, _ := writeScript(t, "#!/bin/sh\ntest -f fix.txt\n")
	writeFullConfig(t, dir, []string{"sh", script}, "fake", "mark-1")
	piScript(t)
	require.Equal(t, "Queued.\n", execute(t, dir, "add"))
	drain(t, dir)
	out := execute(t, dir, "log")
	require.Contains(t, out, "Fixup ")
	require.Contains(t, out, "(passed)")
	require.NotContains(t, execute(t, dir, "status"), "passed")
	_, err := os.Stat(filepath.Join(dir, "fix.txt"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAddHaltsAfterFixupAttempts(t *testing.T) {
	dir := repoWithStagedRename(t)
	script, _ := writeScript(t, "#!/bin/sh\ntest -f never.txt\n")
	writeFullConfig(t, dir, []string{"sh", script}, "fake", "mark-1")
	piScript(t)
	require.Equal(t, "Queued.\n", execute(t, dir, "add"))
	drain(t, dir)
	out := execute(t, dir, "log")
	require.Contains(t, out, "Fixup ")
	require.Equal(t, 4, strings.Count(out, "(failed)\n"))
}

func TestValidateQueuesWithoutACommand(t *testing.T) {
	dir := cleanRepo(t)
	require.Equal(t, "Queued.\n", execute(t, dir, "validate"))
	drain(t, dir)
	require.Equal(t, "watcher: stopped\nsnapshot: none\n", execute(t, dir, "status"))
}

func TestSubmitIsNotFound(t *testing.T) {
	out, err := runCLI(t, cleanRepo(t), "submit")
	require.NoError(t, err)
	require.Contains(t, out, "Available Commands")
	require.Contains(t, out, "add")
	require.NotContains(t, out, "spor-id")
	require.NotContains(t, out, "No staged changes")
}

func TestResetWithNoAdd(t *testing.T) {
	require.Equal(t, "No submissions.\n", execute(t, cleanRepo(t), "reset"))
}

func TestLogShowsNewestFirstWithUnvalidated(t *testing.T) {
	dir := repoWithStagedRename(t)
	execute(t, dir, "add")
	drain(t, dir)

	out := execute(t, dir, "log")
	require.Equal(t, 2, strings.Count(out, "(unvalidated)\n"))
	rename := strings.Index(out, "Refactor: Rename variable x to y in f")
	remainder := strings.Index(out, "Apply the remaining added changes")
	require.Greater(t, rename, remainder)
}

func TestLogWithNoCommits(t *testing.T) {
	require.Equal(t, "No commits to show.\n", execute(t, cleanRepo(t), "log"))
}

func TestLogShowsPassedAfterValidate(t *testing.T) {
	dir := repoWithStagedRename(t)
	writeConfig(t, dir, []string{"true"}, "")
	execute(t, dir, "add")
	drain(t, dir)

	out := execute(t, dir, "log")
	require.Equal(t, 2, strings.Count(out, "(passed)\n"))
	require.NotContains(t, out, "unvalidated")
}

func TestLogShowsFailedAfterValidate(t *testing.T) {
	dir := repoWithStagedRename(t)
	script, _ := failScript(t)
	writeConfig(t, dir, []string{"sh", script}, "")
	require.Equal(t, "Queued.\n", execute(t, dir, "add"))
	drain(t, dir)

	out := execute(t, dir, "log")
	require.Equal(t, 1, strings.Count(out, "(failed)\n"))
	require.Equal(t, 1, strings.Count(out, "(unvalidated)\n"))
}

func drain(t *testing.T, dir string) {
	t.Helper()
	for {
		ok, err := daemon.ProcessPending(t.Context(), dir)
		require.NoError(t, err)
		if !ok {
			return
		}
	}
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
	testutil.IsolateGit(t)
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
	base, err := worktree.Commit("init", &git.CommitOptions{Author: &object.Signature{
		Name:  "Spor Test",
		Email: "spor@example.com",
		When:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}})
	require.NoError(t, err)
	require.NoError(t, repo.Storer.SetReference(plumbing.NewHashReference("refs/remotes/origin/main", base)))
	require.NoError(t, repo.Storer.SetReference(plumbing.NewSymbolicReference("refs/remotes/origin/HEAD", "refs/remotes/origin/main")))
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

type fullDocument struct {
	Auditor auditorDocument `json:"auditor"`
}

func writeFullConfig(t *testing.T, dir string, command []string, provider, model string) {
	t.Helper()
	body, err := json.Marshal(fullDocument{
		Auditor: auditorDocument{
			Validate: validateSettings{Command: command},
			Fixup:    fixupSettings{Model: fixupModelSettings{Provider: provider, Version: model}},
		},
	})
	require.NoError(t, err)
	path := filepath.Join(dir, ".git", "spor", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, body, 0o644))
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

func piScript(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "pi")
	body := "#!/bin/sh\nprintf 'fixed\\n' >> fix.txt\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("PATH", filepath.Join(dir, ":"+os.Getenv("PATH")))
}

func failScript(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	script := filepath.Join(dir, "validate.sh")
	body := "#!/bin/sh\ngit rev-parse HEAD >> \"$VALIDATE_LOG\"\nexit 1\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("VALIDATE_LOG", logPath)
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
