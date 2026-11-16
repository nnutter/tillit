package gh

// CheckSummary is one watched check or status entry.
type CheckSummary struct {
	Name       string
	Status     string
	Conclusion string
	URL        string
}

// WatchOutcome is the state of a pull request head after one poll.
type WatchOutcome struct {
	Number   int
	SHA      string
	URL      string
	Passed   []CheckSummary
	Failed   []CheckSummary
	Pending  []CheckSummary
	Complete bool
}

// PollChecks reads check runs and combined statuses once for a commit SHA.

func appendCheckRuns(outcome WatchOutcome, runs []CheckRun) WatchOutcome {
	for _, run := range runs {
		summary := CheckSummary{Name: run.Name, Status: run.Status, Conclusion: run.Conclusion, URL: run.DetailsURL}
		switch classifyRun(run) {
		case checkPassed:
			outcome.Passed = append(outcome.Passed, summary)
		case checkFailed:
			outcome.Failed = append(outcome.Failed, summary)
		default:
			outcome.Pending = append(outcome.Pending, summary)
		}
	}
	return outcome
}

func appendCommitStatuses(outcome WatchOutcome, state string, statuses []CommitStatus) WatchOutcome {
	for _, status := range statuses {
		summary := CheckSummary{Name: status.Context, Status: status.State, URL: status.TargetURL}
		switch classifyStatus(status.State) {
		case checkPassed:
			outcome.Passed = append(outcome.Passed, summary)
		case checkFailed:
			summary.Conclusion = status.State
			outcome.Failed = append(outcome.Failed, summary)
		default:
			outcome.Pending = append(outcome.Pending, summary)
		}
	}
	return outcome
}

func isSettled(outcome WatchOutcome, state string) bool {
	if len(outcome.Pending) > 0 {
		return false
	}
	if len(outcome.Passed)+len(outcome.Failed) > 0 {
		return true
	}
	return isFinishedState(state)
}

func isFinishedState(state string) bool {
	if state == "" {
		return false
	}
	return !isPendingState(state)
}

func isPendingState(state string) bool {
	return state == "pending"
}

func classifyStatus(state string) checkClass {
	switch state {
	case "success", "skipped":
		return checkPassed
	case "failure", "error":
		return checkFailed
	default:
		return checkPending
	}
}

type checkClass int

const (
	checkPassed checkClass = iota
	checkFailed
	checkPending
)

func classifyRun(run CheckRun) checkClass {
	if run.Status == "completed" {
		return classifyConclusion(run.Conclusion)
	}
	if run.Conclusion == "" {
		return checkPending
	}
	return checkFailed
}

func classifyConclusion(conclusion string) checkClass {
	switch conclusion {
	case "success", "skipped", "neutral":
		return checkPassed
	case "":
		return checkPending
	default:
		return checkFailed
	}
}

// WaitForChecks polls until checks complete or the timeout elapses.
// onPoll observes each poll, including the first.
