package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/cli"
)

func TestConfigCommandsEditTheWorktreeFile(t *testing.T) {
	dir := cleanRepo(t)

	shown := execute(t, dir, "config", "show")
	require.Contains(t, shown, `"mode": "change"`)
	require.NoFileExists(t, filepath.Join(dir, ".git", "spor", "config.json"))
	require.Equal(t, "change\n", execute(t, dir, "config", "get", "auditor.verify.mode"))
	_, err := runCLI(t, dir, "config", "get", "auditor.verify.command")
	require.ErrorContains(t, err, "unset")

	execute(t, dir, "config", "set", "auditor.verify.command", `["go","test","-count=1","hello world"]`)
	execute(t, dir, "config", "set", "auditor.verify.mode", `"patch"`)
	require.Equal(t, "go\ntest\n-count=1\nhello world\n", execute(t, dir, "config", "get", "auditor.verify.command"))
	require.Equal(t, "patch\n", execute(t, dir, "config", "get", "auditor.verify.mode"))
	require.FileExists(t, filepath.Join(dir, ".git", "spor", "config.json"))
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(home, ".config", "spor", "config.json"))

	execute(t, dir, "config", "append", "auditor.verify.command", "-race")
	require.Equal(t, "go\ntest\n-count=1\nhello world\n-race\n", execute(t, dir, "config", "get", "auditor.verify.command"))
	execute(t, dir, "config", "rm", "auditor.verify.command", "-count=1")
	require.Equal(t, "go\ntest\nhello world\n-race\n", execute(t, dir, "config", "get", "auditor.verify.command"))

	execute(t, dir, "config", "remove", "auditor.verify.mode")
	require.Equal(t, "change\n", execute(t, dir, "config", "get", "auditor.verify.mode"))
}

func TestConfigScopeFlagsChooseTheFile(t *testing.T) {
	dir := cleanRepo(t)
	execute(t, dir, "config", "set", "--global", "auditor.verify.command", `["from-global"]`)

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(home, ".config", "spor", "config.json"))
	require.NoFileExists(t, filepath.Join(dir, ".git", "spor", "config.json"))

	shown := execute(t, dir, "config", "show")
	require.Contains(t, shown, "from-global")
	require.Contains(t, shown, `"mode": "change"`)
	globalShown := execute(t, dir, "config", "show", "--global")
	require.Contains(t, globalShown, "from-global")
	require.NotContains(t, globalShown, "change")

	worktree := linkedWorktree(t, dir)
	execute(t, worktree, "config", "set", "--repository", "auditor.verify.mode", `"commit"`)
	require.FileExists(t, filepath.Join(dir, ".git", "spor", "config.json"))
	require.NoFileExists(t, filepath.Join(worktreeGitDir(t, worktree), "spor", "config.json"))
	require.Equal(t, "commit\n", execute(t, worktree, "config", "get", "auditor.verify.mode"))

	_, err = runCLI(t, dir, "config", "show", "--global", "--repository")
	require.ErrorContains(t, err, "scope")
}

func TestConfigCompletesKeys(t *testing.T) {
	get := complete(t, "config", "get", "")
	require.Contains(t, get, "auditor.verify.command\n")
	require.Contains(t, get, "auditor.verify.mode\n")
	require.Contains(t, completeErr(t, "config", "get", ""), "ShellCompDirectiveNoFileComp")

	prefix := complete(t, "config", "get", "auditor.verify.m")
	require.Contains(t, prefix, "auditor.verify.mode\n")
	require.NotContains(t, prefix, "auditor.verify.command")

	set := complete(t, "config", "set", "--global", "")
	require.Contains(t, set, "auditor.verify.command\n")
	require.Contains(t, set, "auditor.verify.mode\n")

	value := complete(t, "config", "set", "auditor.verify.command", "")
	require.NotContains(t, value, "auditor.verify.command")
	require.NotContains(t, value, "auditor.verify.mode")

	removed := complete(t, "config", "rm", "auditor.verify.c")
	require.Contains(t, removed, "auditor.verify.command\n")
	require.NotContains(t, removed, "auditor.verify.mode")

	appended := complete(t, "config", "append", "")
	require.Contains(t, appended, "auditor.verify.command\n")
	require.Contains(t, appended, "auditor.verify.mode\n")
}

func TestConfigRejectsUnknownKeysAndModes(t *testing.T) {
	dir := cleanRepo(t)

	_, err := runCLI(t, dir, "config", "set", "auditor.verify.nope", `["x"]`)
	require.ErrorContains(t, err, "nope")
	_, err = runCLI(t, dir, "config", "set", "auditor.verify.mode", `"nope"`)
	require.ErrorContains(t, err, "mode")
	require.ErrorContains(t, err, "nope")
	require.NoFileExists(t, filepath.Join(dir, ".git", "spor", "config.json"))
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
	cmd.Env = withoutGitEnv(os.Environ())
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

func withoutGitEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if gitEnvKey(key) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func gitEnvKey(key string) bool {
	switch key {
	case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
		"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_PREFIX", "GIT_EXEC_PATH", "GIT_SEQUENCE_EDITOR":
		return true
	default:
		return false
	}
}
