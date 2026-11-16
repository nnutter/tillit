package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/config"
)

func TestParseSetValueReadsJSON(t *testing.T) {
	command, err := config.ParseSetValue("auditor.validate.command", `[
		"mise",
		"run",
		"all",
		"hello world",
		"a\"b"
	]`)
	require.NoError(t, err)
	require.Equal(t, []string{"mise", "run", "all", "hello world", `a"b`}, command)

	mode, err := config.ParseSetValue("auditor.validate.mode", `"patch"`)
	require.NoError(t, err)
	require.Equal(t, []string{"patch"}, mode)

	_, err = config.ParseSetValue("auditor.validate.command", `"mise run all"`)
	require.ErrorContains(t, err, "JSON")
	require.ErrorContains(t, err, "array")
	_, err = config.ParseSetValue("auditor.validate.mode", `["patch"]`)
	require.ErrorContains(t, err, "JSON")
	require.ErrorContains(t, err, "string")
	_, err = config.ParseSetValue("auditor.validate.command", "mise")
	require.ErrorContains(t, err, "JSON")
	_, err = config.ParseSetValue("auditor.validate.command", "null")
	require.ErrorContains(t, err, "array")
	_, err = config.ParseSetValue("auditor.validate.nope", `["x"]`)
	require.ErrorContains(t, err, "nope")
}
