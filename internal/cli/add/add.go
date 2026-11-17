package add

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/daemon"
)

// New builds the add command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:     "add",
		Short:   "Commit basic refactors from the index",
		Aliases: []string{"stage"},
		Args:    cobra.NoArgs,
		RunE:    run,
	}
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	if err := daemon.EnqueueAdd(cmd.Context(), repo); err != nil {
		return err
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), "Queued.\n")
	return err
}
