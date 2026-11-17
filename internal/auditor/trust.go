package auditor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// syncMiseTrust copies the mise trust decision for source onto dest.
// Mise records that decision on one checkout directory. Spor validates in its
// own checkout, so the decision has to be copied there before mise runs.
func syncMiseTrust(ctx context.Context, source, dest string) error {
	if _, err := exec.LookPath("mise"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil
		}
		return err
	}
	source, err := canonicalPath(source)
	if err != nil {
		return err
	}
	dest, err = canonicalPath(dest)
	if err != nil {
		return err
	}
	status, err := miseStatus(ctx, source)
	if err != nil {
		return err
	}
	switch status {
	case miseTrusted:
		return applyMiseTrust(ctx, dest)
	case miseIgnored:
		return applyMiseTrust(ctx, dest, "--ignore")
	default:
		return applyMiseTrust(ctx, dest, "--untrust")
	}
}

type miseTrustStatus int

const (
	miseUntrusted miseTrustStatus = iota
	miseTrusted
	miseIgnored
)

func miseStatus(ctx context.Context, dir string) (miseTrustStatus, error) {
	out, err := runMise(ctx, dir, "trust", "--show", "-C", dir)
	if err != nil {
		return miseUntrusted, err
	}
	trusted, found, err := parseTrustShow(dir, out)
	if err != nil {
		return miseUntrusted, err
	}
	if showsTrusted(found, trusted) {
		return miseTrusted, nil
	}
	ignored, err := persistedIgnored(dir)
	if err != nil {
		return miseUntrusted, err
	}
	if ignored {
		return miseIgnored, nil
	}
	return miseUntrusted, nil
}

func applyMiseTrust(ctx context.Context, dest string, flags ...string) error {
	args := append([]string{"trust"}, flags...)
	args = append(args, dest)
	_, err := runMise(ctx, dest, args...)
	return err
}

func runMise(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "mise", args...)
	cmd.Dir = dir
	cmd.Env = commandEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", miseError(args, err)
	}
	return string(out), nil
}

func miseError(args []string, err error) error {
	text := ""
	exit, ok := errors.AsType[*exec.ExitError](err)
	if ok {
		text = strings.TrimSpace(string(exit.Stderr))
	}
	name := strings.Join(args, " ")
	if text == "" {
		return fmt.Errorf("mise %s: %w", name, err)
	}
	return fmt.Errorf("mise %s: %s", name, text)
}

func parseTrustShow(dir, output string) (bool, bool, error) {
	target, err := canonicalPath(dir)
	if err != nil {
		return false, false, err
	}
	for line := range strings.SplitSeq(output, "\n") {
		path, status, ok := strings.Cut(strings.TrimSpace(line), ": ")
		if !ok {
			continue
		}
		shown, err := shownPath(path)
		if err != nil {
			return false, false, err
		}
		if shown != target {
			continue
		}
		switch status {
		case "trusted":
			return true, true, nil
		case "untrusted":
			return false, true, nil
		default:
			return false, false, fmt.Errorf("mise trust --show: unknown status %q", status)
		}
	}
	return false, false, nil
}

func shownPath(path string) (string, error) {
	expanded, err := expandHome(path)
	if err != nil {
		return "", err
	}
	resolved, err := canonicalPath(expanded)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func showsTrusted(found, trusted bool) bool {
	if !found {
		return false
	}
	return trusted
}

func expandHome(path string) (string, error) {
	if !homePath(path) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}

func homePath(path string) bool {
	return path == "~" || strings.HasPrefix(path, "~/")
}

func persistedIgnored(dir string) (bool, error) {
	target, err := canonicalPath(dir)
	if err != nil {
		return false, err
	}
	state := miseStateDir()
	if state == "" {
		return false, nil
	}
	entries, err := os.ReadDir(filepath.Join(state, "ignored-configs"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if trustMetadata(entry.Name()) {
			continue
		}
		pointed, err := trustEntryPath(filepath.Join(state, "ignored-configs", entry.Name()))
		if err != nil {
			return false, err
		}
		resolved, err := canonicalPath(pointed)
		if err != nil {
			continue
		}
		if resolved == target {
			return true, nil
		}
	}
	return false, nil
}

func trustMetadata(name string) bool {
	return strings.HasSuffix(name, ".hash") || strings.HasSuffix(name, ".monorepo")
}

func trustEntryPath(path string) (string, error) {
	target, err := os.Readlink(path)
	if err != nil {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", readErr
		}
		return strings.TrimSpace(string(data)), nil
	}
	if filepath.IsAbs(target) {
		return target, nil
	}
	return filepath.Join(filepath.Dir(path), target), nil
}

func miseStateDir() string {
	if dir := os.Getenv("MISE_STATE_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "mise")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "mise")
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func worktreeRoot(ctx context.Context, repoPath string) (string, error) {
	out, err := gitOutput(ctx, repoPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
