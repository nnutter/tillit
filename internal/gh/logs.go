package gh

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// ActionJob is one GitHub Actions job in a workflow run.
type ActionJob struct {
	ID         int64
	Name       string
	Conclusion string
	HTMLURL    string
}

// FailedConclusions counts an Actions job as a failure.
var FailedConclusions = []string{"failure", "timed_out", "cancelled"}

type workflowRunsResponse struct {
	WorkflowRuns []struct {
		ID int64 `json:"id"`
	} `json:"workflow_runs"`
}

type jobsResponse struct {
	Jobs []struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
	} `json:"jobs"`
}

// WorkflowRunsForSHA lists workflow run IDs started by a head SHA.

// JobsForRun lists the jobs of one workflow run.

// JobLogs downloads the log text of one Actions job.

func statusOK(code int) bool {
	if code < http.StatusOK {
		return false
	}
	return code < http.StatusMultipleChoices
}

// isFailedConclusion reports whether an Actions job conclusion counts as a failure.
func isFailedConclusion(conclusion string) bool {
	return slices.Contains(FailedConclusions, conclusion)
}

// FailureLogs downloads logs for failed checks, keyed by check name.
// It matches each failed check name to an Actions job of the same name.

// ReproduceHint returns the git commands that check out a pull request head.
func ReproduceHint(owner, repo string, number int) string {
	var hint strings.Builder
	hint.WriteString("To reproduce locally:\n")
	fmt.Fprintf(&hint, "  git fetch origin pull/%d/head:pr-%d\n", number, number)
	fmt.Fprintf(&hint, "  git checkout pr-%d\n", number)
	fmt.Fprintf(&hint, "  git log --oneline -5\n")
	_ = owner
	_ = repo
	return hint.String()
}
