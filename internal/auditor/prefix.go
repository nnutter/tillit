package auditor

import (
	"context"
	"strings"

	"github.com/nnutter/spor/internal/repository"
)

// VerifiedPrefix returns the longest prefix of complete, verified change stacks.
// A passing fixup verifies the preceding failed commit in its stack.
func VerifiedPrefix(ctx context.Context, repoPath string) ([]string, error) {
	prefix, _, err := Pending(ctx, repoPath)
	return prefix, err
}

// Pending returns the verified prefix and all pending Spor commits in order.
func Pending(ctx context.Context, repoPath string) ([]string, []string, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = repo.Close() }()
	commits, err := sporCommits(repo, gitDir)
	if err != nil {
		return nil, nil, err
	}
	hashes := make([]string, len(commits))
	for i, commit := range commits {
		hashes[i] = commit.Hash.String()
	}
	statuses, err := Statuses(ctx, repoPath, hashes)
	if err != nil {
		return nil, nil, err
	}
	end := 0
	for start := 0; start < len(commits); {
		next := start + 1
		change := changeID(commits[start].Message)
		for next < len(commits) && isNextFixup(commits[next].Message, change) {
			change = changeID(commits[next].Message)
			next++
		}
		if statuses[next-1] != "passed" {
			break
		}
		end = next
		start = next
	}
	return hashes[:end], hashes, nil
}

func isNextFixup(message, change string) bool {
	if change == "" {
		return false
	}
	return strings.HasPrefix(trailerOf(message, "Fixup-Id"), change+"-")
}
