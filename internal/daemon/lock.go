package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/nnutter/spor/internal/repository"
)

// ErrAlreadyRunning reports that another daemon already owns this worktree.
var ErrAlreadyRunning = errors.New("spor daemon is already running")

// worktreeLockPath is the lock file beside the spor worktree.
// It sits outside the checkout so a force checkout cannot remove it.
func worktreeLockPath(gitDir string) (string, error) {
	checkout, err := repository.CheckoutDir(gitDir)
	if err != nil {
		return "", err
	}
	return checkout + ".lock", nil
}

func acquireWorktreeLock(repoPath string) (*os.File, error) {
	_, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return nil, err
	}
	lockPath, err := worktreeLockPath(gitDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("%w: worktree %s lock %s", ErrAlreadyRunning, gitDir, lockPath)
		}
		return nil, err
	}
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	if _, err := f.WriteString(fmt.Sprintf("%d\n", os.Getpid())); err != nil {
		// A failed write does not drop the lock; the file stays open.
		_ = err
	}
	_ = f.Sync()
	return f, nil
}

func releaseWorktreeLock(f *os.File) {
	if f == nil {
		return
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}
