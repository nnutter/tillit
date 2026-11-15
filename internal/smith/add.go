package smith

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/nnutter/spor/internal/repository"
)

const (
	noStagedChanges = "No staged changes."
	noRefactors     = "No basic refactors in the staged changes."
	noSubmissions   = "No submissions."

	outcomeCommits = "commits"
	outcomeNone    = "no-refactors"
)

// Commit is one commit written to the spor ref.
type Commit struct {
	Hash    string
	Subject string
}

// Result is the latest submission, or the reason there is nothing to show.
type Result struct {
	SporID  string
	Ref     string
	Commits []Commit
	Message string
}

// Add reads the index against HEAD and commits each basic refactor
// onto the spor ref. The user's branch, index, and worktree stay in place.
func Add(ctx context.Context, repoPath string) (Result, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = repo.Close() }()
	return addRepo(ctx, repo, gitDir)
}

func addRepo(ctx context.Context, repo *git.Repository, gitDir string) (Result, error) {
	head, err := repo.Head()
	if err != nil {
		return Result{}, missingHead(err)
	}
	headFiles, indexFiles, err := loadFiles(repo, head.Hash())
	if err != nil {
		return Result{}, err
	}
	if len(changedPaths(headFiles, indexFiles)) == 0 {
		return Result{Message: noStagedChanges}, nil
	}
	plans, err := planChanges(repo.Storer, headFiles, indexFiles)
	if err != nil {
		return Result{}, err
	}
	ref := repository.HeadRef(gitDir)
	if len(plans) == 0 {
		return recordNone(ctx, gitDir, ref)
	}
	return commitAdd(ctx, repo, gitDir, head.Hash(), headFiles, indexFiles, plans, ref)
}

func commitAdd(ctx context.Context, repo *git.Repository, gitDir string, head plumbing.Hash, headFiles, indexFiles []fileBlob, plans []plannedFile, ref string) (Result, error) {
	who, err := loadIdentity(repo)
	if err != nil {
		return Result{}, err
	}
	sporID := newSporID()
	made, tip, err := applyPlans(repo.Storer, who, head, headFiles, indexFiles, plans, sporID, time.Now())
	if err != nil {
		return Result{}, err
	}
	if err := publish(repo, ref, repository.IDRef(gitDir, sporID), tip); err != nil {
		return Result{}, err
	}
	commits := toCommits(made)
	item := submission{id: sporID, ref: ref, outcome: outcomeCommits, commits: toStored(commits)}
	if err := recordSubmission(ctx, gitDir, item); err != nil {
		return Result{}, err
	}
	return Result{SporID: sporID, Ref: ref, Commits: commits}, nil
}

func recordNone(ctx context.Context, gitDir, ref string) (Result, error) {
	sporID := newSporID()
	item := submission{id: sporID, ref: ref, outcome: outcomeNone, commits: []storedCommit{}}
	if err := recordSubmission(ctx, gitDir, item); err != nil {
		return Result{}, err
	}
	return Result{SporID: sporID, Message: noRefactors}, nil
}

func publish(repo *git.Repository, headRef, idRef string, tip plumbing.Hash) error {
	if err := setRef(repo.Storer, headRef, tip); err != nil {
		return err
	}
	return setRef(repo.Storer, idRef, tip)
}

// Status reports the latest submission for the worktree.
func Status(ctx context.Context, repoPath string) (Result, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return Result{}, err
	}
	_ = repo.Close()
	item, ok, err := latestSubmission(ctx, gitDir)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{Message: noSubmissions}, nil
	}
	return resultFrom(item), nil
}

// Reset undoes the latest add and reports the latest submission that remains.
// The user's branch, index, and worktree stay in place.
func Reset(ctx context.Context, repoPath string) (Result, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = repo.Close() }()
	latest, ok, err := latestSubmission(ctx, gitDir)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{Message: noSubmissions}, nil
	}
	if err := deleteSubmission(ctx, gitDir, latest.id); err != nil {
		return Result{}, err
	}
	if err := removeIDRef(repo, gitDir, latest.id); err != nil {
		return Result{}, err
	}
	if err := restorePreviousTip(ctx, repo, gitDir); err != nil {
		return Result{}, err
	}
	item, ok, err := latestSubmission(ctx, gitDir)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{Message: noSubmissions}, nil
	}
	return resultFrom(item), nil
}

