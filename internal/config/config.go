// Package config loads tillit's layered configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/nnutter/tillit/internal/repository"
)

// Mode selects which recorded identity counts as already validated.
type Mode string

const (
	ModeCommit Mode = "commit"
	ModePatch  Mode = "patch"
	ModeChange Mode = "change"
)

// Auditor is the fixup settings for one worktree.
type Auditor struct {
	Provider string
	Model    string
	Thinking Thinking
}

// Config is the validation settings for one worktree.
type Config struct {
	Command []string
	Mode    Mode
	Auditor Auditor
	Mender  Auditor
}

type document struct {
	Auditor *auditorDocument `json:"auditor,omitempty"`
	Mender  *menderDocument  `json:"mender,omitempty"`
}

type menderDocument struct {
	Conflict *fixupDocument `json:"conflict,omitempty"`
}

type validateDocument struct {
	// An empty command is stored so this file clears commands inherited from an earlier file.
	Command *[]string `json:"command,omitzero"`
	Mode    string    `json:"mode,omitempty"`
}

type auditorDocument struct {
	Validate *validateDocument `json:"validate,omitempty"`
	Fixup    *fixupDocument    `json:"fixup,omitempty"`
}

type fixupDocument struct {
	Model *fixupModel `json:"model,omitempty"`
}

type fixupModel struct {
	Provider string `json:"provider,omitempty"`
	Version  string `json:"version,omitempty"`
	Effort   string `json:"effort,omitempty"`
}

// Load reads the global, repository, worktree Git directory, and worktree root config files.
// A later file replaces only the fields it sets. The mode defaults to change.
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
	return applyDocument(cfg, doc)
}

func loadExternal(gitDir string) (Config, error) {
	cfg := Config{Mode: ModeChange, Auditor: Auditor{Thinking: ThinkingLow}, Mender: Auditor{Thinking: ThinkingLow}}
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

// ParseMode accepts commit, patch, or change.
func ParseMode(value string) (Mode, error) {
	switch Mode(value) {
	case ModeCommit, ModePatch, ModeChange:
		return Mode(value), nil
	default:
		return "", fmt.Errorf("validate mode %q is not commit, patch, or change", value)
	}
}

func apply(cfg Config, path string) (Config, error) {
	doc, err := readDocument(path)
	if err != nil {
		return Config{}, err
	}
	return applyDocument(cfg, doc)
}

func applyDocument(cfg Config, doc document) (Config, error) {
	if err := applyValidate(&cfg, doc); err != nil {
		return Config{}, err
	}
	if err := applyAuditor(&cfg, doc); err != nil {
		return Config{}, err
	}
	if err := applyModel(&cfg.Mender, menderModel(doc)); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyValidate(cfg *Config, doc document) error {
	validate := docAuditorValidate(doc)
	if validate == nil {
		return nil
	}
	if validate.Command != nil {
		cfg.Command = slices.Clone(*validate.Command)
	}
	if validate.Mode == "" {
		return nil
	}
	mode, err := ParseMode(validate.Mode)
	if err != nil {
		return err
	}
	cfg.Mode = mode
	return nil
}

func applyAuditor(cfg *Config, doc document) error {
	return applyModel(&cfg.Auditor, docFixupModel(doc))
}

func applyModel(target *Auditor, model *fixupModel) error {
	if model == nil {
		return nil
	}
	if model.Provider != "" {
		target.Provider = model.Provider
	}
	if model.Version != "" {
		target.Model = model.Version
	}
	if model.Effort == "" {
		return nil
	}
	thinking, err := ParseThinking(model.Effort)
	if err != nil {
		return err
	}
	target.Thinking = thinking
	return nil
}

func docAuditorValidate(doc document) *validateDocument {
	if doc.Auditor == nil {
		return nil
	}
	return doc.Auditor.Validate
}

func docFixupModel(doc document) *fixupModel {
	if doc.Auditor == nil {
		return nil
	}
	if doc.Auditor.Fixup == nil {
		return nil
	}
	return doc.Auditor.Fixup.Model
}
