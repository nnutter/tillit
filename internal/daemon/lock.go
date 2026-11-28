package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/nnutter/tillit/internal/repository"
)

// ErrAlreadyRunning reports that another daemon already owns this worktree.
var ErrAlreadyRunning = errors.New("tillit daemon is already running")

// worktreeLockPath is the lock file beside the tillit worktree.
// It sits outside the checkout so a force checkout cannot remove it.
func worktreeLockPath(gitDir string) (string, error) {
	checkout, err := repository.CheckoutDir(gitDir)
	if err != nil {
		return "", err
	}
	return checkout + ".lock", nil
}

// Running reports whether a daemon holds the worktree lock.
func Running(repoPath string) (bool, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return false, err
	}
	_ = repo.Close()
	path, err := worktreeLockPath(gitDir)
	if err != nil {
		return false, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	if supervisorBusy(err) {
		return true, nil
	}
	return false, err
}

func acquireWorktreeLock(repoPath string) (*os.File, string, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return nil, "", err
	}
	_ = repo.Close()
	lockPath, err := worktreeLockPath(gitDir)
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, "", err
	}
	stagedVersion := stageDaemonVersion(gitDir)
	defer func() { removeStagedVersion(stagedVersion) }()
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, "", err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, "", fmt.Errorf("%w: worktree %s lock %s", ErrAlreadyRunning, gitDir, lockPath)
		}
		return nil, "", err
	}
	publishDaemonVersion(stagedVersion, gitDir)
	stagedVersion = ""
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	if _, err := f.WriteString(fmt.Sprintf("%d\n", os.Getpid())); err != nil {
		// A failed write does not drop the lock; the file stays open.
		_ = err
	}
	_ = f.Sync()
	return f, gitDir, nil
}

func releaseWorktreeLock(f *os.File, gitDir string) {
	if gitDir != "" {
		removeDaemonVersion(gitDir)
	}
	if f == nil {
		return
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}
