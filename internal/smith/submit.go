package smith

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"

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

// Submit reads the index against HEAD and commits each basic refactor
// onto the spor ref. The user's branch, index, and worktree stay in place.
func Submit(ctx context.Context, repoPath string) (Result, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = repo.Close() }()
	return submitRepo(ctx, repo, gitDir)
}

func submitRepo(ctx context.Context, repo *git.Repository, gitDir string) (Result, error) {
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
	return commitSubmission(ctx, repo, gitDir, head.Hash(), headFiles, indexFiles, plans, ref)
}

func commitSubmission(ctx context.Context, repo *git.Repository, gitDir string, head plumbing.Hash, headFiles, indexFiles []fileBlob, plans []plannedFile, ref string) (Result, error) {
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
