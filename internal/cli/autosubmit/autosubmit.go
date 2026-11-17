// Package autosubmit wires the opt-in working-change watcher.
package autosubmit

import (
	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/autosubmit"
)

// New builds the auto-submit command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "auto-submit",
		Short: "Submit stable working refactors for daemon validation and publication",
		Args:  cobra.NoArgs,
		RunE:  run,
	}
}

func run(cmd *cobra.Command, _ []string) error {
	start, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	return autosubmit.Run(cmd.Context(), start)
}
