package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	sporconfig "github.com/nnutter/spor/internal/config"
	"github.com/nnutter/spor/internal/repository"
)

// New builds the config command.
func New() *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Read and write spor configuration",
		Args:  cobra.NoArgs,
	}
	command.PersistentFlags().Bool("global", false, "use the user configuration file")
	command.PersistentFlags().Bool("repository", false, "use the repository configuration file")
	command.AddCommand(newShow(), newGet(), newSet(), newAppend(), newRemove())
	return command
}

func newShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the configuration",
		Args:  cobra.NoArgs,
		RunE:  runShow,
	}
}

func newGet() *cobra.Command {
	return &cobra.Command{
		Use:               "get KEY",
		Short:             "Print one configuration value",
		Args:              cobra.ExactArgs(1),
		RunE:              runGet,
		ValidArgsFunction: completeConfigKey,
	}
}

func newSet() *cobra.Command {
	command := &cobra.Command{
		Use:               "set KEY JSON",
		Short:             "Set a configuration value from JSON",
		Args:              cobra.ExactArgs(2),
		RunE:              runSet,
		ValidArgsFunction: completeConfigKey,
	}
	// The JSON text is one argument, so a leading '-' stays in the value.
	command.Flags().SetInterspersed(false)
	return command
}

func newAppend() *cobra.Command {
	command := &cobra.Command{
		Use:               "append KEY VALUE",
		Short:             "Append one element to a list value",
		Args:              cobra.ExactArgs(2),
		RunE:              runAppend,
		ValidArgsFunction: completeConfigKey,
	}
	command.Flags().SetInterspersed(false)
	return command
}

func newRemove() *cobra.Command {
	command := &cobra.Command{
		Use:               "remove KEY [VALUE]",
		Short:             "Remove a key or one list element",
		Aliases:           []string{"rm"},
		Args:              cobra.RangeArgs(1, 2),
		RunE:              runRemove,
		ValidArgsFunction: completeConfigKey,
	}
	command.Flags().SetInterspersed(false)
	return command
}

func runShow(cmd *cobra.Command, _ []string) error {
	gitDir, scope, err := prepare(cmd, sporconfig.ScopeEffective)
	if err != nil {
		return err
	}
	text, err := sporconfig.Show(gitDir, scope)
	if err != nil {
		return err
	}
	return writeOut(cmd, text)
}

func runGet(cmd *cobra.Command, args []string) error {
	gitDir, scope, err := prepare(cmd, sporconfig.ScopeEffective)
	if err != nil {
		return err
	}
	text, err := sporconfig.Get(gitDir, scope, args[0])
	if err != nil {
		return err
	}
	return writeOut(cmd, text)
}

func runSet(cmd *cobra.Command, args []string) error {
	gitDir, scope, err := prepare(cmd, sporconfig.ScopeWorktree)
	if err != nil {
		return err
	}
	values, err := sporconfig.ParseSetValue(args[0], args[1])
	if err != nil {
		return err
	}
	return sporconfig.Set(gitDir, scope, args[0], values)
}

func runAppend(cmd *cobra.Command, args []string) error {
	gitDir, scope, err := prepare(cmd, sporconfig.ScopeWorktree)
	if err != nil {
		return err
	}
	return sporconfig.Append(gitDir, scope, args[0], args[1])
}

func runRemove(cmd *cobra.Command, args []string) error {
	gitDir, scope, err := prepare(cmd, sporconfig.ScopeWorktree)
	if err != nil {
		return err
	}
	value, hasValue := optionalValue(args)
	return sporconfig.Remove(gitDir, scope, args[0], value, hasValue)
}

func prepare(cmd *cobra.Command, fallback sporconfig.Scope) (string, sporconfig.Scope, error) {
	gitDir, err := gitDirOf(cmd)
	if err != nil {
		return "", "", err
	}
	scope, err := scopeOf(cmd, fallback)
	if err != nil {
		return "", "", err
	}
	return gitDir, scope, nil
}

func gitDirOf(cmd *cobra.Command) (string, error) {
	start, err := cmd.Flags().GetString("repo")
	if err != nil {
		return "", err
	}
	repo, gitDir, err := repository.Open(start)
	if err != nil {
		return "", err
	}
	_ = repo.Close()
	return gitDir, nil
}

func scopeOf(cmd *cobra.Command, fallback sporconfig.Scope) (sporconfig.Scope, error) {
	global, err := cmd.Flags().GetBool("global")
	if err != nil {
		return "", err
	}
	repositoryScope, err := cmd.Flags().GetBool("repository")
	if err != nil {
		return "", err
	}
	return chooseScope(global, repositoryScope, fallback)
}

func chooseScope(global, repositoryScope bool, fallback sporconfig.Scope) (sporconfig.Scope, error) {
	if global {
		return globalScope(repositoryScope)
	}
	if repositoryScope {
		return sporconfig.ScopeRepository, nil
	}
	return fallback, nil
}

func globalScope(repositoryScope bool) (sporconfig.Scope, error) {
	if repositoryScope {
		return "", errors.New("config scope cannot be both global and repository")
	}
	return sporconfig.ScopeGlobal, nil
}

func completeConfigKey(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return matchingKeys(toComplete), cobra.ShellCompDirectiveNoFileComp
}

func matchingKeys(prefix string) []string {
	var matches []string
	for _, key := range sporconfig.Keys() {
		if strings.HasPrefix(key, prefix) {
			matches = append(matches, key)
		}
	}
	return matches
}

func optionalValue(args []string) (string, bool) {
	if len(args) < 2 {
		return "", false
	}
	return args[1], true
}

func writeOut(cmd *cobra.Command, text string) error {
	_, err := fmt.Fprint(cmd.OutOrStdout(), text)
	return err
}
