package daemon

import (
	"fmt"

	"github.com/spf13/cobra"

	daemoncore "github.com/nnutter/spor/internal/daemon"
)

// Start builds the start command.
func Start() *cobra.Command {
	cmd := &cobra.Command{
		Use: "start", Short: "Start the refactor watcher", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := cmd.Flags().GetString("repo")
			if err != nil {
				return err
			}
			foreground, err := cmd.Flags().GetBool("foreground")
			if err != nil {
				return err
			}
			if foreground {
				return daemoncore.RunReady(cmd.Context(), repo, nil)
			}
			path, err := daemoncore.Start(cmd.Context(), repo)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
			return err
		},
	}
	cmd.Flags().Bool("foreground", false, "Run in the foreground until interrupted")
	return cmd
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
