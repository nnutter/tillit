package status

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/daemon"
	"github.com/nnutter/spor/internal/repository"
	"github.com/nnutter/spor/internal/smith"
	"github.com/nnutter/spor/internal/verifier"
)

// New builds the status command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the latest add",
		Args:  cobra.NoArgs,
		RunE:  run,
	}
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	branch, err := auditor.ReportBranch(cmd.Context(), repo)
	if err != nil {
		return err
	}
	verification, err := branchVerification(cmd, repo, branch.Snapshot)
	if err != nil {
		return err
	}
	result, err := smith.Status(cmd.Context(), repo)
	if err != nil {
		return err
	}
	report, err := auditor.Report(cmd.Context(), repo)
	if err != nil {
		return err
	}
	outcome, err := daemon.RebaseOutcome(cmd.Context(), repo)
	if err != nil {
		return err
	}
	outcome = formatRebase(outcome)
	workError, err := daemon.WorkError(cmd.Context(), repo)
	if err != nil {
		return err
	}
	if workError != "" {
		workError += "\n"
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), smith.Format(result)+report+outcome+workError)
	if err != nil {
		return err
	}
	return printBranch(cmd, branch, verification)
}

func formatRebase(outcome string) string {
	if outcome == "" {
		return ""
	}
	return "rebase: " + outcome + "\n"
}

func branchVerification(cmd *cobra.Command, repo string, snapshot repository.BranchSnapshot) (verifier.BranchResult, error) {
	result, err := verifier.ReportBranch(cmd.Context(), repo)
	if err != nil {
		return result, fmt.Errorf("report verification: %w", err)
	}
	if !sameSnapshot(snapshot, result.Snapshot) {
		return result, fmt.Errorf("branch changed while collecting status; retry")
	}
	return result, nil
}

func sameSnapshot(a, b repository.BranchSnapshot) bool {
	if a.Ref != b.Ref {
		return false
	}
	if a.Tip != b.Tip {
		return false
	}
	if a.Boundary != b.Boundary {
		return false
	}
	return slices.Equal(a.Commits, b.Commits)
}

func printBranch(cmd *cobra.Command, branch auditor.BranchResult, verification verifier.BranchResult) error {
	byHash := make(map[string]verifier.BranchOutcome, len(verification.Outcomes))
	for _, item := range verification.Outcomes {
		byHash[item.Hash] = item
	}
	var err error
	for _, item := range branch.Outcomes {
		if _, err = fmt.Fprintf(cmd.OutOrStdout(), "branch: %s %s %s", item.Hash, item.Subject, item.State); err != nil {
			return err
		}
		if item.Error != "" {
			if _, err = fmt.Fprintf(cmd.OutOrStdout(), ": %s", item.Error); err != nil {
				return err
			}
		}
		if _, err = fmt.Fprintf(cmd.OutOrStdout(), " | verification: %s", formatVerification(byHash[item.Hash])); err != nil {
			return err
		}
		if _, err = fmt.Fprintln(cmd.OutOrStdout()); err != nil {
			return err
		}
	}
	return err
}

func formatVerification(item verifier.BranchOutcome) string {
	if item.Error != "" {
		return "error: " + item.Error
	}
	if item.Result == nil {
		return "pending"
	}
	state := string(item.Result.Status)
	if item.Result.Reason != "" {
		state += ": " + item.Result.Reason
	}
	return state
}
