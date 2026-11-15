package add

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/smith"
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
	result, err := smith.Add(cmd.Context(), repo)
	if err != nil {
		return err
	}
	text := smith.Format(result)
	verified, err := auditor.Verify(cmd.Context(), repo, "")
	if errors.Is(err, auditor.ErrCommandNotConfigured) {
		_, err = fmt.Fprint(cmd.OutOrStdout(), text)
		return err
	}
	if err != nil {
		_, printErr := fmt.Fprint(cmd.OutOrStdout(), text)
		return errors.Join(printErr, err)
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), text+auditor.Format(verified.Mode, verified.Outcomes)); err != nil {
		return err
	}
	if verified.Failed {
		return errors.New("verification failed")
	}
	return nil
}
