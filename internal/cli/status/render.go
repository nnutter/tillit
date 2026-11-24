package status

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"

	"github.com/nnutter/tillit/internal/auditor"
	"github.com/nnutter/tillit/internal/ui"
	"github.com/nnutter/tillit/internal/verifier"
)

// renderStatic writes the non-interactive status view.
//
// When theme.ColorEnabled is true the view is drawn with lipgloss
// styling, tables and semantic colors. When false (piped or tests)
// it falls back to a plain, greppable format that still contains
// the historical substrings (hash + subject + state and
// "verification: ..." ).
func renderStatic(w io.Writer, branch auditor.BranchResult, verification verifier.BranchResult, daemonRunning bool, theme ui.Theme) error {
	if theme.ColorEnabled {
		return renderColor(w, branch, verification, daemonRunning, theme)
	}
	return renderPlain(w, branch, verification, daemonRunning, theme)
}

func renderPlain(w io.Writer, branch auditor.BranchResult, verification verifier.BranchResult, daemonRunning bool, theme ui.Theme) error {
	// Header - plain but improved over the historic single line.
	// Keep historics: "daemon: running" substring is produced.
	// Use theme (which is plain) for consistent rendering.
	var b strings.Builder

	// Title
	b.WriteString("Tillit Status\n")
	b.WriteString(strings.Repeat("=", 30) + "\n")

	// Daemon line - keep legacy "daemon:" prefix for compatibility.
	state := "stopped"
	if daemonRunning {
		state = "running"
	}
	fmt.Fprintf(&b, "daemon: %s\n", state)

	// Branch info
	if len(branch.Snapshot.Branch) > 0 {
		fmt.Fprintf(&b, "branch: %s\n", branch.Snapshot.Branch)
		// Tip and boundary are helpful for debugging.
		if branch.Snapshot.Head != "" {
			fmt.Fprintf(&b, "head: %s\n", branch.Snapshot.Head)
		}
		if branch.Snapshot.Base != "" {
			fmt.Fprintf(&b, "base: %s\n", branch.Snapshot.Base)
		}
	}
	b.WriteString("\n")

	// Summary - plain counts, helpful scanning.
	// This replaces invisible counts previously. Always printed.
	summary := plainSummary(branch, verification)
	b.WriteString(summary)
	b.WriteString("\n\n")

	// Commits - one per line, plain, preserving historic triple.
	// Format: "<hash> <subject> <state> | verification: <verif>"
	// Plus optional error suffix after state.
	// This is intentionally not a table so that the triple
	// "hash subject state" remains greppable as before.
	byHash := make(map[string]verifier.BranchOutcome, len(verification.Outcomes))
	for _, out := range verification.Outcomes {
		byHash[out.Hash] = out
	}
	for _, item := range branch.Outcomes {
		verif := plainVerification(byHash[item.Hash])
		line := fmt.Sprintf("%s %s %s", item.Hash, item.Subject, item.State)
		if item.Error != "" {
			line = fmt.Sprintf("%s: %s", line, item.Error)
		}
		line = fmt.Sprintf("%s | verification: %s", line, verif)
		b.WriteString(line + "\n")
	}
	if len(branch.Outcomes) == 0 {
		b.WriteString("No commits selected for this branch.\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func plainSummary(branch auditor.BranchResult, verification verifier.BranchResult) string {
	passed, failed, pending, running, errCount := countValidation(branch.Outcomes)
	verified, rejected, unsupported, vpending, verr := countVerification(verification.Outcomes)
	return fmt.Sprintf("summary: %d commits — validation: %d passed, %d failed, %d pending, %d running, %d error  •  verification: %d verified, %d rejected, %d unsupported, %d pending, %d error",
		len(branch.Outcomes), passed, failed, pending, running, errCount, verified, rejected, unsupported, vpending, verr)
}

func plainVerification(out verifier.BranchOutcome) string {
	if out.Error != "" {
		return "error: " + out.Error
	}
	if out.Result == nil {
		return "pending"
	}
	state := string(out.Result.Status)
	if out.Result.Reason != "" {
		state += ": " + out.Result.Reason
	}
	return state
}

// renderColor draws the full styled view (TTY or forced color).
// It uses bubble's table component for commits, lipgloss for header
// and daemon badges, and the semantic theme for state colors.
func renderColor(w io.Writer, branch auditor.BranchResult, verification verifier.BranchResult, daemonRunning bool, theme ui.Theme) error {
	var b strings.Builder

	// Title block - bordered, primary color.
	title := theme.Title.Render(" Tillit Status ")
	b.WriteString(title)
	b.WriteString("\n")

	// Branch line.
	if len(branch.Snapshot.Branch) > 0 {
		branchLine := fmt.Sprintf("Branch: %s   Head: %s   Base: %s",
			branch.Snapshot.Branch,
			ui.ShortHash(branch.Snapshot.Head),
			ui.ShortHash(branch.Snapshot.Base),
		)
		b.WriteString(theme.BranchInfo.Render(branchLine))
		b.WriteString("\n")
	}

	// Daemon + summary.
	daemonStr := theme.DaemonBadge(daemonRunning)
	// Ensure legacy substring "daemon:" is still present via prefix.
	daemonLine := fmt.Sprintf("daemon: %s   %s", map[bool]string{true: "running", false: "stopped"}[daemonRunning], daemonStr)
	// If theme already renders badge, daemonLine will have both plain and styled. Keep both for greppability.
	// For colored, we prefer badge display but keep plain prefix for greppability.
	if theme.ColorEnabled {
		// The badge already contains running/stopped, but we add plain prefix for search.
		// Render daemonLine with muted prefix.
		prefix := theme.SummaryLabel.Render("daemon:")
		daemonLine = lipgloss.JoinHorizontal(lipgloss.Left,
			prefix, " ", daemonStr,
		)
	}
	summary := buildColorSummary(branch, verification, theme)
	// Join daemon and summary on one line when space permits, else two lines.
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Left,
		daemonLine,
		"   ",
		summary,
	))
	b.WriteString("\n\n")

	// Table for commits - bubbles/table.
	cols, rows := buildTableData(branch, verification, theme)
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithWidth(80),
		table.WithHeight(min(20, len(rows)+6)),
		table.WithFocused(false),
		table.WithStyles(tableStyles(theme)),
	)
	// Table header is already styled via tableStyles.
	b.WriteString(t.View())
	b.WriteString("\n\n")

	// Footer hint - help style.
	helpLine := theme.Help.Render("Tip: navigate with ↑/↓ when interactive  •  q to quit (TUI)  •  tillit feedback --json for details")
	b.WriteString(helpLine)
	b.WriteString("\n")

	// Document padding.
	content := theme.Document.Render(b.String())
	_, err := io.WriteString(w, content)
	return err
}

