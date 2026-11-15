// Package config loads spor's layered configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/nnutter/spor/internal/repository"
)

// Mode selects which recorded identity counts as already verified.
type Mode string

const (
	ModeCommit Mode = "commit"
	ModePatch  Mode = "patch"
	ModeChange Mode = "change"
)

// Config is the verification settings for one worktree.
type Config struct {
	Command []string
	Mode    Mode
}

type document struct {
	Verify *verifyDocument `json:"verify,omitempty"`
}

type verifyDocument struct {
	// An empty command is stored so this file clears commands inherited from an earlier file.
	Command *[]string `json:"command,omitzero"`
	Mode    string    `json:"mode,omitempty"`
}

// Load reads the global, repository, and worktree config files.
// A later file replaces only the fields it sets. The mode defaults to change.
func Load(gitDir string) (Config, error) {
	cfg := Config{Mode: ModeChange}
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	cfg, err = apply(cfg, filepath.Join(home, ".config", "spor", "config.json"))
	if err != nil {
		return Config{}, err
	}
	common, err := repository.CommonDir(gitDir)
	if err != nil {
		return Config{}, err
	}
	cfg, err = apply(cfg, filepath.Join(common, "spor", "config.json"))
	if err != nil {
		return Config{}, err
	}
	if common == gitDir {
		return cfg, nil
	}
	return apply(cfg, filepath.Join(gitDir, "spor", "config.json"))
}

// ParseMode accepts commit, patch, or change.
func ParseMode(value string) (Mode, error) {
	switch Mode(value) {
	case ModeCommit, ModePatch, ModeChange:
		return Mode(value), nil
	default:
		return "", fmt.Errorf("verify mode %q is not commit, patch, or change", value)
	}
}

func apply(cfg Config, path string) (Config, error) {
	doc, err := readDocument(path)
	if err != nil {
		return Config{}, err
	}
	if doc.Verify == nil {
		return cfg, nil
	}
	if doc.Verify.Command != nil {
		cfg.Command = slices.Clone(*doc.Verify.Command)
	}
	if doc.Verify.Mode == "" {
		return cfg, nil
	}
	mode, err := ParseMode(doc.Verify.Mode)
	if err != nil {
		return Config{}, err
	}
	cfg.Mode = mode
	return cfg, nil
}
