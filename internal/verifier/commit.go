package verifier

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/nnutter/commitstack"
)

type treeEntry struct {
	mode   string
	object string
}

// VerifyCommit compares the immutable trees of a commit and its only parent.
// hash must be a full hexadecimal Git object ID. Unsupported commit shapes
// return an Unsupported result; Git and cancellation failures return an error.
func VerifyCommit(ctx context.Context, repo, hash string) (Result, error) {
	if !validHashLength(hash) {
		return Result{}, fmt.Errorf("commit hash must be a full object ID")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return Result{}, fmt.Errorf("invalid commit hash: %w", err)
	}
	commit, err := gitOutput(ctx, repo, "cat-file", "commit", hash)
	if err != nil {
		return Result{}, err
	}
	parents := commitParents(commit)
	if len(parents) != 1 {
		return unsupportedCommit("commit must have exactly one parent"), nil
	}
	return verifyChange(ctx, repo, parents[0], hash)
}

// VerifyStack compares the immutable trees before a commit stack's original
// commit and at its tip, so attached fixups count as part of one change.
func VerifyStack(ctx context.Context, repo string, stack commitstack.Stack) (Result, error) {
	if !validHashLength(stack.Base) || !validHashLength(stack.Tip) {
		return Result{}, fmt.Errorf("stack must use full object IDs")
	}
	return verifyChange(ctx, repo, stack.Base, stack.Tip)
}

func verifyChange(ctx context.Context, repo, parent, hash string) (Result, error) {
	before, err := readTree(ctx, repo, parent)
	if err != nil {
		return Result{}, err
	}
	after, err := readTree(ctx, repo, hash)
	if err != nil {
		return Result{}, err
	}
	changed, reason := eligibleChange(before, after)
	if reason != "" {
		return unsupportedCommit(reason), nil
	}
	oldBlob, err := gitOutput(ctx, repo, "cat-file", "blob", before[changed].object)
	if err != nil {
		return Result{}, err
	}
	newBlob, err := gitOutput(ctx, repo, "cat-file", "blob", after[changed].object)
	if err != nil {
		return Result{}, err
	}
	return Verify(changed, oldBlob, newBlob), nil
}

func validHashLength(hash string) bool {
	switch len(hash) {
	case 40, 64:
		return true
	default:
		return false
	}
}

func commitParents(commit []byte) []string {
	var parents []string
	for line := range strings.SplitSeq(string(commit), "\n") {
		if line == "" {
			break
		}
		if parent, ok := strings.CutPrefix(line, "parent "); ok {
			parents = append(parents, parent)
		}
	}
	return parents
}

func eligibleChange(before, after map[string]treeEntry) (string, string) {
	var changed string
	count := 0
	for name, entry := range before {
		if after[name] != entry {
			changed = name
			count++
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			changed = name
			count++
		}
	}
	if count != 1 {
		return "", "commit must modify exactly one file"
	}
	if !regularGoChange(changed, before, after) {
		return "", "change must modify one regular Go file without changing its mode"
	}
	if hasGoSibling(changed, before) || hasGoSibling(changed, after) {
		return "", "Go file has a sibling Go file"
	}
	return changed, ""
}

func regularGoChange(name string, before, after map[string]treeEntry) bool {
	old, oldOK := before[name]
	current, newOK := after[name]
	if !oldOK {
		return false
	}
	if !newOK {
		return false
	}
	if old.mode != current.mode {
		return false
	}
	if old.mode != "100644" {
		if old.mode != "100755" {
			return false
		}
	}
	return strings.HasSuffix(name, ".go")
}

func hasGoSibling(changed string, tree map[string]treeEntry) bool {
	for name := range tree {
		if name == changed {
			continue
		}
		if path.Dir(name) != path.Dir(changed) {
			continue
		}
		if strings.HasSuffix(name, ".go") {
			return true
		}
	}
	return false
}

func unsupportedCommit(reason string) Result { return Result{Status: Unsupported, Reason: reason} }

func readTree(ctx context.Context, repo, revision string) (map[string]treeEntry, error) {
	out, err := gitOutput(ctx, repo, "ls-tree", "-r", "-z", "--full-tree", revision)
	if err != nil {
		return nil, err
	}
	entries := make(map[string]treeEntry)
	for record := range strings.SplitSeq(string(out), "\x00") {
		if record == "" {
			continue
		}
		meta, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok {
			return nil, fmt.Errorf("invalid git tree entry")
		}
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid git tree entry")
		}
		entries[name] = treeEntry{mode: fields[0], object: fields[2]}
	}
	return entries, nil
}

func gitOutput(ctx context.Context, repo string, args ...string) ([]byte, error) {
	args = append([]string{"--no-replace-objects", "-C", repo}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_PREFIX", "GIT_EXEC_PATH", "GIT_SEQUENCE_EDITOR":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git command failed: %w", err)
	}
	return out, nil
}
