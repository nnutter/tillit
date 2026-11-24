// Package daemonlog writes bounded logs for a worktree's daemon.
package daemonlog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	maxSize = 5 * 1024 * 1024
	backups = 3
)

// Open opens the daemon log under the worktree's Git directory.
func Open(gitDir string) (*Log, error) {
	path := filepath.Join(gitDir, "tillit", "daemon.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create daemon log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open daemon log: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure daemon log: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stat daemon log: %w", err)
	}
	return &Log{file: file, path: path, size: info.Size()}, nil
}

// Log serializes writes and keeps at most three rotated backups.
type Log struct {
	mu   sync.Mutex
	file *os.File
	path string
	size int64
}

// Close closes the log.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

// Write splits large writes across files so no log exceeds five MiB.
func (l *Log) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var written int
	for len(p) > 0 {
		if l.size >= maxSize {
			if err := l.rotate(); err != nil {
				return written, err
			}
		}
		chunk := min(len(p), int(maxSize-l.size))
		n, err := l.file.Write(p[:chunk])
		l.size += int64(n)
		written += n
		p = p[n:]
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func (l *Log) rotate() error {
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("close daemon log for rotation: %w", err)
	}
	for i := backups - 1; i > 0; i-- {
		if err := renameIfExists(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1)); err != nil {
			return err
		}
	}
	if err := os.Rename(l.path, l.path+".1"); err != nil {
		return fmt.Errorf("rotate daemon log: %w", err)
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open rotated daemon log: %w", err)
	}
	l.file = file
	l.size = 0
	return nil
}

func renameIfExists(old, new string) error {
	if err := os.Rename(old, new); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("rotate daemon log backup: %w", err)
	}
	return nil
}
