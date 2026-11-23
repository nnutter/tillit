package status

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/autosubmit"
)

// New builds the status command.
func New() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show watcher and latest snapshot status", Args: cobra.NoArgs, RunE: run}
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	status, err := autosubmit.Report(cmd.Context(), repo)
	if err != nil {
		return err
	}
	watcher := "stopped"
	if status.Active {
		watcher = "running"
	}
	if _, err = fmt.Fprintf(cmd.OutOrStdout(), "watcher: %s\n", watcher); err != nil {
		return err
	}
	if status.SnapshotID == "" {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "snapshot: none")
		return err
	}
	if _, err = fmt.Fprintf(cmd.OutOrStdout(), "snapshot: %s %s", status.SnapshotID, status.State); err != nil {
		return err
	}
	if status.Result != "" {
		if _, err = fmt.Fprintf(cmd.OutOrStdout(), ": %s", status.Result); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout())
	return err
}
