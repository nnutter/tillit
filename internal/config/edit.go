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
	if strings.HasPrefix(key, "mender.conflict.model.") {
		return getMender(gitDir, scope, key)
	}
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
	if strings.HasPrefix(key, "mender.conflict.model.") {
		return setMender(gitDir, scope, key, values)
	}
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
	if strings.HasPrefix(key, "mender.conflict.model.") {
		return removeMender(gitDir, scope, key, hasValue)
	}
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
	kindFixupProvider
	kindFixupVersion
	kindFixupEffort
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
	case "Provider":
		return kindFixupProvider, nil
	case "Version":
		return kindFixupVersion, nil
	case "Effort":
		return kindFixupEffort, nil
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

func fixupModelIsSet(model *fixupModel) bool {
	if model.Provider == "" {
		if model.Version == "" {
			return model.Effort != ""
		}
		return true
	}
	return true
}

func effectiveDocument(cfg Config) document {
	verify := &verifyDocument{Mode: string(cfg.Mode)}
	if len(cfg.Command) > 0 {
		command := slices.Clone(cfg.Command)
		verify.Command = &command
	}
	model := &fixupModel{}
	if cfg.Auditor.Provider != "" {
		model.Provider = cfg.Auditor.Provider
	}
	if cfg.Auditor.Model != "" {
		model.Version = cfg.Auditor.Model
	}
	if cfg.Auditor.Thinking != nil {
		model.Effort = cfg.Auditor.Thinking.String()
	}
	var fixup *fixupDocument
	if fixupModelIsSet(model) {
		fixup = &fixupDocument{Model: model}
	}
	mender := &fixupModel{Provider: cfg.Mender.Provider, Version: cfg.Mender.Model}
	if cfg.Mender.Thinking != nil {
		mender.Effort = cfg.Mender.Thinking.String()
	}
	return document{Auditor: &auditorDocument{Verify: verify, Fixup: fixup}, Mender: &menderDocument{Conflict: &fixupDocument{Model: mender}}}
}

func menderModel(doc document) *fixupModel {
	if doc.Mender == nil {
		return nil
	}
	if doc.Mender.Conflict == nil {
		return nil
	}
	return doc.Mender.Conflict.Model
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
	case kindFixupProvider:
		return assignSingle(doc, values, func(doc *document, value string) {
			ensureFixupModel(doc).Provider = value
		})
	case kindFixupVersion:
		return assignSingle(doc, values, func(doc *document, value string) {
			ensureFixupModel(doc).Version = value
		})
	case kindFixupEffort:
		return assignSingle(doc, values, func(doc *document, value string) {
			ensureFixupModel(doc).Effort = value
		})
	default:
		return fmt.Errorf("unknown config key")
	}
}

func assignSingle(doc *document, values []string, set func(*document, string)) error {
	if len(values) != 1 {
		return fmt.Errorf("%s accepts one value", "auditor setting")
	}
	set(doc, values[0])
	return nil
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
	if auditorVerifyEmpty(doc) {
		doc.Auditor.Verify = nil
	}
	if fixupEmpty(doc) {
		doc.Auditor.Fixup = nil
	}
	if auditorEmpty(doc) {
		doc.Auditor = nil
	}
	return nil
}

func clearKey(doc *document, kind valueKind) error {
	switch kind {
	case kindMode:
		return clearMode(doc)
	case kindCommand:
		return clearCommand(doc)
	case kindFixupProvider:
		return clearProvider(doc)
	case kindFixupVersion:
		return clearVersion(doc)
	case kindFixupEffort:
		return clearEffort(doc)
	default:
		return fmt.Errorf("unknown config key")
	}
}

func clearMode(doc *document) error {
	verify := docAuditorVerify(*doc)
	if verify == nil {
		return unset(kindName(kindMode))
	}
	if verify.Mode == "" {
		return unset(kindName(kindMode))
	}
	verify.Mode = ""
	return nil
}

func clearCommand(doc *document) error {
	verify := docAuditorVerify(*doc)
	if verify == nil {
		return unset(kindName(kindCommand))
	}
	if verify.Command == nil {
		return unset(kindName(kindCommand))
	}
	verify.Command = nil
	return nil
}

func clearProvider(doc *document) error {
	model := docFixupModel(*doc)
	if model == nil {
		return unset(kindName(kindFixupProvider))
	}
	if model.Provider == "" {
		return unset(kindName(kindFixupProvider))
	}
	model.Provider = ""
	return nil
}

func clearVersion(doc *document) error {
	model := docFixupModel(*doc)
	if model == nil {
		return unset(kindName(kindFixupVersion))
	}
	if model.Version == "" {
		return unset(kindName(kindFixupVersion))
	}
	model.Version = ""
	return nil
}

func clearEffort(doc *document) error {
	model := docFixupModel(*doc)
	if model == nil {
		return unset(kindName(kindFixupEffort))
	}
	if model.Effort == "" {
		return unset(kindName(kindFixupEffort))
	}
	model.Effort = ""
	return nil
}

func deleteValue(doc *document, key, value string) error {
	verify := docAuditorVerify(*doc)
	if verify == nil {
		return unset(key)
	}
	if verify.Command == nil {
		return unset(key)
	}
	next, found := withoutValue(*verify.Command, value)
	if !found {
		return fmt.Errorf("%q does not match a value of %s", value, key)
	}
	verify.Command = &next
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
	verify := docAuditorVerify(*doc)
	if verify == nil {
		return nil
	}
	if verify.Command == nil {
		return nil
	}
	return slices.Clone(*verify.Command)
}

