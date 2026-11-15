package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/config"
)

func TestKeysUseTheDocumentJSONNames(t *testing.T) {
	require.Equal(t, []string{"verify.command", "verify.mode"}, config.Keys())
}
