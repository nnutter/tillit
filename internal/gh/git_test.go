package gh_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/gh"
)

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		name      string
		remote    string
		owner     string
		repo      string
		wantError string
	}{
		{name: "https", remote: "https://github.com/nnutter/spor.git", owner: "nnutter", repo: "spor"},
		{name: "https bare", remote: "https://github.com/nnutter/spor", owner: "nnutter", repo: "spor"},
		{name: "ssh scp", remote: "git@github.com:nnutter/spor.git", owner: "nnutter", repo: "spor"},
		{name: "ssh url", remote: "ssh://git@github.com/nnutter/spor.git", owner: "nnutter", repo: "spor"},
		{name: "other host", remote: "git@gitlab.com:nnutter/spor.git", wantError: "gitlab.com"},
		{name: "not a url", remote: "origin", wantError: "parse origin remote"},
		{name: "missing name", remote: "https://github.com/nnutter", wantError: "owner/name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner, repo, err := gh.ParseRemoteURL(tc.remote)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.owner, owner)
			require.Equal(t, tc.repo, repo)
		})
	}
}

func TestCurrentBranchAndOwnerRepo(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "feature")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(dir+"/file.txt", []byte("hi\n"), 0o644))
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "init")
	runGit(t, dir, "remote", "add", "origin", "git@github.com:nnutter/spor.git")

	branch, err := gh.CurrentBranch(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, "feature", branch)

	owner, repo, err := gh.OwnerRepo(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, "nnutter", owner)
	require.Equal(t, "spor", repo)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
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
