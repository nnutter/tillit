package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/config"
)

func TestParseSetValueReadsJSON(t *testing.T) {
	command, err := config.ParseSetValue("verify.command", `[
		"mise",
		"run",
		"all",
		"hello world",
		"a\"b"
	]`)
	require.NoError(t, err)
	require.Equal(t, []string{"mise", "run", "all", "hello world", `a"b`}, command)

	mode, err := config.ParseSetValue("verify.mode", `"patch"`)
	require.NoError(t, err)
	require.Equal(t, []string{"patch"}, mode)

	_, err = config.ParseSetValue("verify.command", `"mise run all"`)
	require.ErrorContains(t, err, "JSON")
	require.ErrorContains(t, err, "array")
	_, err = config.ParseSetValue("verify.mode", `["patch"]`)
	require.ErrorContains(t, err, "JSON")
	require.ErrorContains(t, err, "string")
	_, err = config.ParseSetValue("verify.command", "mise")
	require.ErrorContains(t, err, "JSON")
	_, err = config.ParseSetValue("verify.command", "null")
	require.ErrorContains(t, err, "array")
	_, err = config.ParseSetValue("verify.nope", `["x"]`)
	require.ErrorContains(t, err, "nope")
}
