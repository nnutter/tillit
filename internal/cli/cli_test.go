package cli_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/cli"
	"github.com/nnutter/spor/internal/daemon"
)

func TestAddAndStatusPrintTheSubmission(t *testing.T) {
	dir := repoWithStagedRename(t)

	require.Equal(t, "Queued.\n", execute(t, dir, "add"))
	drain(t, dir)
	status := execute(t, dir, "status")
	require.Contains(t, status, "spor-id: S")
	require.Contains(t, status, "Refactor: Rename variable x to y in f")
	require.Contains(t, status, "Apply the remaining added changes")
}

func TestStatusShowsEveryBranchCommit(t *testing.T) {
	dir := cleanRepo(t)
	writeConfig(t, dir, []string{"false"}, "")
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	defer func() { _ = repo.Close() }()
	wt, err := repo.Worktree()
	require.NoError(t, err)
	sig := &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()}
	first, err := wt.Commit("first failure", &git.CommitOptions{Author: sig, AllowEmptyCommits: true})
	require.NoError(t, err)
	_, err = auditor.ValidateBranch(t.Context(), dir)
	require.NoError(t, err)
	second, err := wt.Commit("later pending", &git.CommitOptions{Author: sig, AllowEmptyCommits: true})
	require.NoError(t, err)
	out := execute(t, dir, "status")
	require.Contains(t, out, first.String()+" first failure failed")
	require.Contains(t, out, second.String()+" later pending pending")
}

func TestStatusReportsAutomaticBranchVerification(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			dir := initRepo(t, []byte("package p\nfunc f() int { var x = 1; return x }\n"), []byte("package p\nfunc f() int { var y = 1; return y }\n"))
			if configured {
				writeConfig(t, dir, []string{"false"}, "")
			}
			repo, err := git.PlainOpen(dir)
			require.NoError(t, err)
			defer func() { _ = repo.Close() }()
			wt, err := repo.Worktree()
			require.NoError(t, err)
			sig := &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()}
			rename, err := wt.Commit("rename", &git.CommitOptions{Author: sig})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package p\nfunc f() int { var z = 1; return z }\n"), 0o644))
			_, err = wt.Add("main.go")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("change"), 0o644))
			_, err = wt.Add("extra.txt")
			require.NoError(t, err)
			mixed, err := wt.Commit("other change", &git.CommitOptions{Author: sig})
			require.NoError(t, err)
			before := execute(t, dir, "status")
			require.Contains(t, before, rename.String()+" rename")
			require.Contains(t, before, "verification: pending")
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- daemon.Run(ctx, dir) }()
			t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
			var out string
			ok := false
			for range 100 {
				out, err = runCLI(t, dir, "status")
				if err == nil && strings.Contains(out, "verification: verified") && strings.Contains(out, "verification: unsupported: commit must modify exactly one file") {
					ok = true
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			require.True(t, ok, "status: %s; error: %v", out, err)
			out = execute(t, dir, "status")
			require.Contains(t, out, mixed.String()+" other change")
			if configured {
				require.Contains(t, out, rename.String()+" rename failed")
			} else {
				require.Contains(t, out, "validate command is not configured")
			}
			head, err := repo.Head()
			require.NoError(t, err)
			require.Equal(t, mixed, head.Hash())
			status, err := wt.Status()
			require.NoError(t, err)
			require.True(t, status.IsClean())
		})
	}
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

func TestAddAndStatusShowAPassingValidation(t *testing.T) {
	dir := repoWithStagedRename(t)
	writeConfig(t, dir, []string{"true"}, "")

	require.Equal(t, "Queued.\n", execute(t, dir, "add"))
	drain(t, dir)
	status := execute(t, dir, "status")
	require.Contains(t, status, "mode: change")
	require.Equal(t, 2, strings.Count(status, " passed\n"))
	require.NotContains(t, status, "failed")
}

func TestAddStopsValidationAtTheFirstFailure(t *testing.T) {
	dir := repoWithStagedRename(t)
	script, logPath := failScript(t)
	writeConfig(t, dir, []string{"sh", script}, "")

	require.Equal(t, "Queued.\n", execute(t, dir, "add"))
	drain(t, dir)
	status := execute(t, dir, "status")
	require.Equal(t, 1, strings.Count(status, " failed\n"))
	require.NotContains(t, status, " passed\n")
	require.Len(t, logLines(t, logPath), 1)
}

func TestValidateModeOverridesTheConfiguredMode(t *testing.T) {
	dir := repoWithStagedRename(t)
	writeConfig(t, dir, []string{"true"}, "commit")
	execute(t, dir, "add")
	drain(t, dir)

	require.Equal(t, "Queued.\n", execute(t, dir, "validate", "--mode", "patch"))
	drain(t, dir)
	require.Contains(t, execute(t, dir, "status"), "mode: commit")
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
	require.Equal(t, "No submissions.\n", execute(t, dir, "status"))
	require.Equal(t, "Queued.\n", execute(t, dir, "add"))
	drain(t, dir)
	require.Equal(t, "No submissions.\n", execute(t, dir, "status"))
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
	require.Equal(t, "No submissions.\n", execute(t, dir, "status"))
}

func TestStageIsAnAliasForAdd(t *testing.T) {
	dir := repoWithStagedRename(t)

	require.Equal(t, "Queued.\n", execute(t, dir, "add"))
	drain(t, dir)
	status := execute(t, dir, "status")
	require.Contains(t, status, "spor-id: S")

	other := repoWithStagedRename(t)
	require.Equal(t, "Queued.\n", execute(t, other, "stage"))
	drain(t, other)
	staged := execute(t, other, "status")
	require.Contains(t, staged, "spor-id: S")
	require.Contains(t, staged, "Refactor: Rename variable x to y in f")
	require.Contains(t, staged, "Apply the remaining added changes")
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

	require.Equal(t, "Queued.\n", execute(t, dir, "add"))
	drain(t, dir)
	first := execute(t, dir, "status")

	require.Equal(t, "Queued.\n", execute(t, dir, "add"))
	drain(t, dir)
	second := execute(t, dir, "status")
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

func TestStatusReportsQueuedAddFailure(t *testing.T) {
	dir := repoWithStagedRename(t)
	writeConfig(t, dir, []string{"spor-command-that-does-not-exist"}, "")
	require.Contains(t, execute(t, dir, "add"), "Queued")
	processed, err := daemon.ProcessPending(t.Context(), dir)
	require.True(t, processed)
	require.Error(t, err)
	status := execute(t, dir, "status")
	require.Contains(t, status, "add: Error:")
	require.Contains(t, status, "spor-command-that-does-not-exist")
	writeConfig(t, dir, []string{"true"}, "")
	require.Contains(t, execute(t, dir, "validate"), "Queued")
	drain(t, dir)
	status = execute(t, dir, "status")
	require.Contains(t, status, "add: Error:")
	require.Contains(t, status, "spor-command-that-does-not-exist")
}

func TestRebaseReportsQueuedOutcome(t *testing.T) {
	dir := cleanRepo(t)
	require.Contains(t, execute(t, dir, "rebase"), "Queued")
	drain(t, dir)
	require.Contains(t, execute(t, dir, "status"), "No validated commits to integrate")
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
