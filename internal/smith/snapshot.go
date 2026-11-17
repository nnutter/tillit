package smith

import (
	"context"
	"time"

	"github.com/go-git/go-git/v6/plumbing"

	"github.com/nnutter/spor/internal/repository"
)

// ExtractSnapshot writes only refactor commits from an immutable working tree.
// Unlike Add, it neither commits the remainder nor updates manual submission state.
// The caller must retain the resulting objects with a ref before releasing its lock.
func ExtractSnapshot(ctx context.Context, start, base, tree string) ([]Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo, _, err := repository.Open(start)
	if err != nil {
		return nil, err
	}
	defer func() { _ = repo.Close() }()
	parent, err := repo.CommitObject(plumbing.NewHash(base))
	if err != nil {
		return nil, err
	}
	files, err := treeFiles(repo.Storer, parent.TreeHash)
	if err != nil {
		return nil, err
	}
	working, err := treeFiles(repo.Storer, plumbing.NewHash(tree))
	if err != nil {
		return nil, err
	}
	plans, err := planChanges(repo.Storer, files, working)
	if err != nil {
		return nil, err
	}
	who, err := loadIdentity(repo)
	if err != nil {
		return nil, err
	}
	hash := parent.Hash
	var made []madeCommit
	id := newSporID()
	for _, plan := range plans {
		next, commits, err := applyFile(repo.Storer, who, hash, files, plan, id, time.Now())
		if err != nil {
			return nil, err
		}
		files = next
		hash = tipHash(commits, hash)
		made = append(made, commits...)
	}
	return toCommits(made), nil
}
