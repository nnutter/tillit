// Package rebase queues integration of verified commits.
package rebase

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/daemon"
)

// New builds the rebase command.
func New() *cobra.Command {
	return &cobra.Command{Use: "rebase", Short: "Queue integration of verified commits", Args: cobra.NoArgs, RunE: run}
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	if err := daemon.EnqueueRebase(cmd.Context(), repo); err != nil {
		return err
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), "Queued. Check spor status for the result.\n")
	return err
}
