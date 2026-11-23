// Package auditor validates commits on the spor ref.
package auditor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/repository"
)

// Result is one validation walk.
type Result struct {
	Mode     config.Mode
	Outcomes []Outcome
	Failed   bool
}

// Validate checks unvalidated spor commits from oldest to newest.
// An empty mode uses the configured mode. The walk stops at the first failure
// that no fixup repairs.
func Validate(ctx context.Context, repoPath string, mode config.Mode) (Result, error) {
	repo, gitDir, cfg, mode, err := openValidate(repoPath, mode)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = repo.Close() }()
	if err := repository.EnsureNoPendingIntegration(gitDir); err != nil {
		return Result{}, err
	}
	if len(cfg.Command) == 0 {
		return Result{}, ErrCommandNotConfigured
	}
	commits, err := sporCommits(repo, gitDir)
	if err != nil {
		return Result{}, err
	}
	return walk(ctx, repo, repoPath, gitDir, cfg, cfg.Command, mode, commits)
}

// Report describes the recorded state of each spor commit.
// Commits that have not been checked are omitted.
func Report(ctx context.Context, repoPath string) (string, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = repo.Close() }()
	cfg, err := config.Load(gitDir)
	if err != nil {
		return "", err
	}
	commits, err := sporCommits(repo, gitDir)
	if err != nil {
		return "", err
	}
	outcomes, err := recorded(ctx, gitDir, cfg.Mode, commits)
	if err != nil {
		return "", err
	}
	return Format(cfg.Mode, outcomes), nil
}

// Statuses reports a validation word for each hash under the configured mode.
// Words are passed, failed, or unvalidated, in the same order as hashes.
func Statuses(ctx context.Context, repoPath string, hashes []string) ([]string, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = repo.Close() }()
	cfg, err := config.Load(gitDir)
	if err != nil {
		return nil, err
	}
	db, err := openState(ctx, gitDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	words := make([]string, len(hashes))
	for i, hash := range hashes {
		commit, err := repo.CommitObject(plumbing.NewHash(hash))
		if err != nil {
			return nil, err
		}
		item, err := identify(ctx, gitDir, commit)
		if err != nil {
			return nil, err
		}
		outcome, ok, err := recordedOutcome(ctx, db, cfg.Mode, item)
		if err != nil {
			return nil, err
		}
		if !ok {
			words[i] = "unvalidated"
			continue
		}
		words[i] = outcomeWord(outcome.Passed)
	}
	return words, nil
}

// Format renders a walk as stable plain text.
func Format(mode config.Mode, outcomes []Outcome) string {
	if len(outcomes) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "mode: %s\n", mode)
	for _, outcome := range outcomes {
		fmt.Fprintf(&b, "%s %s\n", shortHash(outcome.Hash), outcomeWord(outcome.Passed))
	}
	return b.String()
}

func openValidate(repoPath string, mode config.Mode) (*git.Repository, string, config.Config, config.Mode, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return nil, "", config.Config{}, "", err
	}
	cfg, err := config.Load(gitDir)
	if err != nil {
		_ = repo.Close()
		return nil, "", config.Config{}, "", err
	}
	if mode == "" {
		mode = cfg.Mode
	}
	if _, err := config.ParseMode(string(mode)); err != nil {
		_ = repo.Close()
		return nil, "", config.Config{}, "", err
	}
	return repo, gitDir, cfg, mode, nil
}

func walk(ctx context.Context, repo *git.Repository, repoPath, gitDir string, cfg config.Config, command []string, mode config.Mode, commits []*object.Commit) (Result, error) {
	result := Result{Mode: mode}
	if len(commits) == 0 {
		return result, nil
	}
	root, err := worktreeRoot(ctx, repoPath)
	if err != nil {
		return Result{}, err
	}
	db, err := openState(ctx, gitDir)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = db.Close() }()
	queue := append([]*object.Commit(nil), commits...)
	for i := 0; i < len(queue); i++ {
		commit := queue[i]
		outcome, stop, err := validateCommit(ctx, db, root, gitDir, command, mode, commit)
		if err != nil {
			return Result{}, err
		}
		result.Outcomes = append(result.Outcomes, outcome)
		if !stop {
			continue
		}
		following := queue[i+1:]
		fixed, err := fixCommit(ctx, repo, db, root, gitDir, cfg, command, mode, commit, following)
		if err != nil {
			return Result{}, err
		}
		if !fixed.ok {
			result.Failed = true
			result.Outcomes = append(result.Outcomes, fixed.outcomes...)
			return result, nil
		}
		result.Outcomes = append(result.Outcomes, fixed.outcomes...)
		queue = fixed.queue
		i = len(queue) - len(fixed.tail) - 1
	}
	return result, nil
}

type fixResult struct {
	ok       bool
	outcomes []Outcome
	queue    []*object.Commit
	tail     []*object.Commit
}

