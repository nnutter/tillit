package cli

import (
	"os"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/cli/status"
	"github.com/nnutter/spor/internal/cli/submit"
)

// New builds the spor command tree.
func New(version string) *cobra.Command {
	log.SetDefault(log.NewWithOptions(os.Stderr, log.Options{ReportTimestamp: false}))
	root := &cobra.Command{
		Use:     "spor",
		Short:   "Extract refactor commits from staged changes",
		Version: version,
		Args:    cobra.NoArgs,
	}
	root.PersistentFlags().String("repo", ".", "path to the git worktree")
	root.AddCommand(submit.New(), status.New())
	return root
}
