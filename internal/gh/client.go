package gh

import (
	"context"
	json "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client talks to the GitHub REST API with a read-only token.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// NewClient builds a client for base with token.
// An empty base means the default GitHub API endpoint.
func NewClient(base, token string) *Client {
	if strings.TrimSpace(base) == "" {
		base = APIEndpoint
	}
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

// APIBase returns the GitHub API endpoint, honoring GITHUB_API_URL for tests.
func APIBase() string {
	if base := strings.TrimSpace(os.Getenv("GITHUB_API_URL")); base != "" {
		return strings.TrimRight(base, "/")
	}
	return APIEndpoint
}

// TokenInfo describes a validated token.
type TokenInfo struct {
	Login       string
	Scopes      []string
	RateLimit   int
	RateRemain  int
	FineGrained bool
}

type rateLimitResponse struct {
	Resources struct {
		Core struct {
			Limit     int `json:"limit"`
			Remaining int `json:"remaining"`
		} `json:"core"`
	} `json:"resources"`
}

type userResponse struct {
	Login string `json:"login"`
}

// CheckToken validates the client token and reports its owner and scopes.

func parseScopes(header string) []string {
	var scopes []string
	for part := range strings.SplitSeq(header, ",") {
		if scope := strings.TrimSpace(part); scope != "" {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

// PullRequest is the pull request matching the feature branch.
type PullRequest struct {
	Number  int
	HeadSHA string
	HTMLURL string
}

type pullResponse struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Head    struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

// FindPRForBranch returns the open pull request whose head is owner:branch.

// CheckRun is one check run on a commit.
type CheckRun struct {
	ID         int64
	Name       string
	Status     string
	Conclusion string
	DetailsURL string
	AppSlug    string
}

type checkRunsResponse struct {
	CheckRuns []struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		DetailsURL string `json:"details_url"`
		App        struct {
			Slug string `json:"slug"`
		} `json:"app"`
	} `json:"check_runs"`
}

// ListCheckRuns returns the check runs for a commit SHA.

// CommitStatus is one combined commit status entry.
type CommitStatus struct {
	Context   string
	State     string
	TargetURL string
}

type combinedStatusResponse struct {
	State    string `json:"state"`
	Statuses []struct {
		Context   string `json:"context"`
		State     string `json:"state"`
		TargetURL string `json:"target_url"`
	} `json:"statuses"`
}

// CombinedStatus returns the combined commit status state and its entries.

// isAuthFailure reports whether an API response means the token is rejected.
func isAuthFailure(code int) bool {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return true
	default:
		return false
	}
}

func (c *Client) CheckToken(ctx context.Context) (TokenInfo, error) {
	var info TokenInfo
	limitBody, limitHeader, err := c.get(ctx, "/rate_limit", nil)
	if err != nil {
		return info, err
	}
	var limit rateLimitResponse
	if err := json.Unmarshal(limitBody, &limit); err != nil {
		return info, fmt.Errorf("decode rate limit: %w", err)
	}
	info.RateLimit = limit.Resources.Core.Limit
	info.RateRemain = limit.Resources.Core.Remaining
	_ = limitHeader

	userBody, userHeader, err := c.get(ctx, "/user", nil)
	if err != nil {
		return info, err
	}
	var user userResponse
	if err := json.Unmarshal(userBody, &user); err != nil {
		return info, fmt.Errorf("decode user: %w", err)
	}
	info.Login = user.Login
	info.Scopes = parseScopes(userHeader.Get("X-OAuth-Scopes"))
	info.FineGrained = len(info.Scopes) == 0
	return info, nil
}

func (c *Client) CombinedStatus(ctx context.Context, owner, repo, sha string) (string, []CommitStatus, error) {
	body, _, err := c.get(ctx, "/repos/"+owner+"/"+repo+"/commits/"+sha+"/status", nil)
	if err != nil {
		return "", nil, err
	}
	var response combinedStatusResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", nil, fmt.Errorf("decode commit status: %w", err)
	}
	statuses := make([]CommitStatus, 0, len(response.Statuses))
	for _, status := range response.Statuses {
		statuses = append(statuses, CommitStatus{Context: status.Context, State: status.State, TargetURL: status.TargetURL})
	}
	return response.State, statuses, nil
}

func (c *Client) FailureLogs(ctx context.Context, owner, repo, sha string, failed []string) (map[string]string, error) {
	logs := make(map[string]string, len(failed))
	if len(failed) == 0 {
		return logs, nil
	}
	remaining := make(map[string]bool, len(failed))
	for _, name := range failed {
		remaining[name] = true
	}
	runs, err := c.WorkflowRunsForSHA(ctx, owner, repo, sha)
	if err != nil {
		return nil, err
	}
	for _, runID := range runs {
		if len(remaining) == 0 {
			return logs, nil
		}
		if err := c.collectRunLogs(ctx, owner, repo, runID, remaining, logs); err != nil {
			return nil, err
		}
	}
	return logs, nil
}

func (c *Client) FindPRForBranch(ctx context.Context, owner, repo, branch string) (PullRequest, error) {
	var result PullRequest
	query := url.Values{}
	query.Set("head", owner+":"+branch)
	query.Set("state", "open")
	body, _, err := c.get(ctx, "/repos/"+owner+"/"+repo+"/pulls", query)
	if err != nil {
		return result, err
	}
	var pulls []pullResponse
	if err := json.Unmarshal(body, &pulls); err != nil {
		return result, fmt.Errorf("decode pull requests: %w", err)
	}
	if len(pulls) == 0 {
		return result, fmt.Errorf("no open pull request for %s:%s", owner, branch)
	}
	result.Number = pulls[0].Number
	result.HeadSHA = pulls[0].Head.SHA
	result.HTMLURL = pulls[0].HTMLURL
	return result, nil
}

func (c *Client) JobLogs(ctx context.Context, owner, repo string, jobID int64) (string, error) {
	target := fmt.Sprintf("%s/repos/%s/%s/actions/jobs/%d/logs", c.base, owner, repo, jobID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("request job logs: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return "", fmt.Errorf("read job logs: %w", err)
	}
	if !statusOK(response.StatusCode) {
		return "", fmt.Errorf("job logs returned %s", response.Status)
	}
	return string(body), nil
}

func (c *Client) JobsForRun(ctx context.Context, owner, repo string, runID int64) ([]ActionJob, error) {
	body, _, err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs", owner, repo, runID), nil)
	if err != nil {
		return nil, err
	}
	var response jobsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode workflow jobs: %w", err)
	}
	jobs := make([]ActionJob, 0, len(response.Jobs))
	for _, job := range response.Jobs {
		jobs = append(jobs, ActionJob{ID: job.ID, Name: job.Name, Conclusion: job.Conclusion, HTMLURL: job.HTMLURL})
	}
	return jobs, nil
}

func (c *Client) ListCheckRuns(ctx context.Context, owner, repo, sha string) ([]CheckRun, error) {
	body, _, err := c.get(ctx, "/repos/"+owner+"/"+repo+"/commits/"+sha+"/check-runs", nil)
	if err != nil {
		return nil, err
	}
	var response checkRunsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode check runs: %w", err)
	}
	runs := make([]CheckRun, 0, len(response.CheckRuns))
	for _, run := range response.CheckRuns {
		runs = append(runs, CheckRun{
			ID:         run.ID,
			Name:       run.Name,
			Status:     run.Status,
			Conclusion: run.Conclusion,
			DetailsURL: run.DetailsURL,
			AppSlug:    run.App.Slug,
		})
	}
	return runs, nil
}

func (c *Client) PollChecks(ctx context.Context, owner, repo string, pr PullRequest) (WatchOutcome, error) {
	outcome := WatchOutcome{Number: pr.Number, SHA: pr.HeadSHA, URL: pr.HTMLURL}
	runs, err := c.ListCheckRuns(ctx, owner, repo, pr.HeadSHA)
	if err != nil {
		return outcome, err
	}
	state, statuses, err := c.CombinedStatus(ctx, owner, repo, pr.HeadSHA)
	if err != nil {
		return outcome, err
	}
	outcome = appendCheckRuns(outcome, runs)
	outcome = appendCommitStatuses(outcome, state, statuses)
	outcome.Complete = isSettled(outcome, state)
	return outcome, nil
}

func (c *Client) WaitForChecks(ctx context.Context, owner, repo string, pr PullRequest, interval, timeout time.Duration, onPoll func(WatchOutcome)) (WatchOutcome, error) {
	var outcome WatchOutcome
	timeoutCtx, cancel := context.WithTimeoutCause(ctx, timeout, fmt.Errorf("watch timed out after %s", timeout))
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		poll, err := c.PollChecks(timeoutCtx, owner, repo, pr)
		if err != nil {
			return outcome, err
		}
		outcome = poll
		if onPoll != nil {
			onPoll(poll)
		}
		if poll.Complete {
			return poll, nil
		}
		select {
		case <-timeoutCtx.Done():
			return poll, fmt.Errorf("watch %s: %w", pr.HeadSHA, context.Cause(timeoutCtx))
		case <-ticker.C:
		}
	}
}

