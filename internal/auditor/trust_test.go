package auditor_test

import (
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/smith"
)

func TestVerifyFollowsMiseTrust(t *testing.T) {
	unsetCI(t)
	_, dir := linkedRepo(t)
	trustMise(t, dir)
	addChange(t, dir, "a.go", pureAfter)
	verifyPassed(t, dir)
	require.Equal(t, "trusted", miseTrustWord(t, sporCheckout(t, dir)))

	untrustMise(t, dir)
	addChange(t, dir, "b.go", extractAfter)
	verifyPassed(t, dir)
	require.Equal(t, "untrusted", miseTrustWord(t, sporCheckout(t, dir)))
}

func TestVerifyFollowsAnIgnoredMiseConfig(t *testing.T) {
	unsetCI(t)
	_, dir := linkedRepo(t)
	ignoreMise(t, dir)
	addChange(t, dir, "a.go", pureAfter)
	verifyPassed(t, dir)

	// mise run trusts an untrusted config. An ignored one stays untrusted.
	spor := sporCheckout(t, dir)
	runMise(t, "run", "-C", spor, "marker")
	require.Equal(t, "untrusted", miseTrustWord(t, spor))
}

func linkedRepo(t *testing.T) (string, string) {
	t.Helper()
	_, mainDir := newRepo(t, map[string]string{
		"a.go":      pureBefore,
		"b.go":      extractBefore,
		"mise.toml": "[tasks.marker]\nrun = \"echo marker\"\n",
		"notes.txt": "keep\n",
	})
	userDir := filepath.Join(t.TempDir(), "user")
	runGit(t, mainDir, "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "worktree", "add", "--detach", userDir, "HEAD")
	return mainDir, userDir
}

func addChange(t *testing.T, dir, name, body string) {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	stage(t, repo, dir, name, body)
	writeWorktreeConfig(t, dir, []string{"/usr/bin/true"})
	added, err := smith.Add(t.Context(), dir)
	require.NoError(t, err)
	require.NotEmpty(t, added.Commits)
}

func verifyPassed(t *testing.T, dir string) {
	t.Helper()
	verified, err := auditor.Verify(t.Context(), dir, "")
	require.NoError(t, err)
	require.False(t, verified.Failed)
	require.NotEmpty(t, verified.Outcomes)
}

func writeWorktreeConfig(t *testing.T, dir string, command []string) {
	t.Helper()
	gitDir := strings.TrimSpace(runGit(t, dir, "rev-parse", "--absolute-git-dir"))
	body, err := json.Marshal(verifyDocument{Auditor: auditorDocument{Verify: verifySettings{Command: command}}})
	require.NoError(t, err)
	path := filepath.Join(gitDir, "spor", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, body, 0o644))
}

func trustMise(t *testing.T, dir string) {
	t.Helper()
	runMise(t, "trust", dir)
}

func untrustMise(t *testing.T, dir string) {
	t.Helper()
	runMise(t, "trust", "--untrust", dir)
}

func ignoreMise(t *testing.T, dir string) {
	t.Helper()
	runMise(t, "trust", "--ignore", dir)
}

func miseTrustWord(t *testing.T, dir string) string {
	t.Helper()
	text := runMise(t, "trust", "--show", "-C", dir)
	prefix := dir + ": "
	for line := range strings.SplitSeq(text, "\n") {
		status, ok := strings.CutPrefix(strings.TrimSpace(line), prefix)
		if ok {
			return status
		}
	}
	require.Fail(t, "mise trust status missing", text)
	return ""
}

func runMise(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "mise", args...)
	cmd.Dir = t.TempDir()
	cmd.Env = withoutGitEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

func isolateMise(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("MISE_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("MISE_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("MISE_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("MISE_CACHE_DIR", filepath.Join(root, "cache"))
	t.Setenv("MISE_TRUSTED_CONFIG_PATHS", "")
	t.Setenv("MISE_IGNORED_CONFIG_PATHS", workspaceMiseConfig(t))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "config"), 0o755))
}

func workspaceMiseConfig(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		path := filepath.Join(dir, "mise.toml")
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func unsetCI(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"CI",
		"CONTINUOUS_INTEGRATION",
		"GITHUB_ACTIONS",
		"GITLAB_CI",
		"TRAVIS",
		"CIRCLECI",
		"BUILDKITE",
		"JENKINS_URL",
		"TF_BUILD",
		"DRONE",
		"APPVEYOR",
		"CODEBUILD_BUILD_ID",
		"TEAMCITY_VERSION",
	} {
		unsetEnv(t, key)
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	value, ok := os.LookupEnv(key)
	require.NoError(t, os.Unsetenv(key))
	t.Cleanup(func() {
		if !ok {
			return
		}
		_ = os.Setenv(key, value)
	})
}
