package auditor

import (
	"context"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/object"
)

type identity struct {
	hash     string
	patchID  string
	changeID string
}

func identify(ctx context.Context, gitDir string, commit *object.Commit) (identity, error) {
	hash := commit.Hash.String()
	patch, err := patchID(ctx, gitDir, hash)
	if err != nil {
		return identity{}, err
	}
	return identity{hash: hash, patchID: patch, changeID: changeID(commit.Message)}, nil
}

func changeID(message string) string {
	for line := range strings.SplitSeq(message, "\n") {
		value, ok := strings.CutPrefix(line, "Change-Id: ")
		if ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func patchID(ctx context.Context, gitDir, hash string) (string, error) {
	out, err := gitOutput(ctx, gitDir, "--git-dir", gitDir, "diff-tree", "-p", hash)
	if err != nil {
		return "", err
	}
	id, err := gitInput(ctx, gitDir, out, "patch-id", "--stable")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(id)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}
