package auditor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nnutter/spor/internal/repository"
)

func checkout(ctx context.Context, gitDir, hash string) error {
	dest, err := repository.CheckoutDir(gitDir)
	if err != nil {
		return err
	}
	ready, err := worktreeReady(dest)
	if err != nil {
		return err
	}
	if !ready {
		return addWorktree(ctx, gitDir, dest, hash)
	}
	return runGit(ctx, dest, "checkout", "--force", "--detach", hash)
}

func worktreeReady(dest string) (bool, error) {
	_, err := os.Stat(filepath.Join(dest, ".git"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func addWorktree(ctx context.Context, gitDir, dest, hash string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	common, err := repository.CommonDir(gitDir)
	if err != nil {
		return err
	}
	return runGit(ctx, common, "--git-dir", common, "worktree", "add", "--detach", dest, hash)
}

func runCommand(ctx context.Context, gitDir string, command []string) (bool, error) {
	if len(command) == 0 {
		return false, ErrCommandNotConfigured
	}
	dest, err := repository.CheckoutDir(gitDir)
	if err != nil {
		return false, err
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = dest
	cmd.Env = commandEnv()
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err == nil {
		return true, nil
	}
	if _, ok := errors.AsType[*exec.ExitError](err); ok {
		return false, nil
	}
	return false, err
}

func runGit(ctx context.Context, dir string, args ...string) error {
	_, err := gitOutput(ctx, dir, args...)
	return err
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := gitCommand(ctx, dir, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", gitError(args, err)
	}
	return string(out), nil
}

func gitInput(ctx context.Context, dir, input string, args ...string) (string, error) {
	cmd := gitCommand(ctx, dir, args...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		return "", gitError(args, err)
	}
	return string(out), nil
}

func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	prefix := []string{"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false"}
	cmd := exec.CommandContext(ctx, "git", append(prefix, args...)...)
	cmd.Dir = dir
	cmd.Env = commandEnv()
	return cmd
}

func gitError(args []string, err error) error {
	name := "git"
	if len(args) > 0 {
		name = args[0]
	}
	exit, ok := errors.AsType[*exec.ExitError](err)
	if ok {
		return exitError(name, exit)
	}
	return fmt.Errorf("%s: %w", name, err)
}

func exitError(name string, exit *exec.ExitError) error {
	text := strings.TrimSpace(string(exit.Stderr))
	if text == "" {
		return fmt.Errorf("%s: %w", name, exit)
	}
	return fmt.Errorf("%s: %s", name, text)
}

func commandEnv() []string {
	return stripGitEnv(os.Environ())
}

func stripGitEnv(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if gitEnvKey(key) {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

func gitEnvKey(key string) bool {
	switch key {
	case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR":
		return true
	case "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES":
		return true
	case "GIT_PREFIX", "GIT_EXEC_PATH", "GIT_SEQUENCE_EDITOR":
		return true
	default:
		return false
	}
}
