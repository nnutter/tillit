package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/config"
)

func TestAppendAndRemoveListValues(t *testing.T) {
	gitDir := tempGitDir(t)
	require.NoError(t, config.Set(gitDir, config.ScopeGlobal, "validate.command", []string{"from-global"}))
	require.NoError(t, config.Append(gitDir, config.ScopeWorktree, "validate.command", "go"))
	require.NoError(t, config.Append(gitDir, config.ScopeWorktree, "validate.command", "test"))
	require.NoError(t, config.Append(gitDir, config.ScopeWorktree, "validate.command", "go"))

	command, err := config.Get(gitDir, config.ScopeWorktree, "validate.command")
	require.NoError(t, err)
	require.Equal(t, "go\ntest\ngo\n", command)

	require.NoError(t, config.Remove(gitDir, config.ScopeWorktree, "validate.command", "go", true))
	command, err = config.Get(gitDir, config.ScopeEffective, "validate.command")
	require.NoError(t, err)
	require.Equal(t, "test\n", command)

	err = config.Remove(gitDir, config.ScopeWorktree, "validate.command", "missing", true)
	require.ErrorContains(t, err, "match")
	require.ErrorContains(t, err, "missing")
	command, err = config.Get(gitDir, config.ScopeEffective, "validate.command")
	require.NoError(t, err)
	require.Equal(t, "test\n", command)

	require.NoError(t, config.Remove(gitDir, config.ScopeWorktree, "validate.command", "test", true))
	_, err = config.Get(gitDir, config.ScopeEffective, "validate.command")
	require.ErrorContains(t, err, "unset")
	command, err = config.Get(gitDir, config.ScopeWorktree, "validate.command")
	require.NoError(t, err)
	require.Empty(t, command)
	file, err := config.Show(gitDir, config.ScopeWorktree)
	require.NoError(t, err)
	require.Contains(t, file, "\"command\": []")
	require.NotContains(t, configText(t, gitDir, config.ScopeEffective), "from-global")
}

func TestRemoveKeyRestoresTheLowerLayer(t *testing.T) {
	gitDir := tempGitDir(t)
	require.NoError(t, config.Set(gitDir, config.ScopeGlobal, "validate.command", []string{"from-global"}))
	require.NoError(t, config.Set(gitDir, config.ScopeWorktree, "validate.command", []string{"from-worktree"}))
	require.NoError(t, config.Remove(gitDir, config.ScopeWorktree, "validate.command", "", false))

	command, err := config.Get(gitDir, config.ScopeEffective, "validate.command")
	require.NoError(t, err)
	require.Equal(t, "from-global\n", command)
	err = config.Remove(gitDir, config.ScopeWorktree, "validate.command", "", false)
	require.ErrorContains(t, err, "unset")
}

func TestEditRejectsUnknownKeys(t *testing.T) {
	gitDir := tempGitDir(t)
	err := config.Set(gitDir, config.ScopeWorktree, "validate.nope", []string{"x"})
	require.ErrorContains(t, err, "key")
	require.ErrorContains(t, err, "nope")
	err = config.Set(gitDir, config.ScopeWorktree, "validate.command", nil)
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
