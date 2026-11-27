package verifier_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/gittest"

	"github.com/nnutter/tillit/internal/verifier"
)

func TestVerifyCommit(t *testing.T) {
	const before = "package p\nfunc f() int { var x = 1; return x }\n"
	const after = "package p\nfunc f() int { var value = 1; return value }\n"
	tests := []struct {
		name             string
		initial, changes map[string]string
		status           verifier.Status
	}{
		{"rename", map[string]string{"p/a.go": before}, map[string]string{"p/a.go": after}, verifier.Verified},
		{"mixed", map[string]string{"p/a.go": before, "note": "a"}, map[string]string{"p/a.go": after, "note": "b"}, verifier.Bypassed},
		{"sibling test", map[string]string{"p/a.go": before, "p/a_test.go": "package p"}, map[string]string{"p/a.go": after}, verifier.Bypassed},
		{"sibling ignored", map[string]string{"p/a.go": before, "p/ignored.go": "//go:build ignore\n\npackage p"}, map[string]string{"p/a.go": after}, verifier.Bypassed},
		{"two go files", map[string]string{"p/a.go": before, "p/b.go": before}, map[string]string{"p/a.go": after, "p/b.go": after}, verifier.Bypassed},
		{"non go", map[string]string{"note": "a"}, map[string]string{"note": "b"}, verifier.Bypassed},
		{"addition", map[string]string{"note": "a"}, map[string]string{"p/a.go": after}, verifier.Bypassed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			gitTest(t, repo, "init", "-q")
			gitTest(t, repo, "config", "user.email", "test@example.com")
			gitTest(t, repo, "config", "user.name", "Test")
			for name, content := range tt.initial {
				writeTestFile(t, repo, name, content)
			}
			gitTest(t, repo, "add", ".")
			gitTest(t, repo, "commit", "-qm", "parent")
			root := gitTest(t, repo, "rev-parse", "HEAD")
			for name, content := range tt.changes {
				writeTestFile(t, repo, name, content)
			}
			gitTest(t, repo, "add", ".")
			gitTest(t, repo, "commit", "-qm", "change")
			hash := gitTest(t, repo, "rev-parse", "HEAD")
			writeTestFile(t, repo, "dirty", "untracked")
			writeTestFile(t, repo, "staged", "staged")
			gitTest(t, repo, "add", "staged")
			status := gitTest(t, repo, "status", "--porcelain=v1")
			head := gitTest(t, repo, "symbolic-ref", "HEAD")
			result, err := verifier.VerifyCommit(t.Context(), repo, hash)
			require.NoError(t, err)
			require.Equal(t, tt.status, result.Status, result.Reason)
			if tt.status == verifier.Verified {
				require.NotNil(t, result.Evidence)
				require.Equal(t, "p/a.go", result.Evidence.File)
				require.Equal(t, "value", result.Evidence.AfterName)
			} else {
				require.NotEmpty(t, result.Reason)
			}
			require.Equal(t, status, gitTest(t, repo, "status", "--porcelain=v1"))
			require.Equal(t, head, gitTest(t, repo, "symbolic-ref", "HEAD"))
			require.Equal(t, hash, gitTest(t, repo, "rev-parse", "HEAD"))
			rootResult, err := verifier.VerifyCommit(t.Context(), repo, root)
			require.NoError(t, err)
			require.Equal(t, verifier.Bypassed, rootResult.Status)
			tree := gitTest(t, repo, "rev-parse", hash+"^{tree}")
			merge := gitTest(t, repo, "commit-tree", tree, "-p", root, "-p", hash, "-m", "merge")
			mergeResult, err := verifier.VerifyCommit(t.Context(), repo, merge)
			require.NoError(t, err)
			require.Equal(t, verifier.Bypassed, mergeResult.Status)
		})
	}
}

func gitTest(t *testing.T, repo string, args ...string) string {
	t.Helper()
	gittest.WithSetenvEnviron(t)
	args = append([]string{"-C", repo, "-c", "core.hooksPath=/dev/null"}, args...)
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	return strings.TrimSpace(string(out))
}

func writeTestFile(t *testing.T, repo, name, content string) {
	t.Helper()
	filename := filepath.Join(repo, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0o755))
	require.NoError(t, os.WriteFile(filename, []byte(content), 0o600))
}
