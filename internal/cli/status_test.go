package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/cli"
)

func TestStatusReportsDeferredSnapshot(t *testing.T) {
	dir := cleanRepo(t)
	writeConfig(t, dir, []string{"false"}, "")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(after), 0o644))
	root := cli.New("test")
	root.SetArgs([]string{"--repo", dir, "start", "--foreground"})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	require.Eventually(t, func() bool {
		out, err := runCLI(t, dir, "status")
		return err == nil && strings.Contains(out, "watcher: running") && strings.Contains(out, "skipped") && strings.Contains(out, "Validation failed")
	}, 15*time.Second, 50*time.Millisecond)
}
