package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nnutter/tillit/internal/repository"
)

// State describes whether the worktree daemon is running and whether its
// binary matches the current executable.
type State struct {
	Running      bool
	NeedsRestart bool
}

var currentBinaryFingerprint = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	file, err := os.Open(exe)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return ""
	}
	return hex.EncodeToString(sum.Sum(nil))
})

func versionFilePath(gitDir string) string {
	return filepath.Join(gitDir, "tillit", "daemon.version")
}

func writeDaemonVersion(gitDir string) {
	fingerprint := currentBinaryFingerprint()
	if fingerprint == "" {
		return
	}
	path := versionFilePath(gitDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(fingerprint+"\n"), 0o600)
}

func removeDaemonVersion(gitDir string) {
	_ = os.Remove(versionFilePath(gitDir))
}

// Status reports whether a daemon holds the worktree lock and whether it is
// running an out-of-sync binary.
func Status(repoPath string) (State, error) {
	running, err := Running(repoPath)
	if err != nil {
		return State{}, err
	}
	if !running {
		return State{}, nil
	}
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return State{Running: true}, err
	}
	_ = repo.Close()
	return State{
		Running:      true,
		NeedsRestart: daemonVersionOutOfSync(gitDir),
	}, nil
}

func daemonVersionOutOfSync(gitDir string) bool {
	want := currentBinaryFingerprint()
	if want == "" {
		return false
	}
	data, err := os.ReadFile(versionFilePath(gitDir))
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) != want
}