func buildColorSummary(branch auditor.BranchResult, verification verifier.BranchResult, theme ui.Theme) string {
	passed, failed, pending, running, errCount := countValidation(branch.Outcomes)
	verified, rejected, unsupported, vpending, verr := countVerification(verification.Outcomes)

	if isColorDisabled(theme) {
		return fmt.Sprintf("Validation: %d passed, %d failed, %d pending, %d running, %d error  •  Verification: %d verified, %d rejected, %d unsupported, %d pending, %d error",
			passed, failed, pending, running, errCount, verified, rejected, unsupported, vpending, verr)
	}
	label := theme.SummaryLabel

	validParts := lipgloss.JoinHorizontal(lipgloss.Left,
		label.Render("Validation: "),
		theme.ValidationPassed.Render(fmt.Sprintf("%d passed", passed)), ", ",
		theme.ValidationFailed.Render(fmt.Sprintf("%d failed", failed)), ", ",
		label.Render(fmt.Sprintf("%d pending", pending)),
		", ", theme.ValidationRunning.Render(fmt.Sprintf("%d running", running)),
	)
	if isPositive(errCount) {
		validParts = lipgloss.JoinHorizontal(lipgloss.Left,
			validParts, ", ", theme.ValidationError.Render(fmt.Sprintf("%d error", errCount)),
		)
	}
	verifParts := lipgloss.JoinHorizontal(lipgloss.Left,
		label.Render("Verification: "),
		theme.VerificationVerified.Render(fmt.Sprintf("%d verified", verified)), ", ",
		theme.VerificationRejected.Render(fmt.Sprintf("%d rejected", rejected)), ", ",
		theme.VerificationUnsupported.Render(fmt.Sprintf("%d unsupported", unsupported)),
	)
	verifParts = appendPendingVerif(verifParts, vpending, verr, theme, label)
	return lipgloss.JoinHorizontal(lipgloss.Left,
		validParts, "  │  ", verifParts,
	)
}
