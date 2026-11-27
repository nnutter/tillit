package cli_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/gittest"

	"github.com/nnutter/tillit/internal/auditor"
	"github.com/nnutter/tillit/internal/cli"
	"github.com/nnutter/tillit/internal/daemon"
)

func TestStatusShowsEveryBranchCommit(t *testing.T) {
	dir := cleanRepo(t)
	writeConfig(t, dir, []string{"false"}, "")
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	defer func() { _ = repo.Close() }()
	wt, err := repo.Worktree()
	require.NoError(t, err)
	sig := &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()}
	first, err := wt.Commit("first failure", &git.CommitOptions{Author: sig, AllowEmptyCommits: true})
	require.NoError(t, err)
	_, err = auditor.ValidateBranch(t.Context(), dir)
	require.NoError(t, err)
	second, err := wt.Commit("later pending", &git.CommitOptions{Author: sig, AllowEmptyCommits: true})
	require.NoError(t, err)
	out := execute(t, dir, "status")
	require.Contains(t, out, first.String()+" first failure failed")
	require.Contains(t, out, second.String()+" later pending pending")
}

func TestStatusReportsAutomaticBranchVerification(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			dir := initRepo(t, []byte("package p\nfunc f() int { var x = 1; return x }\n"), []byte("package p\nfunc f() int { var y = 1; return y }\n"))
			if configured {
				writeConfig(t, dir, []string{"false"}, "")
			}
			repo, err := git.PlainOpen(dir)
			require.NoError(t, err)
			defer func() { _ = repo.Close() }()
			wt, err := repo.Worktree()
			require.NoError(t, err)
			sig := &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()}
			rename, err := wt.Commit("rename", &git.CommitOptions{Author: sig})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package p\nfunc f() int { var z = 1; return z }\n"), 0o644))
			_, err = wt.Add("main.go")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("change"), 0o644))
			_, err = wt.Add("extra.txt")
			require.NoError(t, err)
			mixed, err := wt.Commit("other change", &git.CommitOptions{Author: sig})
			require.NoError(t, err)
			before := execute(t, dir, "status")
			require.Contains(t, before, rename.String()+" rename")
			require.Contains(t, before, "verification: pending")
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- daemon.Run(ctx, dir) }()
			t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
			var out string
			ok := false
			for range 100 {
				out, err = runCLI(t, dir, "status")
				if err == nil && strings.Contains(out, "verification: verified") && strings.Contains(out, "verification: unsupported: commit must modify exactly one file") {
					ok = true
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			require.True(t, ok, "status: %s; error: %v", out, err)
			out = execute(t, dir, "status")
			require.Contains(t, out, mixed.String()+" other change")
			if configured {
				require.Contains(t, out, rename.String()+" rename failed")
			} else {
				require.Contains(t, out, "validate command is not configured")
			}
			head, err := repo.Head()
			require.NoError(t, err)
			require.Equal(t, mixed, head.Hash())
			status, err := wt.Status()
			require.NoError(t, err)
			require.True(t, status.IsClean())
		})
	}
}

func TestVersion(t *testing.T) {
	require.Equal(t, "0.1.0", cli.New("0.1.0").Version)
}

func TestGitHubCommandIsNotAvailable(t *testing.T) {
	out, err := runCLI(t, cleanRepo(t), "gh")
	require.NoError(t, err)
	require.Contains(t, out, "Available Commands")
	require.NotContains(t, out, "  gh ")
}

func execute(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runCLI(t, dir, args...)
	require.NoError(t, err)
	return out
}

func runCLI(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	root := cli.New("0.1.0")
	// Match Fang, which silences Cobra's usage and error text.
	root.SilenceUsage = true
	root.SilenceErrors = true
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	// Keep --repo ahead of the subcommand so a later value may start with '-'.
	root.SetArgs(append([]string{"--repo", dir}, args...))
	err := root.ExecuteContext(t.Context())
	return out.String(), err
}

func cleanRepo(t *testing.T) string {
	t.Helper()
	return initRepo(t, []byte(before), []byte(before))
}

func initRepo(t *testing.T, committed, staged []byte) string {
	t.Helper()
	gittest.WithSetenvEnviron(t)
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	cfg, err := repo.Config()
	require.NoError(t, err)
	cfg.User.Name = "Tillit Test"
	cfg.User.Email = "tillit@example.com"
	require.NoError(t, repo.SetConfig(cfg))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), committed, 0o644))
	worktree, err := repo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add("main.go")
	require.NoError(t, err)
	base, err := worktree.Commit("init", &git.CommitOptions{Author: &object.Signature{
		Name:  "Tillit Test",
		Email: "tillit@example.com",
		When:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}})
	require.NoError(t, err)
	require.NoError(t, repo.Storer.SetReference(plumbing.NewHashReference("refs/remotes/origin/main", base)))
	require.NoError(t, repo.Storer.SetReference(plumbing.NewSymbolicReference("refs/remotes/origin/HEAD", "refs/remotes/origin/main")))
	if !bytes.Equal(committed, staged) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), staged, 0o644))
		_, err = worktree.Add("main.go")
		require.NoError(t, err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return dir
}

func writeConfig(t *testing.T, dir string, command []string, mode string) {
	t.Helper()
	body, err := json.Marshal(validateDocument{Auditor: auditorDocument{Validate: validateSettings{Command: command, Mode: mode}}})
	require.NoError(t, err)
	path := filepath.Join(dir, ".git", "tillit", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, body, 0o644))
}

type validateDocument struct {
	Auditor auditorDocument `json:"auditor"`
}

type auditorDocument struct {
	Validate validateSettings `json:"validate"`
}

type validateSettings struct {
	Command []string `json:"command"`
	Mode    string   `json:"mode,omitempty"`
}

const before = `package p
import "fmt"
func f() {
	x := 1
	fmt.Println(x)
}
`
