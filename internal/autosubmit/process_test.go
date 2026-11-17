package autosubmit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/smith"
)

const (
	original = "package p\nfunc f() { x := 1; println(x) }\n"
	renamed  = "package p\nfunc f() { y := 1; println(y) }\n"
)

func fixture(t *testing.T, command string) (string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	mustGit(t, root, "init", "-b", "feature")
	mustGit(t, root, "config", "user.name", "Test")
	mustGit(t, root, "config", "user.email", "test@example.com")
	mustGit(t, root, "config", "core.hooksPath", "/dev/null")
	mustGit(t, root, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(root, "main.go"), original)
	mustGit(t, root, "add", "main.go")
	mustGit(t, root, "commit", "-m", "base")
	_, dir, err := location(root)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "spor"), 0o755))
	writeFile(t, filepath.Join(dir, "spor", "config.json"), `{"auditor":{"validate":{"command":["`+command+`"]}}}`)
	return root, dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func mustGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := git(t.Context(), root, "", args...)
	require.NoError(t, err)
	return out
}

func enable(t *testing.T, dir string) {
	t.Helper()
	db, err := openState(t.Context(), dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, heartbeat(t.Context(), db))
}

func state(t *testing.T, dir string) string {
	t.Helper()
	db, err := openState(t.Context(), dir)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var value string
	require.NoError(t, db.QueryRow(`SELECT state FROM auto_snapshot ORDER BY id DESC LIMIT 1`).Scan(&value))
	return value
}

func TestPublishesOnlyValidatedRefactorsWithoutWritingFiles(t *testing.T) {
	root, dir := fixture(t, "true")
	content := renamed + "func feature() int { return 2 }\n"
	writeFile(t, filepath.Join(root, "main.go"), content)
	writeFile(t, filepath.Join(root, "untracked.txt"), "untouched")
	before := mustGit(t, root, "rev-parse", "HEAD")
	index, err := os.ReadFile(filepath.Join(dir, "index"))
	require.NoError(t, err)
	message, err := Capture(t.Context(), root)
	require.NoError(t, err)
	require.Contains(t, message, "Submitted")
	captured, err := os.ReadFile(filepath.Join(dir, "index"))
	require.NoError(t, err)
	require.Equal(t, index, captured)
	require.NoError(t, Process(t.Context(), root)) // no watcher, no validation or publication
	require.Equal(t, before, mustGit(t, root, "rev-parse", "HEAD"))
	enable(t, dir)
	require.NoError(t, Process(t.Context(), root))
	require.Equal(t, "published", state(t, dir))
	require.NotEqual(t, before, mustGit(t, root, "rev-parse", "HEAD"))
	require.Contains(t, mustGit(t, root, "show", "HEAD:main.go"), "y := 1")
	require.NotContains(t, mustGit(t, root, "show", "HEAD:main.go"), "feature")
	actual, err := os.ReadFile(filepath.Join(root, "main.go"))
	require.NoError(t, err)
	require.Equal(t, content, string(actual))
	untracked, err := os.ReadFile(filepath.Join(root, "untracked.txt"))
	require.NoError(t, err)
	require.Equal(t, "untouched", string(untracked))
	require.Empty(t, mustGit(t, root, "diff", "--cached"))
	require.Contains(t, mustGit(t, root, "diff"), "feature")
	manual, err := smith.Status(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, "No submissions.", manual.Message)
}

func TestFailedValidationDoesNotPublish(t *testing.T) {
	root, dir := fixture(t, "false")
	before := mustGit(t, root, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(root, "main.go"), renamed)
	_, err := Capture(t.Context(), root)
	require.NoError(t, err)
	enable(t, dir)
	require.NoError(t, Process(t.Context(), root))
	require.Equal(t, "skipped", state(t, dir))
	require.Equal(t, before, mustGit(t, root, "rev-parse", "HEAD"))
	require.Empty(t, mustGit(t, root, "diff", "--cached"))
}

func TestStaleSnapshotsDoNotPublish(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, string, string)
	}{
		{"working edit", func(t *testing.T, root, dir string) {
			writeFile(t, filepath.Join(root, "main.go"), renamed+"// newer edit\n")
		}},
		{"staging", func(t *testing.T, root, dir string) { mustGit(t, root, "add", "main.go") }},
		{"branch", func(t *testing.T, root, dir string) { mustGit(t, root, "switch", "-c", "other") }},
		{"rebase", func(t *testing.T, root, dir string) {
			require.NoError(t, os.Mkdir(filepath.Join(dir, "rebase-merge"), 0o755))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, dir := fixture(t, "true")
			base := mustGit(t, root, "rev-parse", "HEAD")
			writeFile(t, filepath.Join(root, "main.go"), renamed)
			_, err := Capture(t.Context(), root)
			require.NoError(t, err)
			tc.change(t, root, dir)
			enable(t, dir)
			require.NoError(t, Process(t.Context(), root))
			require.Equal(t, "skipped", state(t, dir))
			require.Equal(t, base, mustGit(t, root, "rev-parse", "HEAD"))
		})
	}
}

func TestCaptureRefusesStagedChangesAndDeduplicates(t *testing.T) {
	root, dir := fixture(t, "true")
	writeFile(t, filepath.Join(root, "main.go"), renamed)
	mustGit(t, root, "add", "main.go")
	_, err := Capture(t.Context(), root)
	require.ErrorContains(t, err, "clean index")
	mustGit(t, root, "reset", "--mixed", "HEAD")
	_, err = Capture(t.Context(), root)
	require.NoError(t, err)
	enable(t, dir)
	require.NoError(t, Process(t.Context(), root))
	// After publication there is no tracked diff to submit again.
	message, err := Capture(t.Context(), root)
	require.NoError(t, err)
	require.Contains(t, message, "No tracked")
}

func TestWatcherCancellationAndDebounce(t *testing.T) {
	root, dir := fixture(t, "true")
	writeFile(t, filepath.Join(root, "main.go"), renamed)
	message, previous := watchPoll(t.Context(), root, dir, "")
	require.Contains(t, message, "stable snapshot")
	message, _ = watchPoll(t.Context(), root, dir, previous)
	require.Contains(t, message, "Submitted")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// A cancelled watcher must return promptly, never touching the branch.
	require.ErrorIs(t, Run(ctx, root), context.Canceled)
}
