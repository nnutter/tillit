package gh

import (
	"github.com/spf13/cobra"
)

// New builds the gh command.
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "gh",
		Short: "Monitor GitHub pull-request checks",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newAuth(), newWatch())
	return command
}
