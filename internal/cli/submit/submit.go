package submit

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/smith"
)

// New builds the submit command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "submit",
		Short: "Commit basic refactors from the index",
		Args:  cobra.NoArgs,
		RunE:  run,
	}
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	result, err := smith.Submit(cmd.Context(), repo)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), smith.Format(result))
	return err
}
