package auditor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nnutter/tillit/internal/repository"
	"github.com/nnutter/tillit/internal/subprocess"
)

var ErrCommandNotConfigured = errors.New("validate command is not configured")

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

func runCommand(ctx context.Context, gitDir string, command []string) (bool, commandOutput, error) {
	if len(command) == 0 {
		return false, commandOutput{}, ErrCommandNotConfigured
	}
	dest, err := repository.CheckoutDir(gitDir)
	if err != nil {
		return false, commandOutput{}, err
	}
	cmd := subprocess.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = dest
	cmd.Env = commandEnv()
	var capture outputCapture
	stream := io.MultiWriter(&capture, os.Stderr)
	cmd.Stdout, cmd.Stderr = stream, stream
	hash, _ := gitOutput(ctx, dest, "rev-parse", "HEAD")
	started := time.Now()
	slog.InfoContext(ctx, "Starting validation", "commit", strings.TrimSpace(hash))
	err = cmd.Run()
	slog.InfoContext(ctx, "Validation finished", "commit", strings.TrimSpace(hash), "passed", err == nil, "duration", time.Since(started))
	return commandResult(ctx, capture.result(), err)
}

func commandResult(ctx context.Context, output commandOutput, err error) (bool, commandOutput, error) {
	if err == nil {
		return true, output, nil
	}
	if ctx.Err() != nil {
		return false, output, ctx.Err()
	}
	if _, ok := errors.AsType[*exec.ExitError](err); ok {
		return false, output, nil
	}
	return false, output, err
}

const outputLimit = 1 << 20

type commandOutput struct {
	Text      string `json:"output"`
	Truncated bool   `json:"output_truncated"`
}

type outputCapture struct {
	buffer    bytes.Buffer
	truncated bool
}

func (c *outputCapture) Write(p []byte) (int, error) {
	size := min(len(p), outputLimit-c.buffer.Len())
	_, _ = c.buffer.Write(p[:size])
	if size < len(p) {
		c.truncated = true
	}
	return len(p), nil
}

func (c *outputCapture) result() commandOutput {
	return commandOutput{
		Text:      strings.ToValidUTF8(c.buffer.String(), "\uFFFD"),
		Truncated: c.truncated,
	}
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
