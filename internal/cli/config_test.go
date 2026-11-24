package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/cli"
)

func TestConfigCommandsEditTheWorktreeFile(t *testing.T) {
	dir := cleanRepo(t)

	shown := execute(t, dir, "config", "show")
	require.Contains(t, shown, `"mode": "change"`)
	require.NoFileExists(t, filepath.Join(dir, ".git", "tillit", "config.json"))
	require.Equal(t, "change\n", execute(t, dir, "config", "get", "auditor.validate.mode"))
	_, err := runCLI(t, dir, "config", "get", "auditor.validate.command")
	require.ErrorContains(t, err, "unset")

	execute(t, dir, "config", "set", "auditor.validate.command", `["go","test","-count=1","hello world"]`)
	execute(t, dir, "config", "set", "auditor.validate.mode", `"patch"`)
	require.Equal(t, "go\ntest\n-count=1\nhello world\n", execute(t, dir, "config", "get", "auditor.validate.command"))
	require.Equal(t, "patch\n", execute(t, dir, "config", "get", "auditor.validate.mode"))
	require.FileExists(t, filepath.Join(dir, ".git", "tillit", "config.json"))
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(home, ".config", "tillit", "config.json"))

	execute(t, dir, "config", "append", "auditor.validate.command", "-race")
	require.Equal(t, "go\ntest\n-count=1\nhello world\n-race\n", execute(t, dir, "config", "get", "auditor.validate.command"))
	execute(t, dir, "config", "rm", "auditor.validate.command", "-count=1")
	require.Equal(t, "go\ntest\nhello world\n-race\n", execute(t, dir, "config", "get", "auditor.validate.command"))

	execute(t, dir, "config", "remove", "auditor.validate.mode")
	require.Equal(t, "change\n", execute(t, dir, "config", "get", "auditor.validate.mode"))
}

func TestConfigScopeFlagsChooseTheFile(t *testing.T) {
	dir := cleanRepo(t)
	execute(t, dir, "config", "set", "--global", "auditor.validate.command", `["from-global"]`)

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(home, ".config", "tillit", "config.json"))
	require.NoFileExists(t, filepath.Join(dir, ".git", "tillit", "config.json"))

	shown := execute(t, dir, "config", "show")
	require.Contains(t, shown, "from-global")
	require.Contains(t, shown, `"mode": "change"`)
	globalShown := execute(t, dir, "config", "show", "--global")
	require.Contains(t, globalShown, "from-global")
	require.NotContains(t, globalShown, "change")

	worktree := linkedWorktree(t, dir)
	execute(t, worktree, "config", "set", "--repository", "auditor.validate.mode", `"commit"`)
	require.FileExists(t, filepath.Join(dir, ".git", "tillit", "config.json"))
	require.NoFileExists(t, filepath.Join(worktreeGitDir(t, worktree), "tillit", "config.json"))
	require.Equal(t, "commit\n", execute(t, worktree, "config", "get", "auditor.validate.mode"))

	_, err = runCLI(t, dir, "config", "show", "--global", "--repository")
	require.ErrorContains(t, err, "scope")
}

func TestConfigReadsMainWorktreeRootFromSubdirectory(t *testing.T) {
	dir := cleanRepo(t)
	subdir := filepath.Join(dir, "nested")
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	execute(t, dir, "config", "set", "auditor.validate.mode", `"commit"`)
	path := filepath.Join(dir, ".tillit", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`{"auditor":{"validate":{"mode":"patch"}}}`), 0o644))
	require.Equal(t, "patch\n", execute(t, subdir, "config", "get", "auditor.validate.mode"))
}