func auditorVerifyEmpty(doc *document) bool {
	verify := docAuditorVerify(*doc)
	if verify == nil {
		return true
	}
	if verify.Mode != "" {
		return false
	}
	return verify.Command == nil
}

func fixupEmpty(doc *document) bool {
	if doc.Auditor == nil {
		return true
	}
	if doc.Auditor.Fixup == nil {
		return true
	}
	if doc.Auditor.Fixup.Model == nil {
		return true
	}
	return fixupModelIsEmpty(doc.Auditor.Fixup.Model)
}

func fixupModelIsEmpty(model *fixupModel) bool {
	if model.Provider == "" {
		if model.Version == "" {
			return model.Effort == ""
		}
		return false
	}
	return false
}

func auditorEmpty(doc *document) bool {
	if doc.Auditor == nil {
		return true
	}
	if !auditorVerifyEmpty(doc) {
		return false
	}
	return fixupEmpty(doc)
}

func ensureVerify(doc *document) *verifyDocument {
	if doc.Auditor == nil {
		doc.Auditor = &auditorDocument{}
	}
	if doc.Auditor.Verify == nil {
		doc.Auditor.Verify = &verifyDocument{}
	}
	return doc.Auditor.Verify
}

func ensureFixupModel(doc *document) *fixupModel {
	if doc.Auditor == nil {
		doc.Auditor = &auditorDocument{}
	}
	if doc.Auditor.Fixup == nil {
		doc.Auditor.Fixup = &fixupDocument{}
	}
	if doc.Auditor.Fixup.Model == nil {
		doc.Auditor.Fixup.Model = &fixupModel{}
	}
	return doc.Auditor.Fixup.Model
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
	case kindFixupProvider:
		return formatFixupString(doc, key)
	case kindFixupVersion:
		return formatFixupString(doc, key)
	case kindFixupEffort:
		return formatFixupString(doc, key)
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

func formatFixupString(doc document, key string) (string, error) {
	model := docFixupModel(doc)
	if model == nil {
		return "", unset(key)
	}
	kind, _ := kindOf(key)
	switch kind {
	case kindFixupProvider:
		if model.Provider == "" {
			return "", unset(key)
		}
		return model.Provider + "\n", nil
	case kindFixupVersion:
		if model.Version == "" {
			return "", unset(key)
		}
		return model.Version + "\n", nil
	case kindFixupEffort:
		if model.Effort == "" {
			return "", unset(key)
		}
		return model.Effort + "\n", nil
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

func formatMode(doc document) (string, error) {
	verify := docAuditorVerify(doc)
	if verify == nil {
		return "", unset(kindName(kindMode))
	}
	if verify.Mode == "" {
		return "", unset(kindName(kindMode))
	}
	return verify.Mode + "\n", nil
}

func formatCommand(doc document) (string, error) {
	verify := docAuditorVerify(doc)
	if verify == nil {
		return "", unset(kindName(kindCommand))
	}
	if verify.Command == nil {
		return "", unset(kindName(kindCommand))
	}
	return joinLines(*verify.Command), nil
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
	case kindFixupProvider:
		return keyByName("Provider")
	case kindFixupVersion:
		return keyByName("Version")
	case kindFixupEffort:
		return keyByName("Effort")
	default:
		return ""
	}
}

func getMender(gitDir string, scope Scope, key string) (string, error) {
	doc, err := documentFor(gitDir, scope)
	if err != nil {
		return "", err
	}
	model := menderModel(doc)
	if model == nil {
		return "", unset(key)
	}
	var value string
	switch key {
	case "mender.conflict.model.provider":
		value = model.Provider
	case "mender.conflict.model.version":
		value = model.Version
	case "mender.conflict.model.effort":
		value = model.Effort
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
	if value == "" {
		return "", unset(key)
	}
	return value + "\n", nil
}

func setMender(gitDir string, scope Scope, key string, values []string) error {
	if len(values) != 1 {
		return fmt.Errorf("%s accepts one value", key)
	}
	return update(gitDir, scope, func(doc *document) error {
		if doc.Mender == nil {
			doc.Mender = new(menderDocument)
		}
		if doc.Mender.Conflict == nil {
			doc.Mender.Conflict = new(fixupDocument)
		}
		if doc.Mender.Conflict.Model == nil {
			doc.Mender.Conflict.Model = new(fixupModel)
		}
		model := doc.Mender.Conflict.Model
		switch key {
		case "mender.conflict.model.provider":
			model.Provider = values[0]
		case "mender.conflict.model.version":
			model.Version = values[0]
		case "mender.conflict.model.effort":
			if _, err := ParseThinking(values[0]); err != nil {
				return err
			}
			model.Effort = values[0]
		default:
			return fmt.Errorf("unknown config key %q", key)
		}
		return nil
	})
}

func removeMender(gitDir string, scope Scope, key string, hasValue bool) error {
	if hasValue {
		return fmt.Errorf("%s is not a list", key)
	}
	return update(gitDir, scope, func(doc *document) error {
		model := menderModel(*doc)
		if model == nil {
			return unset(key)
		}
		var field *string
		switch key {
		case "mender.conflict.model.provider":
			field = &model.Provider
		case "mender.conflict.model.version":
			field = &model.Version
		case "mender.conflict.model.effort":
			field = &model.Effort
		default:
			return fmt.Errorf("unknown config key %q", key)
		}
		if *field == "" {
			return unset(key)
		}
		*field = ""
		if fixupModelIsEmpty(model) {
			doc.Mender = nil
		}
		return nil
	})
}
