package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/daemon"
)

func TestAutoSubmitAndDaemonPublishWithoutChangingWorkingFiles(t *testing.T) {
	dir, _ := branchRepo(t)
	base := branchGit(t, dir, "rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(afterFile), 0o644))
	ctx, cancel := context.WithCancel(t.Context())
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- daemon.RunReady(ctx, dir, nil) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-daemonDone) })
	require.Eventually(t, func() bool {
		return branchGit(t, dir, "rev-parse", "HEAD") != base
	}, 15*time.Second, 100*time.Millisecond)
	require.Empty(t, branchGit(t, dir, "diff", "--cached"))
	require.Empty(t, branchGit(t, dir, "diff"))
	content, err := os.ReadFile(filepath.Join(dir, "main.go"))
	require.NoError(t, err)
	require.Equal(t, afterFile, string(content))
}
