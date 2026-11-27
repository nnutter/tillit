package status

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

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
Use --simple for plain output, --json for machine-readable JSON,
and --interactive to force the TUI.
The design language is centralized in internal/ui/theme.go and uses
semantic colors for every state (passed/failed/pending/running/error, verified/rejected/bypassed).
The TUI auto-refreshes every 250ms (configurable with --interval) so that
validation states transition pending → running → passed/failed without restart.
Status starts the daemon automatically if it is not already running;
use --no-start to disable auto-start (--simple and --json imply --no-start).
In the TUI, select a commit and press <enter> to see its feedback in a pop-over
(scroll with ↑/↓, esc/enter to close).
`,
		Args: cobra.NoArgs,
		RunE: run,
	}
	cmd.Flags().Bool("interactive", false, "force interactive TUI")
	cmd.Flags().Bool("simple", false, "render plain text without TUI or colors (implies --no-start)")
	cmd.Flags().Bool("json", false, "render status as JSON (implies --no-start)")
	cmd.Flags().Duration("interval", 250*time.Millisecond, "refresh interval for interactive TUI")
	cmd.Flags().Bool("no-start", false, "do not start the daemon if it is not already running")
	return cmd
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	interactiveFlag, _ := cmd.Flags().GetBool("interactive")
	simpleFlag, _ := cmd.Flags().GetBool("simple")
	jsonFlag, _ := cmd.Flags().GetBool("json")
	interval, _ := cmd.Flags().GetDuration("interval")
	noStart, _ := cmd.Flags().GetBool("no-start")

	daemonState, branch, verification, err := collectStatus(cmd, repo)
	if err != nil {
		return err
	}

	if shouldAutoStart(daemonState.Running, noStart, simpleFlag, jsonFlag) {
		daemonState = ensureDaemon(cmd, repo)
	}

	if jsonFlag {
		return renderJSON(cmd.OutOrStdout(), branch, verification, daemonState)
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
		return renderStatic(cmd.OutOrStdout(), branch, verification, daemonState, theme)
	}

	if shouldUseInteractive(interactiveFlag, isTTY) {
		// Interactive path - requires Bubble Tea.
		theme := ui.Default()
		// Allow bubbletea to update theme on BackgroundColorMsg.
		return runInteractive(cmd, repo, branch, verification, daemonState, theme, interval)
	}

	// Non-TTY auto plain - but still with improved header/summary (plain theme).
	// This keeps historic substrings for scripts/tests while adding the
	// semantic summary and branch info.
	theme := ui.New(false, false)
	return renderStatic(cmd.OutOrStdout(), branch, verification, daemonState, theme)
}

func shouldUseInteractive(a, b bool) bool { return isTruthy(a) || isTruthy(b) }
func isTruthy(b bool) bool                { return b }
func shouldAutoStart(running, noStart, simple, asJSON bool) bool {
	if running {
		return false
	}
	if noStart {
		return false
	}
	if simple {
		return false
	}
	return !asJSON
}

func collectStatus(cmd *cobra.Command, repo string) (daemon.State, auditor.BranchResult, verifier.BranchResult, error) {
	daemonState, err := daemon.Status(repo)
	if err != nil {
		return daemon.State{}, auditor.BranchResult{}, verifier.BranchResult{}, fmt.Errorf("report daemon: %w", err)
	}
	branch, err := auditor.ReportBranch(cmd.Context(), repo)
	if err != nil {
		return daemonState, auditor.BranchResult{}, verifier.BranchResult{}, err
	}
	normalizeValidationRunning(&branch, daemonState.Running)
	verification, err := branchVerification(cmd, repo, branch.Snapshot)
	if err != nil {
		return daemonState, branch, verifier.BranchResult{}, err
	}
	return daemonState, branch, verification, nil
}

// ensureDaemon starts the supervised daemon if it is not already running.
// It returns the new daemon state for display. "already running" races are
// treated as success; other failures are logged to stderr and the caller
// continues to render a status snapshot.
func ensureDaemon(cmd *cobra.Command, repo string) daemon.State {
	if _, err := daemon.Start(cmd.Context(), repo); err != nil {
		if errors.Is(err, daemon.ErrAlreadyRunning) || strings.Contains(err.Error(), "already running") {
			if updated, checkErr := daemon.Status(repo); checkErr == nil {
				return updated
			}
			return daemon.State{Running: true}
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "tillit status: start daemon: %v\n", err)
		// Re-check: start may have partially succeeded.
		if updated, checkErr := daemon.Status(repo); checkErr == nil {
			return updated
		}
		return daemon.State{}
	}
	return daemon.State{Running: true}
}

func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

func runInteractive(cmd *cobra.Command, repo string, branch auditor.BranchResult, verification verifier.BranchResult, daemonState daemon.State, theme ui.Theme, interval time.Duration) error {
	m := NewModelWithRepo(branch, verification, daemonState, theme, repo, interval)

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
			if fbErr := renderColor(out, branch, verification, daemonState, fallbackTheme); fbErr != nil {
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

// normalizeValidationRunning maps a stale "running" row to "pending" when
// the daemon is not active. The daemon persists "running" while a validation
// command executes; if it later stops before finishing (crash, SIGTERM), the
// row would otherwise stay "running" forever.
func normalizeValidationRunning(branch *auditor.BranchResult, daemonRunning bool) {
	if daemonRunning {
		return
	}
	for i := range branch.Outcomes {
		if branch.Outcomes[i].State == "running" {
			branch.Outcomes[i].State = "pending"
		}
	}
}
