package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/nnutter/tillit/internal/cli/config"
	clicmd "github.com/nnutter/tillit/internal/cli/daemon"
	"github.com/nnutter/tillit/internal/cli/status"
)

// New builds the tillit command tree.
func New(version string) *cobra.Command {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	root := &cobra.Command{
		Use:     "tillit",
		Short:   "Validate and verify feature-branch commits",
		Version: version,
		Args:    cobra.NoArgs,
	}
	root.PersistentFlags().String("repo", ".", "path to the git worktree")
	root.AddCommand(status.New(), config.New(), clicmd.Start(), clicmd.Stop())
	return root
}
