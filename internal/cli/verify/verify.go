package verify

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/config"
)

// New builds the verify command.
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "verify",
		Short: "Verify spor commits",
		Args:  cobra.NoArgs,
		RunE:  run,
	}
	command.Flags().String("mode", "", "verification mode for this run: commit, patch, or change")
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
	result, err := auditor.Verify(cmd.Context(), repo, mode)
	if err != nil {
		return err
	}
	text := "No commits to verify.\n"
	if len(result.Outcomes) > 0 {
		text = auditor.Format(result.Mode, result.Outcomes)
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), text); err != nil {
		return err
	}
	if result.Failed {
		return errors.New("verification failed")
	}
	return nil
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
