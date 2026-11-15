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

// Auditor is the fixup settings for one worktree.
type Auditor struct {
	Provider string
	Model    string
	Thinking Thinking
}

// Config is the verification settings for one worktree.
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

type verifyDocument struct {
	// An empty command is stored so this file clears commands inherited from an earlier file.
	Command *[]string `json:"command,omitzero"`
	Mode    string    `json:"mode,omitempty"`
}

type auditorDocument struct {
	Verify *verifyDocument `json:"verify,omitempty"`
	Fixup  *fixupDocument  `json:"fixup,omitempty"`
}

type fixupDocument struct {
	Model *fixupModel `json:"model,omitempty"`
}

type fixupModel struct {
	Provider string `json:"provider,omitempty"`
	Version  string `json:"version,omitempty"`
	Effort   string `json:"effort,omitempty"`
}

// Load reads the global, repository, and worktree config files.
// A later file replaces only the fields it sets. The mode defaults to change.
func Load(gitDir string) (Config, error) {
	cfg := Config{Mode: ModeChange, Auditor: Auditor{Thinking: ThinkingLow}, Mender: Auditor{Thinking: ThinkingLow}}
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
	if err := applyVerify(&cfg, doc); err != nil {
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

func applyVerify(cfg *Config, doc document) error {
	verify := docAuditorVerify(doc)
	if verify == nil {
		return nil
	}
	if verify.Command != nil {
		cfg.Command = slices.Clone(*verify.Command)
	}
	if verify.Mode == "" {
		return nil
	}
	mode, err := ParseMode(verify.Mode)
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

func docAuditorVerify(doc document) *verifyDocument {
	if doc.Auditor == nil {
		return nil
	}
	return doc.Auditor.Verify
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
