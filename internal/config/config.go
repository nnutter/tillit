// Package config loads tillit's layered configuration.
package config

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/nnutter/tillit/internal/repository"
)

// Config is the effective validation configuration.
type Config struct {
	Command []string
}

type document struct {
	Validate *validateDocument `json:"validate,omitzero"`
}

type validateDocument struct {
	// An empty command clears commands inherited from an earlier file.
	Command *[]string `json:"command,omitzero"`
}

// Load reads the global, repository, worktree Git directory, and worktree root config files.
// A later file replaces only the fields it sets.
func Load(gitDir string) (Config, error) {
	root, err := repository.WorktreeRoot(gitDir)
	if err != nil {
		return Config{}, err
	}
	return LoadAt(gitDir, root)
}

// LoadAt merges external configuration from sourceGitDir with root's config.
func LoadAt(sourceGitDir, root string) (Config, error) {
	cfg, err := loadExternal(sourceGitDir)
	if err != nil {
		return Config{}, err
	}
	return apply(cfg, filepath.Join(root, ".tillit", "config.json"))
}

// LoadHistorical merges external settings with a historical root config, if present.
func LoadHistorical(sourceGitDir string, data []byte) (Config, error) {
	cfg, err := loadExternal(sourceGitDir)
	if err != nil {
		return cfg, err
	}
	if data == nil {
		return cfg, nil
	}
	doc, err := decodeDocument(data, ".tillit/config.json")
	if err != nil {
		return Config{}, err
	}
	return applyDocument(cfg, doc), nil
}

func loadExternal(gitDir string) (Config, error) {
	var cfg Config
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	cfg, err = apply(cfg, filepath.Join(home, ".config", "tillit", "config.json"))
	if err != nil {
		return Config{}, err
	}
	common, err := repository.CommonDir(gitDir)
	if err != nil {
		return Config{}, err
	}
	cfg, err = apply(cfg, filepath.Join(common, "tillit", "config.json"))
	if err != nil {
		return Config{}, err
	}
	if common != gitDir {
		cfg, err = apply(cfg, filepath.Join(gitDir, "tillit", "config.json"))
		if err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

func apply(cfg Config, path string) (Config, error) {
	doc, err := readDocument(path)
	if err != nil {
		return Config{}, err
	}
	return applyDocument(cfg, doc), nil
}

func applyDocument(cfg Config, doc document) Config {
	if command := documentCommand(doc); command != nil {
		cfg.Command = slices.Clone(*command)
	}
	return cfg
}

func documentCommand(doc document) *[]string {
	if doc.Validate == nil {
		return nil
	}
	return doc.Validate.Command
}
