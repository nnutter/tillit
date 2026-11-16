package gh

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	sporgh "github.com/nnutter/spor/internal/gh"
)

func newAuth() *cobra.Command {
	command := &cobra.Command{
		Use:   "auth",
		Short: "Manage the GitHub token",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newLogin(), newStatus())
	return command
}

func newLogin() *cobra.Command {
	command := &cobra.Command{
		Use:   "login",
		Short: "Store a read-only GitHub token in the system keyring",
		Args:  cobra.NoArgs,
		RunE:  runLogin,
	}
	command.Flags().String("token", "", "store this token instead of printing setup guidance")
	command.Flags().Bool("import-gh", false, "copy the token the gh CLI already holds into the keyring")
	return command
}

func newStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report the stored token and its scopes",
		Args:  cobra.NoArgs,
		RunE:  runStatus,
	}
}

func runLogin(cmd *cobra.Command, _ []string) error {
	token, err := cmd.Flags().GetString("token")
	if err != nil {
		return err
	}
	importGH, err := cmd.Flags().GetBool("import-gh")
	if err != nil {
		return err
	}
	if importGH && tokenFlagSet(token) {
		return fmt.Errorf("login accepts only one of --token and --import-gh")
	}
	ring, err := sporgh.OpenRing()
	if err != nil {
		return fmt.Errorf("open system keyring: %w", err)
	}
	if importGH {
		resolved, err := sporgh.ImportGHToken(cmd.Context(), ring)
		if err != nil {
			return err
		}
		return printStored(cmd, resolved.Source)
	}
	if tokenFlagSet(token) {
		if err := storeValidated(cmd, ring, strings.TrimSpace(token)); err != nil {
			return err
		}
		return printStored(cmd, sporgh.SourceKeyring)
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), loginGuidance())
	return err
}

func tokenFlagSet(token string) bool {
	return strings.TrimSpace(token) != ""
}

func storeValidated(cmd *cobra.Command, ring sporgh.Ring, token string) error {
	client := sporgh.NewClient(sporgh.APIBase(), token)
	info, err := client.CheckToken(cmd.Context())
	if err != nil {
		return fmt.Errorf("validate token: %w", err)
	}
	if err := sporgh.StoreToken(ring, token); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Validated token for %s; stored in %s.\n", displayLogin(info.Login), sporgh.SourceKeyring)
	return err
}

func printStored(cmd *cobra.Command, source string) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Stored GitHub token in %s.\n", source)
	return err
}

func displayLogin(login string) string {
	if login == "" {
		return "the token owner"
	}
	return login
}

func loginGuidance() string {
	var guidance strings.Builder
	guidance.WriteString("Create a read-only fine-grained personal access token:\n")
	guidance.WriteString("  1. Open https://github.com/settings/personal-access-tokens/new\n")
	guidance.WriteString("  2. Grant read access to Checks, Actions, Pull requests, Contents, and Metadata.\n")
	guidance.WriteString("  3. Store it with: spor gh auth login --token TOKEN\n")
	guidance.WriteString("  Or copy the gh CLI token with: spor gh auth login --import-gh\n")
	return guidance.String()
}

func runStatus(cmd *cobra.Command, _ []string) error {
	ring, ringErr := sporgh.OpenRing()
	var resolved sporgh.ResolvedToken
	var err error
	if ringErr == nil {
		resolved, err = sporgh.ResolveToken(ring)
		if err != nil {
			return err
		}
	} else {
		resolved, err = sporgh.ResolveToken(nil)
		if err != nil {
			return err
		}
	}
	if resolved.Token == "" {
		hint := "no GitHub token is configured; run spor gh auth login"
		if ringErr != nil {
			hint += fmt.Sprintf(" (%v)", ringErr)
		}
		return fmt.Errorf("%s", hint)
	}
	client := sporgh.NewClient(sporgh.APIBase(), resolved.Token)
	info, err := client.CheckToken(cmd.Context())
	if err != nil {
		return fmt.Errorf("validate token from %s: %w", resolved.Source, err)
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), formatStatus(resolved.Source, info))
	return err
}

func formatStatus(source string, info sporgh.TokenInfo) string {
	var status strings.Builder
	fmt.Fprintf(&status, "token: %s\n", source)
	fmt.Fprintf(&status, "login: %s\n", displayLogin(info.Login))
	if len(info.Scopes) == 0 {
		status.WriteString("scopes: fine-grained (per-repository permissions are not listed by the API)\n")
	} else {
		fmt.Fprintf(&status, "scopes: %s\n", strings.Join(info.Scopes, ", "))
	}
	fmt.Fprintf(&status, "rate: %d/%d remaining\n", info.RateRemain, info.RateLimit)
	return status.String()
}
