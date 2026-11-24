package daemon

import (
	"fmt"

	"github.com/spf13/cobra"

	daemoncore "github.com/nnutter/spor/internal/daemon"
)

// New builds the daemon command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the spor daemon",
		Args:  cobra.NoArgs,
		RunE:  run,
	}
}

// Start builds the detached start command.
func Start() *cobra.Command {
	return &cobra.Command{
		Use: "start", Short: "Start the daemon in the background", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := cmd.Flags().GetString("repo")
			if err != nil {
				return err
			}
			path, err := daemoncore.Start(cmd.Context(), repo)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
			return err
		},
	}
}

// Stop builds the supervised stop command.
func Stop() *cobra.Command {
	cmd := &cobra.Command{
		Use: "stop", Short: "Stop the background daemon", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := cmd.Flags().GetString("repo")
			if err != nil {
				return err
			}
			kill, err := cmd.Flags().GetBool("kill")
			if err != nil {
				return err
			}
			return daemoncore.Stop(cmd.Context(), repo, kill)
		},
	}
	cmd.Flags().Bool("kill", false, "Force stop an unresponsive daemon")
	return cmd
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	return daemoncore.Run(cmd.Context(), repo)
}
