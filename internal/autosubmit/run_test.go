package autosubmit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOnlyOneWatcherAndStoppingRevokesPublication(t *testing.T) {
	root, dir := fixture(t, "true")
	db, err := openState(t.Context(), dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, root) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	require.Eventually(t, func() bool { return active(t.Context(), db) }, 5*time.Second, 10*time.Millisecond)
	require.ErrorContains(t, Run(t.Context(), root), "already running")
	cancel()
	require.Eventually(t, func() bool { return !active(t.Context(), db) }, 5*time.Second, 10*time.Millisecond)
}
