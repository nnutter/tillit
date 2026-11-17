package gh

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	sporgh "github.com/nnutter/spor/internal/gh"
	"github.com/nnutter/spor/internal/repository"
)

// watchConfig carries the watch-pr flag values.
type watchConfig struct {
	repoPath  string
	interval  time.Duration
	timeout   time.Duration
	once      bool
	fetchLogs bool
	logsDir   string
}

func newWatch() *cobra.Command {
	command := &cobra.Command{
		Use:   "watch-pr",
		Short: "Watch the pull request for the feature branch",
		Args:  cobra.NoArgs,
		RunE:  runWatch,
	}
	command.Flags().Duration("interval", 20*time.Second, "time between status polls")
	command.Flags().Duration("timeout", 30*time.Minute, "give up after this long")
	command.Flags().Bool("once", false, "poll once instead of watching")
	command.Flags().Bool("logs", true, "fetch failure logs for failing checks")
	command.Flags().String("logs-dir", "", "where to save failure logs (default <gitdir>/spor/gh-logs)")
	return command
}

func runWatch(cmd *cobra.Command, _ []string) error {
	cfg, err := watchConfigFromFlags(cmd)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	branch, err := sporgh.CurrentBranch(ctx, cfg.repoPath)
	if err != nil {
		return err
	}
	owner, name, err := sporgh.OwnerRepo(ctx, cfg.repoPath)
	if err != nil {
		return err
	}
	token, err := resolveWatchToken()
	if err != nil {
		return err
	}
	client := sporgh.NewClient(sporgh.APIBase(), token)
	pr, err := client.FindPRForBranch(ctx, owner, name, branch)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "pr: #%d %s %s\n", pr.Number, pr.HeadSHA, pr.HTMLURL); err != nil {
		return err
	}
	outcome, err := pollFeaturePR(cmd, client, owner, name, pr, cfg)
	if err != nil {
		return err
	}
	printOutcome(cmd, outcome)
	if len(outcome.Failed) == 0 {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "watch-pr: all %d checks passed\n", len(outcome.Passed))
		return err
	}
	if err := maybeSaveLogs(cmd, client, owner, name, pr, outcome, cfg); err != nil {
		return err
	}
	_, _ = fmt.Fprint(cmd.OutOrStdout(), sporgh.ReproduceHint(owner, name, pr.Number))
	return fmt.Errorf("watch-pr: %d failing checks on pr #%d", len(outcome.Failed), pr.Number)
}

func watchConfigFromFlags(cmd *cobra.Command) (watchConfig, error) {
	var cfg watchConfig
	var err error
	if cfg.repoPath, err = cmd.Flags().GetString("repo"); err != nil {
		return cfg, err
	}
	if cfg.interval, err = cmd.Flags().GetDuration("interval"); err != nil {
		return cfg, err
	}
	if cfg.timeout, err = cmd.Flags().GetDuration("timeout"); err != nil {
		return cfg, err
	}
	if cfg.once, err = cmd.Flags().GetBool("once"); err != nil {
		return cfg, err
	}
	if cfg.fetchLogs, err = cmd.Flags().GetBool("logs"); err != nil {
		return cfg, err
	}
	cfg.logsDir, err = cmd.Flags().GetString("logs-dir")
	return cfg, err
}

// resolveWatchToken returns the token from the environment or the keyring.
func resolveWatchToken() (string, error) {
	ring, ringErr := sporgh.OpenRing()
	var resolved sporgh.ResolvedToken
	var err error
	if ringErr == nil {
		resolved, err = sporgh.ResolveToken(ring)
	} else {
		resolved, err = sporgh.ResolveToken(nil)
	}
	if err != nil {
		return "", err
	}
	if resolved.Token == "" {
		return "", missingTokenError(ringErr)
	}
	return resolved.Token, nil
}

