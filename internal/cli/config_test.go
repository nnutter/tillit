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

	require.NoFileExists(t, filepath.Join(dir, ".git", "tillit", "config.json"))
	_, err := runCLI(t, dir, "config", "get", "validate.command")
	require.ErrorContains(t, err, "unset")

	execute(t, dir, "config", "set", "validate.command", `["go","test","-count=1","hello world"]`)
	require.Equal(t, "go\ntest\n-count=1\nhello world\n", execute(t, dir, "config", "get", "validate.command"))
	require.FileExists(t, filepath.Join(dir, ".git", "tillit", "config.json"))
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(home, ".config", "tillit", "config.json"))

	execute(t, dir, "config", "append", "validate.command", "-race")
	require.Equal(t, "go\ntest\n-count=1\nhello world\n-race\n", execute(t, dir, "config", "get", "validate.command"))
	execute(t, dir, "config", "rm", "validate.command", "-count=1")
	require.Equal(t, "go\ntest\nhello world\n-race\n", execute(t, dir, "config", "get", "validate.command"))
}

func TestConfigScopeFlagsChooseTheFile(t *testing.T) {
	dir := cleanRepo(t)
	execute(t, dir, "config", "set", "--global", "validate.command", `["from-global"]`)

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(home, ".config", "tillit", "config.json"))
	require.NoFileExists(t, filepath.Join(dir, ".git", "tillit", "config.json"))

	shown := execute(t, dir, "config", "show")
	require.Contains(t, shown, "from-global")
	globalShown := execute(t, dir, "config", "show", "--global")
	require.Contains(t, globalShown, "from-global")
	require.NotContains(t, globalShown, "change")

	worktree := linkedWorktree(t, dir)
	execute(t, worktree, "config", "set", "--repository", "validate.command", `["repository"]`)
	require.FileExists(t, filepath.Join(dir, ".git", "tillit", "config.json"))
	require.NoFileExists(t, filepath.Join(worktreeGitDir(t, worktree), "tillit", "config.json"))
	require.Equal(t, "repository\n", execute(t, worktree, "config", "get", "validate.command"))

	_, err = runCLI(t, dir, "config", "show", "--global", "--repository")
	require.ErrorContains(t, err, "scope")
}

func TestConfigReadsWorktreeRootFromSubdirectory(t *testing.T) {
	dir := cleanRepo(t)
	subdir := filepath.Join(dir, "nested")
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	execute(t, dir, "config", "set", "validate.command", `["external"]`)
	path := filepath.Join(dir, ".tillit", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`{"validate":{"command":["root"]}}`), 0o644))
	require.Equal(t, "root\n", execute(t, subdir, "config", "get", "validate.command"))
}

func TestConfigCompletesKeys(t *testing.T) {
	args := []string{"config", "get", "validate.c"}
	require.Contains(t, complete(t, args...), "validate.command\n")
	require.Contains(t, completeErr(t, args...), "ShellCompDirectiveNoFileComp")
	require.NotContains(t, complete(t, "config", "set", "validate.command", ""), "validate.command")
}

func TestConfigRejectsUnknownKeys(t *testing.T) {
	dir := cleanRepo(t)
	_, err := runCLI(t, dir, "config", "set", "validate.nope", `["x"]`)
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