func fixCommit(ctx context.Context, repo *git.Repository, db statedb, root, gitDir string, cfg config.Config, command []string, mode config.Mode, commit *object.Commit, following []*object.Commit) (fixResult, error) {
	if !configured(cfg.Auditor) {
		return fixResult{ok: false}, nil
	}
	target, err := newTarget(repo, gitDir, commit)
	if err != nil {
		return fixResult{}, err
	}
	if target.sporID == "" {
		return fixResult{ok: false}, nil
	}
	var attempts []Outcome
	for attempt := target.next(); attempt <= target.limit(); attempt++ {
		outcome, nextCommit, err := fixAttempt(ctx, repo, db, root, gitDir, cfg, command, mode, commit, target, attempt)
		if err != nil {
			return fixResult{}, err
		}
		if outcome == nil {
			return fixResult{ok: false, outcomes: attempts}, nil
		}
		attempts = append(attempts, *outcome)
		commit = nextCommit
		if outcome.Passed {
			return replayResult(ctx, repo, gitDir, attempts, following, true)
		}
		if attempt == target.limit() {
			return replayResult(ctx, repo, gitDir, attempts, following, false)
		}
	}
	return fixResult{ok: false, outcomes: attempts}, nil
}

type fixTarget struct {
	sporID   string
	subject  string
	changeID string
	existing int
}

func (t *fixTarget) limit() int {
	return t.existing + 3
}
func (t *fixTarget) next() int { return t.existing + 1 }

func newTarget(repo *git.Repository, gitDir string, commit *object.Commit) (*fixTarget, error) {
	current, err := sporCommits(repo, gitDir)
	if err != nil {
		return nil, err
	}
	changeID := changeID(commit.Message)
	return &fixTarget{
		sporID:   trailerOf(commit.Message, "Spor-Id"),
		subject:  subjectOf(commit.Message),
		changeID: changeID,
		existing: countFixups(current, changeID),
	}, nil
}

func fixAttempt(ctx context.Context, repo *git.Repository, db statedb, root, gitDir string, cfg config.Config, command []string, mode config.Mode, commit *object.Commit, target *fixTarget, attempt int) (*Outcome, *object.Commit, error) {
	slog.InfoContext(ctx, "Attempting fixup", "commit", commit.Hash.String(), "attempt", attempt)
	kind, err := runAgent(ctx, gitDir, cfg.Auditor, commit.Hash.String(), target.subject)
	if err != nil {
		return nil, commit, nil
	}
	if !isFixed(kind) {
		return nil, commit, nil
	}
	hash, err := writeFixup(ctx, repo, gitDir, commit.Hash.String(), target.sporID, target.subject, attempt)
	if err != nil {
		return nil, commit, err
	}
	if hash == "" {
		return nil, commit, nil
	}
	fixupCommit, err := repo.CommitObject(plumbing.NewHash(hash))
	if err != nil {
		return nil, commit, err
	}
	outcome, _, err := validateCommit(ctx, db, root, gitDir, command, mode, fixupCommit)
	if err != nil {
		return nil, commit, err
	}
	return &outcome, fixupCommit, nil
}

func isFixed(kind OutcomeKind) bool {
	switch kind.(type) {
	case outcomeFixed:
		return true
	case outcomeUnchanged:
		return false
	case outcomeFailed:
		return false
	default:
		return false
	}
}

func replayResult(ctx context.Context, repo *git.Repository, gitDir string, attempts []Outcome, following []*object.Commit, ok bool) (fixResult, error) {
	tail, err := replayFollowing(ctx, repo, gitDir, following)
	if err != nil {
		return fixResult{}, err
	}
	head, err := sporCommits(repo, gitDir)
	if err != nil {
		return fixResult{}, err
	}
	return fixResult{ok: ok, outcomes: attempts, queue: head, tail: tail}, nil
}

func countFixups(commits []*object.Commit, targetChange string) int {
	count := 0
	for _, commit := range commits {
		if !countsAsFixup(commit, targetChange) {
			continue
		}
		count++
	}
	return count
}

func countsAsFixup(commit *object.Commit, targetChange string) bool {
	id := trailerOf(commit.Message, "Fixup-Id")
	if id == "" {
		return false
	}
	if targetChange == "" {
		return true
	}
	return hasFixupPrefix(id, targetChange)
}

func hasFixupPrefix(id, target string) bool {
	if len(id) <= len(target) {
		return false
	}
	return id[:len(target)] == target
}

func recorded(ctx context.Context, gitDir string, mode config.Mode, commits []*object.Commit) ([]Outcome, error) {
	if len(commits) == 0 {
		return nil, nil
	}
	db, err := openState(ctx, gitDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	var outcomes []Outcome
	for _, commit := range commits {
		item, err := identify(ctx, gitDir, commit)
		if err != nil {
			return nil, err
		}
		outcome, ok, err := recordedOutcome(ctx, db, mode, item)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		outcomes = append(outcomes, outcome)
		if !outcome.Passed {
			break
		}
	}
	return outcomes, nil
}

func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func outcomeWord(passed bool) string {
	if passed {
		return "passed"
	}
	return "failed"
}
