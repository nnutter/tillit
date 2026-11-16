package gh_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/99designs/keyring"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	cligh "github.com/nnutter/spor/internal/cli/gh"
	sporgh "github.com/nnutter/spor/internal/gh"
)

type fakeRing struct {
	items map[string][]byte
}

func newFakeRing() *fakeRing {
	return &fakeRing{items: map[string][]byte{}}
}

func (f *fakeRing) Get(key string) (keyring.Item, error) {
	data, ok := f.items[key]
	if !ok {
		return keyring.Item{}, keyring.ErrKeyNotFound
	}
	return keyring.Item{Key: key, Data: data}, nil
}

func (f *fakeRing) Remove(key string) error {
	delete(f.items, key)
	return nil
}

func (f *fakeRing) Set(item keyring.Item) error {
	f.items[item.Key] = item.Data
	return nil
}

func stubRing(t *testing.T, ring sporgh.Ring) {
	t.Helper()
	old := sporgh.OpenRing
	t.Cleanup(func() { sporgh.OpenRing = old })
	sporgh.OpenRing = func() (sporgh.Ring, error) { return ring, nil }
}

func runGH(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "spor"}
	root.PersistentFlags().String("repo", ".", "path to the git worktree")
	root.AddCommand(cligh.New())
	root.SilenceUsage = true
	root.SilenceErrors = true
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(append([]string{"--repo", dir}, args...))
	err := root.ExecuteContext(t.Context())
	return out.String(), err
}

func scrubGitEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
		"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_PREFIX", "GIT_EXEC_PATH", "GIT_SEQUENCE_EDITOR",
	} {
		// exec git rejects variables set to the empty string, so unset them.
		if value, ok := os.LookupEnv(key); ok {
			t.Cleanup(func() { _ = os.Setenv(key, value) })
			_ = os.Unsetenv(key)
		}
	}
}

func initGitRepo(t *testing.T, branch, remote string) string {
	t.Helper()
	scrubGitEnv(t)
	dir := t.TempDir()
	run(t, dir, "git", "init", "-b", branch)
	run(t, dir, "git", "config", "user.email", "test@example.com")
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "remote", "add", "origin", remote)
	run(t, dir, "git", "commit", "--allow-empty", "-m", "init")
	return dir
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
			"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
			"GIT_PREFIX", "GIT_EXEC_PATH", "GIT_SEQUENCE_EDITOR":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestAuthLoginPrintsGuidance(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	stubRing(t, newFakeRing())
	dir := initGitRepo(t, "feature", "https://github.com/octo/repo.git")

	out, err := runGH(t, dir, "gh", "auth", "login")
	require.NoError(t, err)
	require.Contains(t, out, "personal-access-tokens/new")
	require.Contains(t, out, "--token")
	require.Contains(t, out, "--import-gh")
}

func TestAuthLoginRejectsBothFlags(t *testing.T) {
	stubRing(t, newFakeRing())
	dir := initGitRepo(t, "feature", "https://github.com/octo/repo.git")

	_, err := runGH(t, dir, "gh", "auth", "login", "--token", "x", "--import-gh")
	require.ErrorContains(t, err, "only one of")
}

func TestAuthLoginStoresValidatedToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rate_limit":
			_, _ = w.Write([]byte(`{"resources":{"core":{"limit":5000,"remaining":4999}}}`))
		case "/user":
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	ring := newFakeRing()
	stubRing(t, ring)
	dir := initGitRepo(t, "feature", "https://github.com/octo/repo.git")

	out, err := runGH(t, dir, "gh", "auth", "login", "--token", "valid-token")
	require.NoError(t, err)
	require.Contains(t, out, "octocat")
	require.Contains(t, out, "Stored")
	require.Equal(t, []byte("valid-token"), ring.items[sporgh.TokenKey])
}

func TestAuthLoginRejectsInvalidToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	ring := newFakeRing()
	stubRing(t, ring)
	dir := initGitRepo(t, "feature", "https://github.com/octo/repo.git")

	_, err := runGH(t, dir, "gh", "auth", "login", "--token", "bad-token")
	require.ErrorContains(t, err, "validate token")
	require.Empty(t, ring.items[sporgh.TokenKey])
}

func TestAuthStatusReportsToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "env-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer env-token" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/rate_limit":
			_, _ = w.Write([]byte(`{"resources":{"core":{"limit":5000,"remaining":4990}}}`))
		case "/user":
			w.Header().Set("X-OAuth-Scopes", "repo:status, read:org")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	dir := initGitRepo(t, "feature", "https://github.com/octo/repo.git")

	out, err := runGH(t, dir, "gh", "auth", "status")
	require.NoError(t, err)
	require.Contains(t, out, "token: GH_TOKEN")
	require.Contains(t, out, "login: octocat")
	require.Contains(t, out, "repo:status")
	require.Contains(t, out, "4990/5000")
}

func TestAuthStatusReportsMissing(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	stubRing(t, newFakeRing())
	dir := initGitRepo(t, "feature", "https://github.com/octo/repo.git")

	_, err := runGH(t, dir, "gh", "auth", "status")
	require.ErrorContains(t, err, "spor gh auth login")
}

func watchServer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/octo/repo/pulls":
			_, _ = w.Write([]byte(`[{"number":7,"html_url":"https://github.com/octo/repo/pull/7","head":{"sha":"abc123"}}]`))
		case r.URL.Path == "/repos/octo/repo/commits/abc123/check-runs":
			_, _ = w.Write([]byte(`{"check_runs":[
				{"id":1,"name":"build","status":"completed","conclusion":"success","details_url":"https://x/1","app":{"slug":"github-actions"}},
				{"id":2,"name":"test","status":"completed","conclusion":"failure","details_url":"https://x/2","app":{"slug":"github-actions"}}
			]}`))
		case r.URL.Path == "/repos/octo/repo/commits/abc123/status":
			_, _ = w.Write([]byte(`{"state":"failure","statuses":[]}`))
		case r.URL.Path == "/repos/octo/repo/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":99}]}`))
		case r.URL.Path == "/repos/octo/repo/actions/runs/99/jobs":
			_, _ = w.Write([]byte(`{"jobs":[{"id":5,"name":"test","conclusion":"failure","html_url":"https://x/job"}]}`))
		case r.URL.Path == "/repos/octo/repo/actions/jobs/5/logs":
			_, _ = w.Write([]byte("FAIL: TestFoo\ntraceback line\n"))
		default:
			http.NotFound(w, r)
		}
	}
}

func TestWatchPROncePassing(t *testing.T) {
	t.Setenv("GH_TOKEN", "env-token")
	t.Setenv("GITHUB_TOKEN", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/octo/repo/pulls":
			_, _ = w.Write([]byte(`[{"number":7,"html_url":"https://github.com/octo/repo/pull/7","head":{"sha":"abc123"}}]`))
		case strings.Contains(r.URL.Path, "check-runs"):
			_, _ = w.Write([]byte(`{"check_runs":[
				{"id":1,"name":"build","status":"completed","conclusion":"success","details_url":"https://x/1","app":{"slug":"github-actions"}}
			]}`))
		case strings.Contains(r.URL.Path, "/status"):
			_, _ = w.Write([]byte(`{"state":"success","statuses":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	dir := initGitRepo(t, "feature", "https://github.com/octo/repo.git")

	out, err := runGH(t, dir, "gh", "watch-pr", "--once")
	require.NoError(t, err)
	require.Contains(t, out, "pr: #7")
	require.Contains(t, out, "all 1 checks passed")
}

func TestWatchPROnceFailingFetchesLogs(t *testing.T) {
	t.Setenv("GH_TOKEN", "env-token")
	t.Setenv("GITHUB_TOKEN", "")
	server := httptest.NewServer(watchServer())
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	dir := initGitRepo(t, "feature", "https://github.com/octo/repo.git")

	out, err := runGH(t, dir, "gh", "watch-pr", "--once")
	require.ErrorContains(t, err, "1 failing checks")
	require.Contains(t, out, "fail: test")
	require.Contains(t, out, "FAIL: TestFoo")
	require.Contains(t, out, "git fetch origin pull/7/head")

	matches, globErr := filepath.Glob(filepath.Join(dir, ".git", "spor", "gh-logs", "pr-7-abc123", "test.log"))
	require.NoError(t, globErr)
	require.Len(t, matches, 1)
	content, readErr := os.ReadFile(matches[0])
	require.NoError(t, readErr)
	require.Contains(t, string(content), "FAIL: TestFoo")
}
