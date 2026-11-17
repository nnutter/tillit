package daemon

import (
	"github.com/spf13/cobra"

	daemoncore "github.com/nnutter/spor/internal/daemon"
)

// New builds the daemon command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "daemon",
		Short:   "Run the spor daemon",
		Aliases: []string{"start"},
		Args:    cobra.NoArgs,
		RunE:    run,
	}
	cmd.Flags().Bool("auto", false, "Watch working changes and automatically publish validated refactors")
	return cmd
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	auto, err := cmd.Flags().GetBool("auto")
	if err != nil {
		return err
	}
	if auto {
		return daemoncore.RunAuto(cmd.Context(), repo)
	}
	return daemoncore.Run(cmd.Context(), repo)
}
