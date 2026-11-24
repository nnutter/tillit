package status

import (
	"fmt"
	"io"
	"os"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/nnutter/commitstack"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/nnutter/tillit/internal/auditor"
	"github.com/nnutter/tillit/internal/daemon"
	"github.com/nnutter/tillit/internal/ui"
	"github.com/nnutter/tillit/internal/verifier"
)

// New builds the status command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show validation and verification of branch commits",
		Long: `Show validation and verification of branch commits.

By default, status renders an interactive TUI (Bubble Tea + Bubbles + Lip Gloss)
when attached to a terminal, and a plain, scriptable summary otherwise.
Use --simple for plain output and --interactive to force the TUI.
The design language is centralized in internal/ui/theme.go and uses
semantic colors for every state (passed/failed/pending/error, verified/rejected/unsupported).
`,
		Args: cobra.NoArgs,
		RunE: run,
	}
	cmd.Flags().Bool("interactive", false, "force interactive TUI")
	cmd.Flags().Bool("simple", false, "render plain text without TUI or colors")
	return cmd
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	interactiveFlag, _ := cmd.Flags().GetBool("interactive")
	simpleFlag, _ := cmd.Flags().GetBool("simple")

	running, err := daemon.Running(repo)
	if err != nil {
		return fmt.Errorf("report daemon: %w", err)
	}

	branch, err := auditor.ReportBranch(cmd.Context(), repo)
	if err != nil {
		return err
	}
	verification, err := branchVerification(cmd, repo, branch.Snapshot)
	if err != nil {
		return err
	}

	// Determine output mode.
	// - --simple forces plain (tests, piping, scripting).
	// - --interactive forces TUI even if piped (best-effort).
	// - Otherwise, auto-detect TTY: interactive when terminal, plain otherwise.
	isTTY := isTerminal(cmd.OutOrStdout())
	// Also consider stdin TTY for interactive detection.
	if !isTTY {
		// If output is piped to a file or buffer, not a TTY.
		// Fall back to checking os.Stdout directly for --interactive explicit.
		isTTY = interactiveFlag && isTerminal(os.Stdin)
	}

	if simpleFlag {
		theme := ui.New(false, false)
		return renderStatic(cmd.OutOrStdout(), branch, verification, running, theme)
	}

	if shouldUseInteractive(interactiveFlag, isTTY) {
		// Interactive path - requires Bubble Tea.
		theme := ui.Default()
		// Allow bubbletea to update theme on BackgroundColorMsg.
		return runInteractive(cmd, branch, verification, running, theme)
	}

	// Non-TTY auto plain - but still with improved header/summary (plain theme).
	// This keeps historic substrings for scripts/tests while adding the
	// semantic summary and branch info.
	theme := ui.New(false, false)
	return renderStatic(cmd.OutOrStdout(), branch, verification, running, theme)
}

func shouldUseInteractive(a, b bool) bool { return isTruthy(a) || isTruthy(b) }
func isTruthy(b bool) bool                { return b }

func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

func runInteractive(cmd *cobra.Command, branch auditor.BranchResult, verification verifier.BranchResult, running bool, theme ui.Theme) error {
	m := NewModel(branch, verification, running, theme)

	// Bubble Tea v2: View sets AltScreen declaratively.
	// We supply output/input from cobra so tests can capture.
	out := cmd.OutOrStdout()
	in := cmd.InOrStdin()

	p := tea.NewProgram(m,
		tea.WithOutput(out),
		tea.WithInput(in),
		// Ensure we handle resize automatically.
	)
	_, err := p.Run()
	if err != nil {
		// If Bubble Tea cannot start (e.g., non-TTY piped), fall back to
		// the lipgloss table rendering so the user still gets a
		// well-designed output.
		// Best-effort fallback: render color version.
		// Use string writer fallback.
		// Return error only if fallback also fails.
		fallbackTheme := theme
		if err != nil {
			// Try static color fallback.
			if fbErr := renderColor(out, branch, verification, running, fallbackTheme); fbErr != nil {
				return fmt.Errorf("run TUI and fallback: %w; %v", err, fbErr)
			}
			return nil
		}
	}
	return nil
}

func branchVerification(cmd *cobra.Command, repo string, snapshot commitstack.Snapshot) (verifier.BranchResult, error) {
	result, err := verifier.ReportBranch(cmd.Context(), repo)
	if err != nil {
		return result, fmt.Errorf("report verification: %w", err)
	}
	if !sameSnapshot(snapshot, result.Snapshot) {
		return result, fmt.Errorf("branch changed while collecting status; retry")
	}
	return result, nil
}

func sameSnapshot(a, b commitstack.Snapshot) bool {
	if a.Branch != b.Branch {
		return false
	}
	if a.Head != b.Head {
		return false
	}
	if a.Base != b.Base {
		return false
	}
	return slices.EqualFunc(a.Stacks, b.Stacks, commitstack.SameStack)
}
