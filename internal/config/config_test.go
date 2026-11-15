package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/config"
)

func TestLoadDefaultsToChange(t *testing.T) {
	gitDir := tempGitDir(t)
	cfg := load(t, gitDir)
	require.Empty(t, cfg.Command)
	require.Equal(t, config.ModeChange, cfg.Mode)
}

func TestLoadLaterFileReplacesOnlyTheFieldsItSets(t *testing.T) {
	gitDir := tempGitDir(t)
	writeConfig(t, filepath.Join(os.Getenv("HOME"), ".config", "spor", "config.json"), `{
		"verify": {"command": ["go", "test", "./..."], "mode": "commit"}
	}`)
	writeConfig(t, filepath.Join(gitDir, "spor", "config.json"), `{
		"verify": {"mode": "patch"}
	}`)

	cfg := load(t, gitDir)
	require.Equal(t, []string{"go", "test", "./..."}, cfg.Command)
	require.Equal(t, config.ModePatch, cfg.Mode)
}

func TestLoadWorktreeOverridesTheCommonDir(t *testing.T) {
	gitDir := tempGitDir(t)
	common := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "commondir"), []byte(common+"\n"), 0o644))
	writeConfig(t, filepath.Join(common, "spor", "config.json"), `{
		"verify": {"command": ["from-common"], "mode": "commit"}
	}`)
	writeConfig(t, filepath.Join(gitDir, "spor", "config.json"), `{
		"verify": {"command": ["from-worktree"]}
	}`)

	cfg := load(t, gitDir)
	require.Equal(t, []string{"from-worktree"}, cfg.Command)
	require.Equal(t, config.ModeCommit, cfg.Mode)
}

func TestLoadRejectsAnUnknownMode(t *testing.T) {
	gitDir := tempGitDir(t)
	writeConfig(t, filepath.Join(gitDir, "spor", "config.json"), `{
		"verify": {"mode": "nope"}
	}`)
	_, err := config.Load(gitDir)
	require.ErrorContains(t, err, "mode")
	require.ErrorContains(t, err, "nope")
}

func TestParseMode(t *testing.T) {
	for _, value := range []config.Mode{config.ModeCommit, config.ModePatch, config.ModeChange} {
		got, err := config.ParseMode(string(value))
		require.NoError(t, err)
		require.Equal(t, value, got)
	}
	_, err := config.ParseMode("Commit")
	require.ErrorContains(t, err, "mode")
}

func tempGitDir(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return t.TempDir()
}

func load(t *testing.T, gitDir string) config.Config {
	t.Helper()
	cfg, err := config.Load(gitDir)
	require.NoError(t, err)
	return cfg
}

func writeConfig(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}
