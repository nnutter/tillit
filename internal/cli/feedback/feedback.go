// Package feedback reports recorded validation evidence without running checks.
package feedback

import (
	json "encoding/json/v2"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nnutter/tillit/internal/auditor"
)

// New builds the feedback command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feedback",
		Short: "Report recorded validation failures and their output",
		Args:  cobra.NoArgs,
		RunE:  run,
	}
	cmd.Flags().Bool("json", false, "Print the report as JSON")
	return cmd
}

type report struct {
	Version  int                     `json:"version"`
	Branch   string                  `json:"branch"`
	Tip      string                  `json:"tip"`
	Boundary string                  `json:"boundary"`
	Summary  map[string]int          `json:"summary"`
	Commits  []auditor.BranchOutcome `json:"commits"`
}

func run(cmd *cobra.Command, _ []string) error {
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return err
	}
	asJSON, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	result, err := auditor.ReportEvidence(cmd.Context(), repo)
	if err != nil {
		return fmt.Errorf("report validation: %w", err)
	}
	report := makeReport(result)
	if asJSON {
		return json.MarshalWrite(cmd.OutOrStdout(), report)
	}
	return printReport(cmd.OutOrStdout(), report)
}

func makeReport(result auditor.BranchResult) report {
	summary := map[string]int{"passed": 0, "failed": 0, "pending": 0, "error": 0}
	for _, commit := range result.Outcomes {
		summary[commit.State]++
	}
	return report{
		Version: 1, Branch: result.Snapshot.Ref, Tip: result.Snapshot.Tip,
		Boundary: result.Snapshot.Boundary, Summary: summary, Commits: result.Outcomes,
	}
}

func printReport(w io.Writer, report report) error {
	var text strings.Builder
	_, _ = fmt.Fprintf(&text, "%s: %d passed, %d failed, %d pending, %d errors\n",
		report.Branch, report.Summary["passed"], report.Summary["failed"], report.Summary["pending"], report.Summary["error"])
	for _, commit := range report.Commits {
		if commit.Error != "" {
			_, _ = fmt.Fprintf(&text, "\n%s %s: %s\n", commit.Hash, commit.Subject, commit.Error)
			if commit.Evidence != nil {
				printEvidence(&text, commit.Evidence)
			}
		}
	}
	_, err := io.WriteString(w, text.String())
	return err
}

func printEvidence(w *strings.Builder, evidence *auditor.Evidence) {
	_, _ = fmt.Fprintf(w, "  Command: %q\n", evidence.Command)
	if len(evidence.Failures) == 0 {
		_, _ = fmt.Fprintln(w, strings.TrimRight(evidence.Output, "\n"))
	}
	for _, failure := range evidence.Failures {
		_, _ = fmt.Fprintln(w, " ", failure.Kind, failure.Package, failure.Test)
		_, _ = fmt.Fprintln(w, strings.TrimRight(failure.Output, "\n"))
		if failure.OutputTruncated {
			_, _ = fmt.Fprintln(w, "    Failure output truncated")
		}
		printReproductions(w, failure.Reproductions)
	}
	if evidence.OutputTruncated {
		_, _ = fmt.Fprintln(w, "  Command transcript truncated")
	}
}

func printReproductions(w *strings.Builder, runs []auditor.Reproduction) {
	for _, run := range runs {
		_, _ = fmt.Fprintf(w, "    Rerun (%s): %q\n", run.State, run.Command)
	}
}
