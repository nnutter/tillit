package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrWriterBusy means another Spor operation owns the worktree's writer lock.
var ErrWriterBusy = errors.New("another Spor operation is running; try again")

// AcquireWriter serializes worktree state mutations across Spor processes.
func AcquireWriter(repoPath string) (*os.File, error) {
	repo, gitDir, err := Open(repoPath)
	if err != nil {
		return nil, err
	}
	_ = repo.Close()
	dir := filepath.Join(gitDir, "spor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dir, "writer.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrWriterBusy
		}
		return nil, fmt.Errorf("acquire Spor writer lock: %w", err)
	}
	return file, nil
}

// ReleaseWriter releases an acquired writer lock.
func ReleaseWriter(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}
