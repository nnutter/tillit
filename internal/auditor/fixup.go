package auditor

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/nnutter/spor/internal/repository"
)

// replayFollowing cherry-picks each following commit onto the current
// spor tip in the daemon-owned checkout and returns the new tail.
func replayFollowing(ctx context.Context, repo *git.Repository, gitDir string, following []*object.Commit) ([]*object.Commit, error) {
	if len(following) == 0 {
		return nil, nil
	}
	dest, err := checkoutDir(gitDir)
	if err != nil {
		return nil, err
	}
	var tail []*object.Commit
	for _, commit := range following {
		if err := runGit(ctx, dest, "cherry-pick", "--allow-empty", commit.Hash.String()); err != nil {
			return nil, err
		}
		tip, err := gitOutput(ctx, dest, "rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
		tip = strings.TrimSpace(tip)
		if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(repository.HeadRef(gitDir)), plumbing.NewHash(tip))); err != nil {
			return nil, err
		}
		replayed, err := repo.CommitObject(plumbing.NewHash(tip))
		if err != nil {
			return nil, err
		}
		tail = append(tail, replayed)
	}
	return tail, nil
}

// writeFixup snapshots the staged checkout as one fixup commit on the
// spor ref and returns the new hash. An empty stage writes nothing.
func writeFixup(ctx context.Context, repo *git.Repository, gitDir, parentHash, sporID, subject string, attempt int) (string, error) {
	dest, err := checkoutDir(gitDir)
	if err != nil {
		return "", err
	}
	if err := runGit(ctx, dest, "add", "-A"); err != nil {
		return "", err
	}
	diff, err := gitOutput(ctx, dest, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(diff) == "" {
		return "", nil
	}
	targetChange := trailerOf(lastMessage(repo, parentHash), "Change-Id")
	changeID := newFixupChangeID(parentHash, sporID, subject, attempt)
	fixupID := targetChange + "-" + fmt.Sprint(attempt)
	if targetChange == "" {
		fixupID = fmt.Sprint(attempt)
	}
	message := fixupMessage("Fixup "+subject, sporID, changeID, fixupID)
	if err := runGit(ctx, dest, "commit", "-m", message); err != nil {
		return "", err
	}
	hash, err := gitOutput(ctx, dest, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	hash = strings.TrimSpace(hash)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(repository.HeadRef(gitDir)), plumbing.NewHash(hash))); err != nil {
		return "", err
	}
	return hash, nil
}

func lastMessage(repo *git.Repository, hash string) string {
	commit, err := repo.CommitObject(plumbing.NewHash(hash))
	if err != nil {
		return ""
	}
	return commit.Message
}