func (c *Client) WorkflowRunsForSHA(ctx context.Context, owner, repo, sha string) ([]int64, error) {
	query := url.Values{}
	query.Set("head_sha", sha)
	query.Set("per_page", "10")
	body, _, err := c.get(ctx, "/repos/"+owner+"/"+repo+"/actions/runs", query)
	if err != nil {
		return nil, err
	}
	var response workflowRunsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode workflow runs: %w", err)
	}
	ids := make([]int64, 0, len(response.WorkflowRuns))
	for _, run := range response.WorkflowRuns {
		ids = append(ids, run.ID)
	}
	return ids, nil
}

func (c *Client) collectRunLogs(ctx context.Context, owner, repo string, runID int64, remaining map[string]bool, logs map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	jobs, err := c.JobsForRun(ctx, owner, repo, runID)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if !remaining[job.Name] {
			continue
		}
		delete(remaining, job.Name)
		if !isFailedConclusion(job.Conclusion) {
			continue
		}
		content, err := c.JobLogs(ctx, owner, repo, job.ID)
		if err != nil {
			return err
		}
		logs[job.Name] = content
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, http.Header, error) {
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("request %s: %w", path, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	if isAuthFailure(response.StatusCode) {
		return nil, nil, fmt.Errorf("github API denied %s: %s", path, strings.TrimSpace(string(body)))
	}
	if !statusOK(response.StatusCode) {
		return nil, nil, fmt.Errorf("github API %s returned %s", path, response.Status)
	}
	return body, response.Header, nil
}
