package auditor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"

	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/repository"
	"github.com/nnutter/spor/internal/subprocess"
)

// OutcomeKind is the kind of one fixup attempt.
//
// It is sealed: only the kinds declared in this package implement it.
// Switch over concrete types with go-check-sumtype exhaustiveness.
//
//sumtype:decl
type OutcomeKind interface {
	sealedOutcomeKind()
}

type outcomeFixed struct{}

func (outcomeFixed) sealedOutcomeKind() {}

type outcomeUnchanged struct{}

func (outcomeUnchanged) sealedOutcomeKind() {}

type outcomeFailed struct{}

func (outcomeFailed) sealedOutcomeKind() {}

// OutcomeKinds in attempt order.
var (
	OutcomeFixed     OutcomeKind = outcomeFixed{}
	OutcomeUnchanged OutcomeKind = outcomeUnchanged{}
	OutcomeFailed    OutcomeKind = outcomeFailed{}
)

// configured reports whether an agent provider and model are set.
func configured(auditor config.Auditor) bool {
	if auditor.Provider == "" {
		return false
	}
	return auditor.Model != ""
}

// runAgent asks pi to repair the current checkout and reports what changed.
func runAgent(ctx context.Context, gitDir string, auditorCfg config.Auditor, target, subject string) (OutcomeKind, error) {
	if !configured(auditorCfg) {
		return OutcomeFailed, fmt.Errorf("auditor agent is not configured")
	}
	dest, err := checkoutDir(gitDir)
	if err != nil {
		return OutcomeFailed, err
	}
	before, err := treeHash(ctx, dest)
	if err != nil {
		return OutcomeFailed, err
	}
	thinking := thinkingName(auditorCfg)
	prompt := fmt.Sprintf("Fix the failing commit %s (%s) so the validation command passes. Validation failed on this commit. Fix the code in the current checkout and stop; do not commit.", target, subject)
	args := []string{"-p", "--provider", auditorCfg.Provider, "--model", auditorCfg.Model, "--thinking", thinking, "--", prompt}
	if err := runCaptured(ctx, dest, "pi", args...); err != nil {
		return OutcomeFailed, fmt.Errorf("run pi: %w", err)
	}
	after, err := stagedTreeHash(ctx, dest)
	if err != nil {
		return OutcomeFailed, err
	}
	if before == after {
		return OutcomeUnchanged, nil
	}
	return OutcomeFixed, nil
}

func thinkingName(auditorCfg config.Auditor) string {
	if auditorCfg.Thinking == nil {
		return "low"
	}
	return auditorCfg.Thinking.String()
}

func stagedTreeHash(ctx context.Context, dest string) (string, error) {
	if err := runGit(ctx, dest, "add", "-A"); err != nil {
		return "", err
	}
	return treeHash(ctx, dest)
}

func commandCtx(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
	cmd := subprocess.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd
}

func runCaptured(ctx context.Context, dir, name string, args ...string) error {
	return commandCtx(ctx, dir, name, args...).Run()
}

func checkoutDir(gitDir string) (string, error) {
	return repository.CheckoutDir(gitDir)
}

func treeHash(ctx context.Context, dir string) (string, error) {
	out, err := gitOutput(ctx, dir, "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// newFixupChangeID derives a change identity for one fixup attempt.
func newFixupChangeID(parent, sporID, subject string, attempt int) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(parent))
	_, _ = hash.Write([]byte(sporID))
	_, _ = hash.Write([]byte(subject))
	_, _ = hash.Write([]byte(fmt.Sprint(attempt)))
	return "I" + hex.EncodeToString(hash.Sum(nil))[:40]
}

// trailerOf reads one key trailer from a commit message.
func trailerOf(message, key string) string {
	prefix := key + ": "
	for line := range strings.SplitSeq(message, "\n") {
		value, ok := strings.CutPrefix(line, prefix)
		if ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// subjectOf reads the first non-empty line of a commit message.
func subjectOf(message string) string {
	for line := range strings.SplitSeq(message, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return strings.TrimSpace(line)
	}
	return ""
}

// fixupMessage builds the commit message for one fixup attempt.
func fixupMessage(subject, sporID, changeID, fixupID string) string {
	return subject + "\n\nSpor-Id: " + sporID + "\nChange-Id: " + changeID + "\nFixup-Id: " + fixupID + "\n"
}