func TestConfigReadsWorktreeRootWithHighestPriority(t *testing.T) {
	dir := cleanRepo(t)
	worktree := linkedWorktree(t, dir)
	subdir := filepath.Join(worktree, "nested")
	require.NoError(t, os.MkdirAll(subdir, 0o755))

	execute(t, worktree, "config", "set", "--global", "auditor.validate.command", `["global"]`)
	execute(t, worktree, "config", "set", "--global", "auditor.fixup.model.provider", `"global-provider"`)
	execute(t, worktree, "config", "set", "--repository", "auditor.validate.mode", `"commit"`)
	execute(t, worktree, "config", "set", "auditor.validate.mode", `"patch"`)
	execute(t, worktree, "config", "set", "auditor.fixup.model.version", `"worktree-model"`)

	rootConfig := filepath.Join(worktree, ".tillit", "config.json")
	require.Equal(t, "patch\n", execute(t, subdir, "config", "get", "auditor.validate.mode"))
	require.NoError(t, os.MkdirAll(filepath.Dir(rootConfig), 0o755))
	require.NoError(t, os.WriteFile(rootConfig, []byte(`{"auditor":{"validate":{"command":[],"mode":"change"}}}`), 0o644))
	require.Equal(t, "change\n", execute(t, subdir, "config", "get", "auditor.validate.mode"))
	_, err := runCLI(t, subdir, "config", "get", "auditor.validate.command")
	require.ErrorContains(t, err, "unset")
	shown := execute(t, subdir, "config", "show")
	require.Contains(t, shown, "global-provider")
	require.Contains(t, shown, "worktree-model")
	require.NotContains(t, shown, "global\"")
	require.Equal(t, "commit\n", execute(t, dir, "config", "get", "auditor.validate.mode"))
	require.Equal(t, "commit\n", execute(t, worktree, "config", "get", "--repository", "auditor.validate.mode"))
	execute(t, subdir, "config", "set", "auditor.validate.mode", `"commit"`)
	require.Equal(t, "change\n", execute(t, subdir, "config", "get", "auditor.validate.mode"))
	require.FileExists(t, filepath.Join(worktreeGitDir(t, worktree), "tillit", "config.json"))
	require.NoError(t, os.WriteFile(rootConfig, []byte(`{invalid`), 0o644))
	_, err = runCLI(t, subdir, "config", "show")
	require.Error(t, err)
	require.NoError(t, os.Remove(rootConfig))
	require.Equal(t, "commit\n", execute(t, subdir, "config", "get", "auditor.validate.mode"))
}

func TestConfigCompletesKeys(t *testing.T) {
	get := complete(t, "config", "get", "")
	require.Contains(t, get, "auditor.validate.command\n")
	require.Contains(t, get, "auditor.validate.mode\n")
	require.Contains(t, completeErr(t, "config", "get", ""), "ShellCompDirectiveNoFileComp")

	prefix := complete(t, "config", "get", "auditor.validate.m")
	require.Contains(t, prefix, "auditor.validate.mode\n")
	require.NotContains(t, prefix, "auditor.validate.command")

	set := complete(t, "config", "set", "--global", "")
	require.Contains(t, set, "auditor.validate.command\n")
	require.Contains(t, set, "auditor.validate.mode\n")

	value := complete(t, "config", "set", "auditor.validate.command", "")
	require.NotContains(t, value, "auditor.validate.command")
	require.NotContains(t, value, "auditor.validate.mode")

	removed := complete(t, "config", "rm", "auditor.validate.c")
	require.Contains(t, removed, "auditor.validate.command\n")
	require.NotContains(t, removed, "auditor.validate.mode")

	appended := complete(t, "config", "append", "")
	require.Contains(t, appended, "auditor.validate.command\n")
	require.Contains(t, appended, "auditor.validate.mode\n")
}

func TestConfigRejectsUnknownKeysAndModes(t *testing.T) {
	dir := cleanRepo(t)

	_, err := runCLI(t, dir, "config", "set", "auditor.validate.nope", `["x"]`)
	require.ErrorContains(t, err, "nope")
	_, err = runCLI(t, dir, "config", "set", "auditor.validate.mode", `"nope"`)
	require.ErrorContains(t, err, "mode")
	require.ErrorContains(t, err, "nope")
	require.NoFileExists(t, filepath.Join(dir, ".git", "tillit", "config.json"))
}

func complete(t *testing.T, args ...string) string {
	t.Helper()
	out, _ := completeCommand(t, args...)
	return out
}

func completeErr(t *testing.T, args ...string) string {
	t.Helper()
	_, errText := completeCommand(t, args...)
	return errText
}

func completeCommand(t *testing.T, args ...string) (string, string) {
	t.Helper()
	root := cli.New("0.1.0")
	root.SilenceUsage = true
	root.SilenceErrors = true
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"__complete"}, args...))
	require.NoError(t, root.ExecuteContext(t.Context()))
	return out.String(), errBuf.String()
}

func linkedWorktree(t *testing.T, repo string) string {
	t.Helper()
	dest := t.TempDir()
	cmd := exec.Command(
		"git",
		"-C", repo,
		"-c", "core.hooksPath=/dev/null",
		"-c", "commit.gpgsign=false",
		"worktree", "add", "--detach", dest, "HEAD",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return dest
}

func worktreeGitDir(t *testing.T, worktree string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(worktree, ".git"))
	require.NoError(t, err)
	line := strings.TrimSpace(string(data))
	gitDir, ok := strings.CutPrefix(line, "gitdir: ")
	require.True(t, ok)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktree, gitDir)
	}
	return gitDir
}
