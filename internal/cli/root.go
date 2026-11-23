package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/cli/config"
	clicmd "github.com/nnutter/spor/internal/cli/daemon"
	"github.com/nnutter/spor/internal/cli/status"
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
	root.AddCommand(status.New(), config.New(), clicmd.Start(), clicmd.Stop())
	return root
}
