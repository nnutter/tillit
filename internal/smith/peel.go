package smith

import (
	"fmt"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"

	"github.com/nnutter/spor/internal/refactor"
)

type plannedFile struct {
	path  string
	steps []refactor.Step
}

func planChanges(source storer.EncodedObjectStorer, head, index []fileBlob) ([]plannedFile, error) {
	headByPath := byPath(head)
	indexByPath := byPath(index)
	var plans []plannedFile
	for _, path := range changedPaths(head, index) {
		steps, err := planPath(source, headByPath, indexByPath, path)
		if err != nil {
			return nil, err
		}
		if len(steps) == 0 {
			continue
		}
		plans = append(plans, plannedFile{path: path, steps: steps})
	}
	return plans, nil
}

func planPath(source storer.EncodedObjectStorer, head, index map[string]fileBlob, path string) ([]refactor.Step, error) {
	if !goFile(path) {
		return nil, nil
	}
	before, ok := head[path]
	if !ok {
		return nil, nil
	}
	after, ok := index[path]
	if !ok {
		return nil, nil
	}
	beforeBytes, err := readBlob(source, before.hash)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	afterBytes, err := readBlob(source, after.hash)
	if err != nil {
		return nil, fmt.Errorf("read staged %s: %w", path, err)
	}
	steps, err := refactor.Plan(path, beforeBytes, afterBytes)
	if err != nil {
		return nil, fmt.Errorf("plan %s: %w", path, err)
	}
	return steps, nil
}

type madeCommit struct {
	hash    plumbing.Hash
	subject string
}

func applyPlans(source storer.EncodedObjectStorer, who identity, parent plumbing.Hash, files, index []fileBlob, plans []plannedFile, sporID string, when time.Time) ([]madeCommit, plumbing.Hash, error) {
	current := files
	var made []madeCommit
	for _, plan := range plans {
		next, commits, err := applyFile(source, who, parent, current, plan, sporID, when)
		if err != nil {
			return nil, plumbing.ZeroHash, err
		}
		current = next
		parent = tipHash(commits, parent)
		made = append(made, commits...)
	}
	extra, tip, err := maybeRemainder(source, who, parent, current, index, sporID, when)
	if err != nil {
		return nil, plumbing.ZeroHash, err
	}
	if extra.hash.IsZero() {
		return made, tip, nil
	}
	return append(made, extra), tip, nil
}

func tipHash(commits []madeCommit, parent plumbing.Hash) plumbing.Hash {
	if len(commits) == 0 {
		return parent
	}
	return commits[len(commits)-1].hash
}

func applyFile(source storer.EncodedObjectStorer, who identity, parent plumbing.Hash, files []fileBlob, plan plannedFile, sporID string, when time.Time) ([]fileBlob, []madeCommit, error) {
	var made []madeCommit
	current := files
	for _, step := range plan.steps {
		next, commit, err := applyStep(source, who, parent, current, plan.path, step, sporID, when)
		if err != nil {
			return nil, nil, err
		}
		current = next
		parent = commit.hash
		made = append(made, commit)
	}
	return current, made, nil
}

func applyStep(source storer.EncodedObjectStorer, who identity, parent plumbing.Hash, files []fileBlob, path string, step refactor.Step, sporID string, when time.Time) ([]fileBlob, madeCommit, error) {
	blob, err := storeBlob(source, step.Source)
	if err != nil {
		return nil, madeCommit{}, err
	}
	next := replaceHash(files, path, blob)
	tree, err := storeFiles(source, next)
	if err != nil {
		return nil, madeCommit{}, err
	}
	subject := refactorSubject(step.Refactoring)
	hash, err := writeCommit(source, tree, parent, who, commitMessage(subject, sporID, newChangeID(tree, parent, sporID, subject)), when)
	if err != nil {
		return nil, madeCommit{}, err
	}
	return next, madeCommit{hash: hash, subject: subject}, nil
}

func maybeRemainder(source storer.EncodedObjectStorer, who identity, parent plumbing.Hash, peeled, index []fileBlob, sporID string, when time.Time) (madeCommit, plumbing.Hash, error) {
	tipTree, err := storeFiles(source, peeled)
	if err != nil {
		return madeCommit{}, plumbing.ZeroHash, err
	}
	indexTree, err := storeFiles(source, index)
	if err != nil {
		return madeCommit{}, plumbing.ZeroHash, err
	}
	if tipTree.Equal(indexTree) {
		return madeCommit{}, parent, nil
	}
	hash, err := writeCommit(source, indexTree, parent, who, commitMessage(remainderSubject, sporID, newChangeID(indexTree, parent, sporID, remainderSubject)), when)
	if err != nil {
		return madeCommit{}, plumbing.ZeroHash, err
	}
	return madeCommit{hash: hash, subject: remainderSubject}, hash, nil
}
