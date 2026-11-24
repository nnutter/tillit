package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/config"
)

func TestShowPrintsTheMergedSettings(t *testing.T) {
	gitDir := tempGitDir(t)

	text, err := config.Show(gitDir, config.ScopeEffective)
	require.NoError(t, err)
	require.JSONEq(t, `{"auditor":{"fixup":{"model":{"effort":"low"}},"validate":{"mode":"change"}},"mender":{"conflict":{"model":{"effort":"low"}}}}`, text)
	require.Contains(t, text, "\n      \"mode\": \"change\"\n")

	mode, err := config.Get(gitDir, config.ScopeEffective, "auditor.validate.mode")
	require.NoError(t, err)
	require.Equal(t, "change\n", mode)
	_, err = config.Get(gitDir, config.ScopeEffective, "auditor.validate.command")
	require.ErrorContains(t, err, "unset")

	file, err := config.Show(gitDir, config.ScopeWorktree)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, file)
	require.Contains(t, file, "{}\n")
	_, err = os.Stat(filepath.Join(gitDir, "tillit", "config.json"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSetReplacesOneField(t *testing.T) {
	gitDir := tempGitDir(t)
	require.NoError(t, config.Set(gitDir, config.ScopeGlobal, "auditor.validate.command", []string{"from-global"}))
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "auditor.validate.command", []string{"go", "test"}))
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "auditor.validate.mode", []string{"patch"}))

	command, err := config.Get(gitDir, config.ScopeEffective, "auditor.validate.command")
	require.NoError(t, err)
	require.Equal(t, "go\ntest\n", command)
	mode, err := config.Get(gitDir, config.ScopeEffective, "auditor.validate.mode")
	require.NoError(t, err)
	require.Equal(t, "patch\n", mode)

	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "auditor.validate.command", []string{`a"b`}))
	command, err = config.Get(gitDir, config.ScopeEffective, "auditor.validate.command")
	require.NoError(t, err)
	require.Equal(t, "a\"b\n", command)
	mode, err = config.Get(gitDir, config.ScopeWorktree, "auditor.validate.mode")
	require.NoError(t, err)
	require.Equal(t, "patch\n", mode)
}

func TestAppendAndRemoveListValues(t *testing.T) {
	gitDir := tempGitDir(t)
	require.NoError(t, config.Set(gitDir, config.ScopeGlobal, "auditor.validate.command", []string{"from-global"}))
	require.NoError(t, config.Append(gitDir, config.ScopeWorktree, "auditor.validate.command", "go"))
	require.NoError(t, config.Append(gitDir, config.ScopeWorktree, "auditor.validate.command", "test"))
	require.NoError(t, config.Append(gitDir, config.ScopeWorktree, "auditor.validate.command", "go"))

	command, err := config.Get(gitDir, config.ScopeWorktree, "auditor.validate.command")
	require.NoError(t, err)
	require.Equal(t, "go\ntest\ngo\n", command)

	require.NoError(t, config.Remove(gitDir, config.ScopeWorktree, "auditor.validate.command", "go", true))
	command, err = config.Get(gitDir, config.ScopeEffective, "auditor.validate.command")
	require.NoError(t, err)
	require.Equal(t, "test\n", command)

	err = config.Remove(gitDir, config.ScopeWorktree, "auditor.validate.command", "missing", true)
	require.ErrorContains(t, err, "match")
	require.ErrorContains(t, err, "missing")
	command, err = config.Get(gitDir, config.ScopeEffective, "auditor.validate.command")
	require.NoError(t, err)
	require.Equal(t, "test\n", command)

	require.NoError(t, config.Remove(gitDir, config.ScopeWorktree, "auditor.validate.command", "test", true))
	_, err = config.Get(gitDir, config.ScopeEffective, "auditor.validate.command")
	require.ErrorContains(t, err, "unset")
	command, err = config.Get(gitDir, config.ScopeWorktree, "auditor.validate.command")
	require.NoError(t, err)
	require.Empty(t, command)
	file, err := config.Show(gitDir, config.ScopeWorktree)
	require.NoError(t, err)
	require.Contains(t, file, "\"command\": []")
	require.NotContains(t, configText(t, gitDir, config.ScopeEffective), "from-global")
}

func TestRemoveKeyRestoresTheLowerLayer(t *testing.T) {
	gitDir := tempGitDir(t)
	require.NoError(t, config.Set(gitDir, config.ScopeGlobal, "auditor.validate.command", []string{"from-global"}))
	require.NoError(t, config.Set(gitDir, config.ScopeGlobal, "auditor.validate.mode", []string{"commit"}))
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "auditor.validate.mode", []string{"patch"}))

	require.NoError(t, config.Remove(gitDir, config.ScopeWorktree, "auditor.validate.mode", "", false))
	mode, err := config.Get(gitDir, config.ScopeEffective, "auditor.validate.mode")
	require.NoError(t, err)
	require.Equal(t, "commit\n", mode)
	command, err := config.Get(gitDir, config.ScopeEffective, "auditor.validate.command")
	require.NoError(t, err)
	require.Equal(t, "from-global\n", command)

	err = config.Remove(gitDir, config.ScopeWorktree, "auditor.validate.mode", "", false)
	require.ErrorContains(t, err, "unset")
	err = config.Append(gitDir, config.ScopeWorktree, "auditor.validate.mode", "patch")
	require.ErrorContains(t, err, "list")
	err = config.Remove(gitDir, config.ScopeWorktree, "auditor.validate.mode", "commit", true)
	require.ErrorContains(t, err, "list")
}

func TestRepositoryFileIsSeparateFromTheWorktreeFile(t *testing.T) {
	gitDir := tempGitDir(t)
	common := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "commondir"), []byte(common+"\n"), 0o644))
	require.NoError(t, config.Set(gitDir, config.ScopeRepository, "auditor.validate.mode", []string{"commit"}))
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "auditor.validate.command", []string{"from-worktree"}))

	mode, err := config.Get(gitDir, config.ScopeEffective, "auditor.validate.mode")
	require.NoError(t, err)
	require.Equal(t, "commit\n", mode)
	command, err := config.Get(gitDir, config.ScopeEffective, "auditor.validate.command")
	require.NoError(t, err)
	require.Equal(t, "from-worktree\n", command)
	_, err = config.Get(gitDir, config.ScopeRepository, "auditor.validate.command")
	require.ErrorContains(t, err, "unset")
	_, err = os.Stat(filepath.Join(common, "tillit", "config.json"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(gitDir, "tillit", "config.json"))
	require.NoError(t, err)
}

func TestEditRejectsUnknownKeysAndModes(t *testing.T) {
	gitDir := tempGitDir(t)
	err := config.Set(gitDir, config.ScopeWorktree, "auditor.validate.nope", []string{"x"})
	require.ErrorContains(t, err, "key")
	require.ErrorContains(t, err, "nope")
	err = config.Set(gitDir, config.ScopeWorktree, "auditor.validate.mode", []string{"nope"})
	require.ErrorContains(t, err, "mode")
	require.ErrorContains(t, err, "nope")
	err = config.Set(gitDir, config.ScopeWorktree, "auditor.validate.mode", []string{"patch", "commit"})
	require.ErrorContains(t, err, "value")
	err = config.Set(gitDir, config.ScopeWorktree, "auditor.validate.command", nil)
	require.ErrorContains(t, err, "value")
	_, err = os.Stat(filepath.Join(gitDir, "tillit", "config.json"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func configText(t *testing.T, gitDir string, scope config.Scope) string {
	t.Helper()
	text, err := config.Show(gitDir, scope)
	require.NoError(t, err)
	return text
}
