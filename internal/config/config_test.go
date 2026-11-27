package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/config"
)

func TestLoadWorktreeOverridesTheCommonDir(t *testing.T) {
	gitDir := tempGitDir(t)
	common := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "commondir"), []byte(common+"\n"), 0o644))
	writeConfig(t, filepath.Join(common, "tillit", "config.json"), `{
		"auditor": {"validate": {"command": ["from-common"]}}
	}`)
	writeConfig(t, filepath.Join(gitDir, "tillit", "config.json"), `{
		"auditor": {"validate": {"command": ["from-worktree"]}}
	}`)

	cfg := load(t, gitDir)
	require.Equal(t, []string{"from-worktree"}, cfg.Command)
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
