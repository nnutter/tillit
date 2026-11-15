package daemon

import (
	"github.com/spf13/cobra"

	daemoncore "github.com/nnutter/spor/internal/daemon"
)

// New builds the daemon command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:     "daemon",
		Short:   "Run the spor daemon",
		Aliases: []string{"start"},
		Args:    cobra.NoArgs,
		RunE:    run,
	}
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	return daemoncore.Run(cmd.Context(), repo)
}
