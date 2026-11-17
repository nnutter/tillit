package daemon_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/daemon"
)

func TestRequestLogsStartAndCompletion(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	dir := newRepo(t)
	require.NoError(t, daemon.EnqueueAdd(t.Context(), dir))
	processed, err := daemon.ProcessPending(t.Context(), dir)
	require.NoError(t, err)
	require.True(t, processed)
	require.Contains(t, logs.String(), "Processing request")
	require.Contains(t, logs.String(), "Request completed")
	require.Contains(t, logs.String(), "kind=add")
}
