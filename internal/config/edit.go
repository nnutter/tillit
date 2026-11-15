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

	"github.com/nnutter/spor/internal/repository"
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

// Get prints one value. A list is one element per line.
func Get(gitDir string, scope Scope, key string) (string, error) {
	if _, err := kindOf(key); err != nil {
		return "", err
	}
	doc, err := documentFor(gitDir, scope)
	if err != nil {
		return "", err
	}
	return formatField(doc, key)
}

// Set writes a value into one config file.
// A list value replaces the whole list.
func Set(gitDir string, scope Scope, key string, values []string) error {
	kind, err := kindOf(key)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return fmt.Errorf("config key %q needs a value", key)
	}
	return update(gitDir, scope, func(doc *document) error {
		return assign(doc, kind, values)
	})
}

// Append adds one element to a list value.
func Append(gitDir string, scope Scope, key, value string) error {
	kind, err := kindOf(key)
	if err != nil {
		return err
	}
	if kind != kindCommand {
		return fmt.Errorf("%s is not a list", key)
	}
	return update(gitDir, scope, func(doc *document) error {
		current := commandOf(doc)
		current = append(current, value)
		ensureVerify(doc).Command = &current
		return nil
	})
}

// Remove deletes a key, or one matching list element when hasValue is set.
func Remove(gitDir string, scope Scope, key, value string, hasValue bool) error {
	kind, err := kindOf(key)
	if err != nil {
		return err
	}
	return update(gitDir, scope, func(doc *document) error {
		if !hasValue {
			return deleteKey(doc, kind)
		}
		if kind != kindCommand {
			return fmt.Errorf("%s is not a list", key)
		}
		return deleteValue(doc, key, value)
	})
}

type valueKind int

const (
	kindMode valueKind = iota
	kindCommand
)

func kindOf(key string) (valueKind, error) {
	field, ok := fieldByKey(key)
	if !ok {
		return 0, fmt.Errorf("unknown config key %q", key)
	}
	return kindByName(field.name, key)
}

func kindByName(name, key string) (valueKind, error) {
	switch name {
	case "Mode":
		return kindMode, nil
	case "Command":
		return kindCommand, nil
	default:
		return 0, fmt.Errorf("unknown config key %q", key)
	}
}

func documentFor(gitDir string, scope Scope) (document, error) {
	if scope == ScopeEffective {
		cfg, err := Load(gitDir)
		if err != nil {
			return document{}, err
		}
		return effectiveDocument(cfg), nil
	}
	return readScope(gitDir, scope)
}

func effectiveDocument(cfg Config) document {
	verify := &verifyDocument{Mode: string(cfg.Mode)}
	if len(cfg.Command) > 0 {
		command := slices.Clone(cfg.Command)
		verify.Command = &command
	}
	return document{Verify: verify}
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
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return document{}, fmt.Errorf("read spor config %s: %w", path, err)
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
		return filepath.Join(gitDir, "spor", "config.json"), nil
	default:
		return "", fmt.Errorf("config scope %q is not global, repository, or worktree", scope)
	}
}

func globalPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "spor", "config.json"), nil
}

func repositoryPath(gitDir string) (string, error) {
	common, err := repository.CommonDir(gitDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, "spor", "config.json"), nil
}

func assign(doc *document, kind valueKind, values []string) error {
	switch kind {
	case kindMode:
		return assignMode(doc, values)
	case kindCommand:
		command := slices.Clone(values)
		ensureVerify(doc).Command = &command
		return nil
	default:
		return fmt.Errorf("unknown config key")
	}
}

func assignMode(doc *document, values []string) error {
	if len(values) != 1 {
		return fmt.Errorf("%s accepts one value", kindName(kindMode))
	}
	mode, err := ParseMode(values[0])
	if err != nil {
		return err
	}
	ensureVerify(doc).Mode = string(mode)
	return nil
}

func deleteKey(doc *document, kind valueKind) error {
	if err := clearKey(doc, kind); err != nil {
		return err
	}
	if verifyEmpty(doc) {
		doc.Verify = nil
	}
	return nil
}

func clearKey(doc *document, kind valueKind) error {
	if doc.Verify == nil {
		return unset(kindName(kind))
	}
	switch kind {
	case kindMode:
		if doc.Verify.Mode == "" {
			return unset(kindName(kindMode))
		}
		doc.Verify.Mode = ""
		return nil
	case kindCommand:
		if doc.Verify.Command == nil {
			return unset(kindName(kindCommand))
		}
		doc.Verify.Command = nil
		return nil
	default:
		return fmt.Errorf("unknown config key")
	}
}

func deleteValue(doc *document, key, value string) error {
	if commandMissing(doc.Verify) {
		return unset(key)
	}
	next, found := withoutValue(*doc.Verify.Command, value)
	if !found {
		return fmt.Errorf("%q does not match a value of %s", value, key)
	}
	doc.Verify.Command = &next
	return nil
}

func withoutValue(values []string, value string) ([]string, bool) {
	found := false
	next := make([]string, 0, len(values))
	for _, item := range values {
		if item == value {
			found = true
			continue
		}
		next = append(next, item)
	}
	return next, found
}

func commandOf(doc *document) []string {
	if commandMissing(doc.Verify) {
		return nil
	}
	return slices.Clone(*doc.Verify.Command)
}

func commandMissing(verify *verifyDocument) bool {
	if verify == nil {
		return true
	}
	return verify.Command == nil
}

func modeMissing(verify *verifyDocument) bool {
	if verify == nil {
		return true
	}
	return verify.Mode == ""
}

func verifyEmpty(doc *document) bool {
	if doc.Verify == nil {
		return true
	}
	if doc.Verify.Mode != "" {
		return false
	}
	return doc.Verify.Command == nil
}

func ensureVerify(doc *document) *verifyDocument {
	if doc.Verify == nil {
		doc.Verify = &verifyDocument{}
	}
	return doc.Verify
}

func formatField(doc document, key string) (string, error) {
	kind, err := kindOf(key)
	if err != nil {
		return "", err
	}
	switch kind {
	case kindMode:
		return formatMode(doc)
	case kindCommand:
		return formatCommand(doc)
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

func formatMode(doc document) (string, error) {
	if modeMissing(doc.Verify) {
		return "", unset(kindName(kindMode))
	}
	return doc.Verify.Mode + "\n", nil
}

func formatCommand(doc document) (string, error) {
	if commandMissing(doc.Verify) {
		return "", unset(kindName(kindCommand))
	}
	return joinLines(*doc.Verify.Command), nil
}

func joinLines(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, "\n") + "\n"
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
		return fmt.Errorf("write spor config %s: %w", path, err)
	}
	return nil
}

func unset(key string) error {
	return fmt.Errorf("config key %q is unset", key)
}

func kindName(kind valueKind) string {
	switch kind {
	case kindMode:
		return keyByName("Mode")
	case kindCommand:
		return keyByName("Command")
	default:
		return ""
	}
}
