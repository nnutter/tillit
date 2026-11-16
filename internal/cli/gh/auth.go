package gh

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

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
	command.Flags().String("token", "", "store this token instead of opening the creation page")
	command.Flags().Bool("import-gh", false, "copy the token the gh CLI already holds into the keyring")
	command.Flags().Bool("no-open", false, "print the token creation URL without opening a browser")
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
	return openTokenPage(cmd, ring)
}

func openTokenPage(cmd *cobra.Command, ring sporgh.Ring) error {
	target := loginTarget(cmd)
	rawURL := sporgh.NewTokenURL(sporgh.WatchTokenTemplate(target))
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\n", loginGuidance(rawURL)); err != nil {
		return err
	}
	if err := maybeOpenBrowser(cmd, rawURL); err != nil {
		return err
	}
	token, pasted, err := readPastedToken(cmd)
	if err != nil {
		return err
	}
	if !pasted {
		return nil
	}
	if err := storeValidated(cmd, ring, token); err != nil {
		return err
	}
	return printStored(cmd, sporgh.SourceKeyring)
}

func maybeOpenBrowser(cmd *cobra.Command, rawURL string) error {
	noOpen, err := cmd.Flags().GetBool("no-open")
	if err != nil {
		return err
	}
	if noOpen {
		return nil
	}
	if err := sporgh.OpenURL(rawURL); err != nil {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Could not open a browser; use the URL above.")
	}
	return nil
}

// readPastedToken waits for the user to paste the token they just created.
// It reports false when stdin carries nothing, leaving the printed guidance
// as the whole interaction for headless sessions.
func readPastedToken(cmd *cobra.Command) (string, bool, error) {
	if file, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return readTerminalToken(cmd, file)
	}
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", false, fmt.Errorf("read token from stdin: %w", err)
	}
	token := strings.TrimSpace(string(data))
	return token, token != "", nil
}

func readTerminalToken(cmd *cobra.Command, stdin *os.File) (string, bool, error) {
	if _, err := fmt.Fprint(cmd.OutOrStdout(), "Paste your token (input hidden): "); err != nil {
		return "", false, err
	}
	secret, err := term.ReadPassword(int(stdin.Fd()))
	if err != nil {
		return "", false, fmt.Errorf("read token from terminal: %w", err)
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout())
	token := strings.TrimSpace(string(secret))
	return token, token != "", nil
}

func loginTarget(cmd *cobra.Command) string {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return ""
	}
	owner, _, err := sporgh.OwnerRepo(cmd.Context(), repo)
	if err != nil {
		return ""
	}
	return owner
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

func loginGuidance(rawURL string) string {
	var guidance strings.Builder
	guidance.WriteString("Create a read-only fine-grained personal access token:\n")
	guidance.WriteString("  1. Open " + rawURL + "\n")
	guidance.WriteString("  2. Confirm read access to Checks, Actions, Pull requests, Contents, and Metadata.\n")
	guidance.WriteString("  3. Paste the token at the prompt to store it, or store it later with: spor gh auth login --token TOKEN\n")
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
