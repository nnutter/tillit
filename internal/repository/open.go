// Package repository opens the git worktree spor is acting on.
package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

// Open resolves the worktree at start and opens it.
// The returned directory is that worktree's git dir.
func Open(start string) (*git.Repository, string, error) {
	gitDir, err := findGitDir(start)
	if err != nil {
		return nil, "", err
	}
	if os.Getenv("GIT_DIR") != "" {
		repo, err := openGitDir(gitDir, worktreePath(start))
		if err != nil {
			return nil, "", err
		}
		return repo, gitDir, nil
	}
	repo, err := git.PlainOpenWithOptions(start, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, "", err
	}
	return repo, gitDir, nil
}

// HeadRef is the ref that points at the worktree's latest spor commits.
func HeadRef(gitDir string) string {
	return "refs/spor/" + worktreeKey(gitDir) + "/head"
}

// IDRef is the ref that keeps one submission's commits.
func IDRef(gitDir, sporID string) string {
	return "refs/spor/" + worktreeKey(gitDir) + "/" + sporID
}

// CommonDir returns the git common dir for a worktree git dir.
// A repository that is not a linked worktree is its own common dir.
func CommonDir(gitDir string) (string, error) {
	path, ok, err := commonPath(gitDir)
	if err != nil {
		return "", err
	}
	if !ok {
		return gitDir, nil
	}
	return path, nil
}

// WorktreeRoot returns the worktree root associated with a Git directory.
func WorktreeRoot(gitDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(gitDir, "gitdir"))
	if errors.Is(err, os.ErrNotExist) {
		return filepath.Dir(gitDir), nil
	}
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(data))
	if path == "" {
		return "", fmt.Errorf("empty gitdir file in %s", gitDir)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(gitDir, path)
	}
	return filepath.Dir(path), nil
}

func commonPath(gitDir string) (string, bool, error) {
	data, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	path := strings.TrimSpace(string(data))
	if path == "" {
		return "", false, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(gitDir, path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	return path, true, nil
}

func worktreeKey(gitDir string) string {
	sum := sha256.Sum256([]byte(gitDir))
	return hex.EncodeToString(sum[:])
}

func worktreePath(start string) string {
	if wt := os.Getenv("GIT_WORK_TREE"); wt != "" {
		return wt
	}
	return start
}

func findGitDir(start string) (string, error) {
	if dir := os.Getenv("GIT_DIR"); dir != "" {
		return filepath.Abs(dir)
	}
	return walkGitDir(start)
}

func walkGitDir(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		gitDir, ok, err := gitDirAt(dir)
		if err != nil {
			return "", err
		}
		if ok {
			return gitDir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no git repository at %s", start)
		}
		dir = parent
	}
}

func gitDirAt(dir string) (string, bool, error) {
	path := filepath.Join(dir, ".git")
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if info.IsDir() {
		return path, true, nil
	}
	gitDir, err := readGitDirFile(dir, path)
	if err != nil {
		return "", false, err
	}
	return gitDir, true, nil
}

func readGitDirFile(dir, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(data))
	gitDir, ok := strings.CutPrefix(line, "gitdir: ")
	if !ok {
		return "", fmt.Errorf("%s is not a gitdir file", path)
	}
	gitDir = strings.TrimSpace(gitDir)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(dir, gitDir)
	}
	return filepath.Abs(gitDir)
}

func openGitDir(gitDir, worktree string) (*git.Repository, error) {
	gitDir, err := filepath.Abs(gitDir)
	if err != nil {
		return nil, err
	}
	worktree, err = filepath.Abs(worktree)
	if err != nil {
		return nil, err
	}
	dot := osfs.New(gitDir, osfs.WithBoundOS())
	common, err := commonFilesystem(gitDir)
	if err != nil {
		return nil, err
	}
	storage := filesystem.NewStorage(dotgit.NewRepositoryFilesystem(dot, common), cache.NewObjectLRUDefault())
	return git.Open(storage, osfs.New(worktree, osfs.WithBoundOS()))
}

func commonFilesystem(gitDir string) (billy.Filesystem, error) {
	path, ok, err := commonPath(gitDir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return osfs.New(path, osfs.WithBoundOS()), nil
}
