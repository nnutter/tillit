package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/config"
)

func TestKeysUseTheDocumentJSONNames(t *testing.T) {
	require.Equal(t, []string{"auditor.fixup.model.effort", "auditor.fixup.model.provider", "auditor.fixup.model.version", "auditor.verify.command", "auditor.verify.mode", "mender.conflict.model.effort", "mender.conflict.model.provider", "mender.conflict.model.version"}, config.Keys())
}
