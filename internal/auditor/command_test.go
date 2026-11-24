package auditor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/repository"
)

func TestRunCommandCancellationIsNotValidationFailure(t *testing.T) {
	gitDir := t.TempDir()
	dest, err := repository.CheckoutDir(gitDir)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dest, 0o755))
	started := filepath.Join(t.TempDir(), "started")
	descendantOutput := filepath.Join(t.TempDir(), "descendant")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type result struct {
		passed bool
		err    error
	}
	done := make(chan result, 1)
	go func() {
		passed, err := runCommand(ctx, gitDir, []string{"sh", "-c", "(sleep 1; touch \"$2\") & touch \"$1\"; wait", "sh", started, descendantOutput})
		done <- result{passed, err}
	}()
	require.Eventually(t, func() bool {
		_, err := os.Stat(started)
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case got := <-done:
		require.False(t, got.passed)
		require.ErrorIs(t, got.err, context.Canceled)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "validation did not stop")
	}
	time.Sleep(1500 * time.Millisecond)
	_, err = os.Stat(descendantOutput)
	require.ErrorIs(t, err, os.ErrNotExist)
}
