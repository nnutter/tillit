package auditor

import (
	"errors"
	"slices"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/nnutter/spor/internal/repository"
)

func sporCommits(repo *git.Repository, gitDir string) ([]*object.Commit, error) {
	ref, err := repo.Reference(plumbing.ReferenceName(repository.HeadRef(gitDir)), true)
	if err != nil {
		if isMissingRef(err) {
			return nil, nil
		}
		return nil, err
	}
	var chain []*object.Commit
	hash := ref.Hash()
	for {
		commit, err := repo.CommitObject(hash)
		if err != nil {
			return nil, err
		}
		if !hasSporID(commit.Message) {
			break
		}
		chain = append(chain, commit)
		if len(commit.ParentHashes) == 0 {
			break
		}
		hash = commit.ParentHashes[0]
	}
	slices.Reverse(chain)
	return chain, nil
}

func hasSporID(message string) bool {
	for line := range strings.SplitSeq(message, "\n") {
		if strings.HasPrefix(line, "Spor-Id: ") {
			return true
		}
	}
	return false
}

func isMissingRef(err error) bool {
	return errors.Is(err, plumbing.ErrReferenceNotFound)
}
