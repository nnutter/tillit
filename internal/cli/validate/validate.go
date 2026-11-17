package validate

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/daemon"
)

// New builds the validate command.
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "validate",
		Short: "Validate spor commits",
		Args:  cobra.NoArgs,
		RunE:  run,
	}
	command.Flags().String("mode", "", "validation mode for this run: commit, patch, or change")
	return command
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	mode, err := selectedMode(cmd)
	if err != nil {
		return err
	}
	if err := daemon.EnqueueValidate(cmd.Context(), repo, string(mode)); err != nil {
		return err
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), "Queued.\n")
	return err
}

func selectedMode(cmd *cobra.Command) (config.Mode, error) {
	name, err := cmd.Flags().GetString("mode")
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", nil
	}
	return config.ParseMode(name)
}
