package reset

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/smith"
)

// New builds the reset command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Undo the latest add",
		Args:  cobra.NoArgs,
		RunE:  run,
	}
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	result, err := smith.Reset(cmd.Context(), repo)
	if err != nil {
		return err
	}
	report, err := auditor.Report(cmd.Context(), repo)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), smith.Format(result)+report)
	return err
}
