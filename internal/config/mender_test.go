package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/config"
)

func TestMenderModelConfiguration(t *testing.T) {
	dir := tempGitDir(t)
	require.NoError(t, config.Set(dir, config.ScopeWorktree, "mender.conflict.model.provider", []string{"test-provider"}))
	require.NoError(t, config.Set(dir, config.ScopeWorktree, "mender.conflict.model.version", []string{"test-model"}))
	require.NoError(t, config.Set(dir, config.ScopeWorktree, "mender.conflict.model.effort", []string{"high"}))
	cfg, err := config.Load(dir)
	require.NoError(t, err)
	require.Equal(t, "test-provider", cfg.Mender.Provider)
	require.Equal(t, "test-model", cfg.Mender.Model)
	require.Equal(t, "high", cfg.Mender.Thinking.String())
	value, err := config.Get(dir, config.ScopeEffective, "mender.conflict.model.version")
	require.NoError(t, err)
	require.Equal(t, "test-model\n", value)
	require.NoError(t, config.Remove(dir, config.ScopeWorktree, "mender.conflict.model.version", "", false))
	_, err = config.Get(dir, config.ScopeEffective, "mender.conflict.model.version")
	require.ErrorContains(t, err, "unset")
}
