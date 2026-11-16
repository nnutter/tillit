package gh

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CurrentBranch returns the checked-out branch name for the worktree at repoPath.
func CurrentBranch(ctx context.Context, repoPath string) (string, error) {
	branch, err := gitOutput(ctx, repoPath, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read current branch: %w", err)
	}
	if branch == "" {
		return "", fmt.Errorf("read current branch: detached HEAD; check out a branch")
	}
	return branch, nil
}

// OwnerRepo resolves the repository owner and name from the origin remote.
func OwnerRepo(ctx context.Context, repoPath string) (string, string, error) {
	remote, err := gitOutput(ctx, repoPath, "remote", "get-url", "origin")
	if err != nil {
		return "", "", fmt.Errorf("read origin remote: %w", err)
	}
	owner, name, err := ParseRemoteURL(remote)
	if err != nil {
		return "", "", err
	}
	return owner, name, nil
}

// ParseRemoteURL extracts the owner and repository name from a git remote URL.
func ParseRemoteURL(raw string) (string, string, error) {
	trimmed := normalizeRemote(raw)
	if trimmed == "" {
		return "", "", fmt.Errorf("origin remote is empty")
	}
	if isHTTPRemote(trimmed) {
		return parseHTTPRemote(trimmed, raw)
	}
	if isSSHRemote(trimmed) {
		return parseSSHRemote(trimmed, raw)
	}
	return parseSCPRemote(trimmed, raw)
}

func normalizeRemote(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(trimmed, ".git")
	return strings.TrimSuffix(trimmed, "/")
}

func isHTTPRemote(remote string) bool {
	return strings.HasPrefix(remote, "https://") || strings.HasPrefix(remote, "http://")
}

func isSSHRemote(remote string) bool {
	return strings.HasPrefix(remote, "ssh://")
}

func parseHTTPRemote(trimmed, raw string) (string, string, error) {
	rest := strings.TrimPrefix(strings.TrimPrefix(trimmed, "https://"), "http://")
	host, path, _ := strings.Cut(rest, "/")
	if hostWithoutUser(host) != "github.com" {
		return "", "", fmt.Errorf("origin host %q is not github.com", host)
	}
	return splitOwnerName(path, raw)
}

func hostWithoutUser(host string) string {
	if _, bare, found := strings.Cut(host, "@"); found {
		return bare
	}
	return host
}

func parseSSHRemote(trimmed, raw string) (string, string, error) {
	rest := strings.TrimPrefix(trimmed, "ssh://")
	rest = strings.TrimPrefix(rest, "git@")
	host, path, _ := strings.Cut(rest, "/")
	if host != "github.com" {
		return "", "", fmt.Errorf("origin host %q is not github.com", host)
	}
	return splitOwnerName(path, raw)
}

func parseSCPRemote(trimmed, raw string) (string, string, error) {
	userHost, path, found := strings.Cut(trimmed, ":")
	if !found {
		return "", "", fmt.Errorf("parse origin remote %q: want a github.com URL", raw)
	}
	_, host, hasUser := strings.Cut(userHost, "@")
	if !hasUser {
		return "", "", fmt.Errorf("parse origin remote %q: want user@host:path", raw)
	}
	if host != "github.com" {
		return "", "", fmt.Errorf("origin host %q is not github.com", host)
	}
	return splitOwnerName(strings.TrimPrefix(path, "/"), raw)
}

func splitOwnerName(path, raw string) (string, string, error) {
	owner, name, ok := strings.Cut(path, "/")
	if !ok || isEmpty(owner) || isEmpty(name) || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("parse origin remote %q: want owner/name", raw)
	}
	return owner, name, nil
}

func isEmpty(value string) bool {
	return value == ""
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE",
			"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
			"GIT_PREFIX", "GIT_EXEC_PATH":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