func removeIDRef(repo *git.Repository, gitDir, sporID string) error {
	err := repo.Storer.RemoveReference(plumbing.ReferenceName(repository.IDRef(gitDir, sporID)))
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return nil
	}
	return err
}

func restorePreviousTip(ctx context.Context, repo *git.Repository, gitDir string) error {
	headRef := repository.HeadRef(gitDir)
	tip, ok, err := latestTip(ctx, gitDir)
	if err != nil {
		return err
	}
	if !ok {
		err := repo.Storer.RemoveReference(plumbing.ReferenceName(headRef))
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil
		}
		return err
	}
	return setRef(repo.Storer, headRef, plumbing.NewHash(tip))
}

// Log lists spor commits that are not yet on the user's branch, newest first.
func Log(ctx context.Context, repoPath string) ([]Commit, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = repo.Close() }()
	head, err := repo.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, nil
		}
		return nil, err
	}
	chain, err := sporChain(repo, gitDir)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return nil, nil
	}
	onBranch, err := branchCommits(repo, head.Hash())
	if err != nil {
		return nil, err
	}
	var out []Commit
	for _, commit := range chain {
		if onBranch[commit.Hash] {
			continue
		}
		out = append(out, Commit{Hash: commit.Hash.String(), Subject: subjectOf(commit.Message)})
	}
	slices.Reverse(out)
	return out, nil
}

func sporChain(repo *git.Repository, gitDir string) ([]*object.Commit, error) {
	ref, err := repo.Reference(plumbing.ReferenceName(repository.HeadRef(gitDir)), true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
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

func branchCommits(repo *git.Repository, head plumbing.Hash) (map[plumbing.Hash]bool, error) {
	seen := map[plumbing.Hash]bool{}
	queue := []plumbing.Hash{head}
	for len(queue) > 0 {
		hash := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[hash] {
			continue
		}
		seen[hash] = true
		commit, err := repo.CommitObject(hash)
		if err != nil {
			return nil, err
		}
		queue = append(queue, commit.ParentHashes...)
	}
	return seen, nil
}

func hasSporID(message string) bool {
	for line := range strings.SplitSeq(message, "\n") {
		if strings.HasPrefix(line, "Spor-Id: ") {
			return true
		}
	}
	return false
}

func subjectOf(message string) string {
	for line := range strings.SplitSeq(message, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return strings.TrimSpace(line)
	}
	return ""
}

func resultFrom(item submission) Result {
	if item.outcome == outcomeNone {
		return Result{SporID: item.id, Message: noRefactors}
	}
	return Result{SporID: item.id, Ref: item.ref, Commits: fromStored(item.commits)}
}

// Format renders a result as stable plain text.
func Format(result Result) string {
	var b strings.Builder
	if result.SporID != "" {
		fmt.Fprintf(&b, "spor-id: %s\n", result.SporID)
	}
	if result.Ref != "" {
		if len(result.Commits) > 0 {
			fmt.Fprintf(&b, "ref: %s\n", result.Ref)
		}
	}
	for _, commit := range result.Commits {
		fmt.Fprintf(&b, "%s %s\n", shortHash(commit.Hash), commit.Subject)
	}
	if result.Message != "" {
		b.WriteString(result.Message)
		b.WriteByte('\n')
	}
	return b.String()
}

func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func newSporID() string {
	id := uuid.New()
	return "S" + hex.EncodeToString(id[:])
}

func toCommits(made []madeCommit) []Commit {
	out := make([]Commit, len(made))
	for i, commit := range made {
		out[i] = Commit{Hash: commit.hash.String(), Subject: commit.subject}
	}
	return out
}

func toStored(commits []Commit) []storedCommit {
	out := make([]storedCommit, len(commits))
	for i, commit := range commits {
		out[i] = storedCommit(commit)
	}
	return out
}

func fromStored(commits []storedCommit) []Commit {
	out := make([]Commit, len(commits))
	for i, commit := range commits {
		out[i] = Commit(commit)
	}
	return out
}
