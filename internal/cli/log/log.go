package log

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nnutter/spor/internal/auditor"
	"github.com/nnutter/spor/internal/smith"
)

// New builds the log command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "log",
		Short: "Show added commits with validation status",
		Args:  cobra.NoArgs,
		RunE:  run,
	}
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	commits, err := smith.Log(cmd.Context(), repo)
	if err != nil {
		return err
	}
	if len(commits) == 0 {
		_, err = fmt.Fprint(cmd.OutOrStdout(), "No commits to show.\n")
		return err
	}
	hashes := make([]string, len(commits))
	for i, commit := range commits {
		hashes[i] = commit.Hash
	}
	statuses, err := auditor.Statuses(cmd.Context(), repo, hashes)
	if err != nil {
		return err
	}
	var b strings.Builder
	for i, commit := range commits {
		fmt.Fprintf(&b, "%s %s (%s)\n", shortHash(commit.Hash), commit.Subject, statuses[i])
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), b.String())
	return err
}

func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}
