package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveVersionPrefersBuildVersion(t *testing.T) {
	previous := version
	t.Cleanup(func() { version = previous })

	version = "v9.9.9"
	require.Equal(t, "v9.9.9", resolveVersion())
}

func TestResolveVersionFallsBackWithoutBuildVersion(t *testing.T) {
	previous := version
	t.Cleanup(func() { version = previous })

	version = ""
	require.Equal(t, "dev", resolveVersion())
}
