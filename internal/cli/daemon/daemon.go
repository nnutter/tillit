package daemon

import (
	"fmt"

	"github.com/spf13/cobra"

	daemoncore "github.com/nnutter/tillit/internal/daemon"
)

// Start builds the daemon start command.
func Start() *cobra.Command {
	cmd := &cobra.Command{
		Use: "start", Short: "Start the daemon", Args: cobra.NoArgs,
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
				return daemoncore.Foreground(repo, cmd.ErrOrStderr())
			}
			path, err := daemoncore.Start(cmd.Context(), repo)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
			return err
		},
	}
	cmd.Flags().Bool("foreground", false, "Run in the foreground and stream daemon output to stderr")
	return cmd
}

// Stop builds the supervised stop command.
func Stop() *cobra.Command {
	cmd := &cobra.Command{
		Use: "stop", Short: "Stop the daemon", Args: cobra.NoArgs,
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
