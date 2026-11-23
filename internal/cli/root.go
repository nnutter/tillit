package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/cli/add"
	"github.com/nnutter/spor/internal/cli/config"
	clicmd "github.com/nnutter/spor/internal/cli/daemon"
	cligh "github.com/nnutter/spor/internal/cli/gh"
	logcmd "github.com/nnutter/spor/internal/cli/log"
	"github.com/nnutter/spor/internal/cli/rebase"
	"github.com/nnutter/spor/internal/cli/reset"
	"github.com/nnutter/spor/internal/cli/status"
	"github.com/nnutter/spor/internal/cli/validate"
)

// New builds the spor command tree.
func New(version string) *cobra.Command {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	root := &cobra.Command{
		Use:     "spor",
		Short:   "Extract and validate refactor commits",
		Version: version,
		Args:    cobra.NoArgs,
	}
	root.PersistentFlags().String("repo", ".", "path to the git worktree")
	root.AddCommand(add.New(), status.New(), validate.New(), config.New(), reset.New(), logcmd.New(), clicmd.Start(), clicmd.Stop(), rebase.New(), cligh.New())
	return root
}
