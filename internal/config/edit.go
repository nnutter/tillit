package config

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nnutter/tillit/internal/repository"
)

// Scope chooses a config file. ScopeEffective is the merged settings.
type Scope string

const (
	ScopeEffective  Scope = "effective"
	ScopeGlobal     Scope = "global"
	ScopeRepository Scope = "repository"
	ScopeWorktree   Scope = "worktree"
)

// Show formats one config file, or the merged settings for ScopeEffective.
func Show(gitDir string, scope Scope) (string, error) {
	doc, err := documentFor(gitDir, scope)
	if err != nil {
		return "", err
	}
	return formatJSON(doc)
}

// Get prints one command argument per line.
func Get(gitDir string, scope Scope, key string) (string, error) {
	if err := checkKey(key); err != nil {
		return "", err
	}
	doc, err := documentFor(gitDir, scope)
	if err != nil {
		return "", err
	}
	command := documentCommand(doc)
	if command == nil {
		return "", unset(key)
	}
	if len(*command) == 0 {
		return "", nil
	}
	return strings.Join(*command, "\n") + "\n", nil
}

// Set replaces the command in one config file.
func Set(gitDir string, scope Scope, key string, values []string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if len(values) == 0 {
		return fmt.Errorf("config key %q needs a value", key)
	}
	return update(gitDir, scope, func(doc *document) error {
		*doc = commandDocument(slices.Clone(values))
		return nil
	})
}

// Append adds one command argument.
func Append(gitDir string, scope Scope, key, value string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	return update(gitDir, scope, func(doc *document) error {
		var command []string
		if current := documentCommand(*doc); current != nil {
			command = *current
		}
		*doc = commandDocument(append(command, value))
		return nil
	})
}

// Remove deletes the command, or all matching arguments when hasValue is set.
func Remove(gitDir string, scope Scope, key, value string, hasValue bool) error {
	if err := checkKey(key); err != nil {
		return err
	}
	return update(gitDir, scope, func(doc *document) error {
		current := documentCommand(*doc)
		if current == nil {
			return unset(key)
		}
		if !hasValue {
			*doc = document{}
			return nil
		}
		if !slices.Contains(*current, value) {
			return fmt.Errorf("config key %q has no match for %q", key, value)
		}
		command := slices.DeleteFunc(*current, func(arg string) bool { return arg == value })
		*doc = commandDocument(command)
		return nil
	})
}

func commandDocument(command []string) document {
	return document{Validate: new(validateDocument{Command: new(command)})}
}

func documentFor(gitDir string, scope Scope) (document, error) {
	if scope != ScopeEffective {
		return readScope(gitDir, scope)
	}
	cfg, err := Load(gitDir)
	if err != nil {
		return document{}, err
	}
	if len(cfg.Command) == 0 {
		return document{}, nil
	}
	return commandDocument(cfg.Command), nil
}

func readScope(gitDir string, scope Scope) (document, error) {
	path, err := scopePath(gitDir, scope)
	if err != nil {
		return document{}, err
	}
	return readDocument(path)
}

func readDocument(path string) (document, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return document{}, nil
	}
	if err != nil {
		return document{}, err
	}
	return decodeDocument(data, path)
}

func decodeDocument(data []byte, path string) (document, error) {
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return document{}, fmt.Errorf("read tillit config %s: %w", path, err)
	}
	var envelope struct {
		Auditor jsontext.Value `json:"auditor"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return document{}, fmt.Errorf("read tillit config %s: %w", path, err)
	}
	if len(envelope.Auditor) > 0 {
		return document{}, fmt.Errorf("read tillit config %s: auditor section was removed; move its settings to validate", path)
	}
	return doc, nil
}

func update(gitDir string, scope Scope, fn func(*document) error) error {
	path, err := scopePath(gitDir, scope)
	if err != nil {
		return err
	}
	doc, err := readDocument(path)
	if err != nil {
		return err
	}
	if err := fn(&doc); err != nil {
		return err
	}
	return writeDocument(path, doc)
}

func scopePath(gitDir string, scope Scope) (string, error) {
	switch scope {
	case ScopeGlobal:
		return globalPath()
	case ScopeRepository:
		return repositoryPath(gitDir)
	case ScopeWorktree:
		return filepath.Join(gitDir, "tillit", "config.json"), nil
	default:
		return "", fmt.Errorf("config scope %q is not global, repository, or worktree", scope)
	}
}

func globalPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "tillit", "config.json"), nil
}

func repositoryPath(gitDir string) (string, error) {
	common, err := repository.CommonDir(gitDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, "tillit", "config.json"), nil
}

func formatJSON(doc document) (string, error) {
	body, err := json.Marshal(&doc, jsontext.WithIndent("  "))
	if err != nil {
		return "", err
	}
	return string(body) + "\n", nil
}

func writeDocument(path string, doc document) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := formatJSON(doc)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write tillit config %s: %w", path, err)
	}
	return nil
}

func unset(key string) error {
	return fmt.Errorf("config key %q is unset", key)
}
