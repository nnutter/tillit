package status

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/daemon"
	"github.com/nnutter/spor/internal/smith"
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
	if outcome != "" {
		outcome = "rebase: " + outcome + "\n"
	}
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
	return printBranch(cmd, branch)
}

func printBranch(cmd *cobra.Command, branch auditor.BranchResult) error {
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
		if _, err = fmt.Fprintln(cmd.OutOrStdout()); err != nil {
			return err
		}
	}
	return err
}
