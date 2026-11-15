package cli

import (
	"os"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/cli/add"
	"github.com/nnutter/spor/internal/cli/config"
	logcmd "github.com/nnutter/spor/internal/cli/log"
	"github.com/nnutter/spor/internal/cli/reset"
	"github.com/nnutter/spor/internal/cli/status"
	"github.com/nnutter/spor/internal/cli/verify"
)

// New builds the spor command tree.
func New(version string) *cobra.Command {
	log.SetDefault(log.NewWithOptions(os.Stderr, log.Options{ReportTimestamp: false}))
	root := &cobra.Command{
		Use:     "spor",
		Short:   "Extract and verify refactor commits",
		Version: version,
		Args:    cobra.NoArgs,
	}
	root.PersistentFlags().String("repo", ".", "path to the git worktree")
	root.AddCommand(add.New(), status.New(), verify.New(), config.New(), reset.New(), logcmd.New())
	return root
}