func missingTokenError(ringErr error) error {
	hint := "no GitHub token is configured; run spor gh auth login"
	if ringErr != nil {
		hint += fmt.Sprintf(" (%v)", ringErr)
	}
	return errors.New(hint)
}

func pollFeaturePR(cmd *cobra.Command, client *sporgh.Client, owner, name string, pr sporgh.PullRequest, cfg watchConfig) (sporgh.WatchOutcome, error) {
	if cfg.once {
		return client.PollChecks(cmd.Context(), owner, name, pr)
	}
	return client.WaitForChecks(cmd.Context(), owner, name, pr, cfg.interval, cfg.timeout, func(poll sporgh.WatchOutcome) {
		printPoll(cmd, poll)
	})
}

func maybeSaveLogs(cmd *cobra.Command, client *sporgh.Client, owner, name string, pr sporgh.PullRequest, outcome sporgh.WatchOutcome, cfg watchConfig) error {
	if !cfg.fetchLogs {
		return nil
	}
	dir := cfg.logsDir
	if dir == "" {
		gitDir, err := worktreeGitDir(cfg.repoPath)
		if err != nil {
			return err
		}
		dir = filepath.Join(gitDir, "spor", "gh-logs")
	}
	return saveFailureLogs(cmd, client, owner, name, pr, outcome, dir)
}

func worktreeGitDir(repoPath string) (string, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return "", err
	}
	_ = repo.Close()
	return gitDir, nil
}

func printPoll(cmd *cobra.Command, poll sporgh.WatchOutcome) {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "poll: %d passed, %d failed, %d pending\n",
		len(poll.Passed), len(poll.Failed), len(poll.Pending))
}

func printOutcome(cmd *cobra.Command, outcome sporgh.WatchOutcome) {
	for _, passed := range outcome.Passed {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "pass: %s\n", passed.Name)
	}
	for _, failed := range outcome.Failed {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", formatFailed(failed))
	}
	for _, pending := range outcome.Pending {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "pending: %s (%s)\n", pending.Name, pending.Status)
	}
}

func formatFailed(failed sporgh.CheckSummary) string {
	line := "fail: " + failed.Name
	if failed.Conclusion != "" {
		line += " (" + failed.Conclusion + ")"
	}
	if failed.URL != "" {
		line += " " + failed.URL
	}
	return line
}

func saveFailureLogs(cmd *cobra.Command, client *sporgh.Client, owner, name string, pr sporgh.PullRequest, outcome sporgh.WatchOutcome, dir string) error {
	failed := make([]string, 0, len(outcome.Failed))
	for _, item := range outcome.Failed {
		failed = append(failed, item.Name)
	}
	logs, err := client.FailureLogs(cmd.Context(), owner, name, outcome.SHA, failed)
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, fmt.Sprintf("pr-%d-%s", pr.Number, shortSHA(outcome.SHA)))
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, item := range outcome.Failed {
		if err := saveOneLog(cmd, dest, item, logs); err != nil {
			return err
		}
	}
	return nil
}

func saveOneLog(cmd *cobra.Command, dest string, failed sporgh.CheckSummary, logs map[string]string) error {
	content, ok := logs[failed.Name]
	if !ok {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "logs: none found for %s\n", failed.Name)
		return nil
	}
	path := filepath.Join(dest, sanitizeName(failed.Name)+".log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "logs: %s\n%s\n", path, excerpt(content, 20))
	return nil
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func sanitizeName(name string) string {
	return strings.Map(func(r rune) rune {
		if isLogNameRune(r) {
			return r
		}
		return '_'
	}, name)
}

func isLogNameRune(r rune) bool {
	switch r {
	case '-', '_':
		return true
	default:
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	}
}

func excerpt(content string, lines int) string {
	trimmed := strings.TrimRight(content, "\n")
	parts := strings.Split(trimmed, "\n")
	if len(parts) <= lines {
		return trimmed
	}
	return strings.Join(parts[len(parts)-lines:], "\n")
}
