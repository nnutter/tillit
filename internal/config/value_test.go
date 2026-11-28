package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/config"
)

func TestParseSetValueReadsJSON(t *testing.T) {
	command, err := config.ParseSetValue("validate.command", `[
		"mise",
		"run",
		"all",
		"hello world",
		"a\"b"
	]`)
	require.NoError(t, err)
	require.Equal(t, []string{"mise", "run", "all", "hello world", `a"b`}, command)

	_, err = config.ParseSetValue("validate.command", `"mise run all"`)
	require.ErrorContains(t, err, "JSON")
	require.ErrorContains(t, err, "array")
	_, err = config.ParseSetValue("validate.command", "mise")
	require.ErrorContains(t, err, "JSON")
	_, err = config.ParseSetValue("validate.command", "null")
	require.ErrorContains(t, err, "array")
	_, err = config.ParseSetValue("validate.nope", `["x"]`)
	require.ErrorContains(t, err, "nope")
}
