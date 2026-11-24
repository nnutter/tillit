// Package testutil provides shared helpers for Tillit tests.
package testutil

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// IsolateGit removes inherited Git settings for the lifetime of the test.
func IsolateGit(t *testing.T) {
	t.Helper()
	// Git prepends its exec directory to PATH before running rebase exec commands.
	// Remove it before os/exec looks up Git, not just from the command's environment.
	if execPath := os.Getenv("GIT_EXEC_PATH"); execPath != "" {
		first, rest, _ := strings.Cut(os.Getenv("PATH"), string(os.PathListSeparator))
		if first == execPath {
			t.Setenv("PATH", rest)
		}
	}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			continue
		}
		key, _, _ := strings.Cut(entry, "=")
		// Setenv registers restoration and checks that the test is not parallel.
		// An empty value is not enough: Git distinguishes it from an unset variable.
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}
}
