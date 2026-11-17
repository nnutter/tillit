package gh_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/gh"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *gh.Client) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, gh.NewClient(server.URL, "test-token")
}

func TestCheckTokenReportsLoginAndScopes(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/rate_limit":
			_, _ = w.Write([]byte(`{"resources":{"core":{"limit":5000,"remaining":4999}}}`))
		case "/user":
			w.Header().Set("X-OAuth-Scopes", "repo, read:org")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		default:
			http.NotFound(w, r)
		}
	})

	info, err := client.CheckToken(t.Context())
	require.NoError(t, err)
	require.Equal(t, "octocat", info.Login)
	require.Equal(t, []string{"repo", "read:org"}, info.Scopes)
	require.Equal(t, 5000, info.RateLimit)
	require.Equal(t, 4999, info.RateRemain)
	require.False(t, info.FineGrained)
}

func TestCheckTokenMarksFineGrained(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rate_limit":
			_, _ = w.Write([]byte(`{"resources":{"core":{"limit":5000,"remaining":100}}}`))
		case "/user":
			_, _ = w.Write([]byte(`{"login":"bot"}`))
		default:
			http.NotFound(w, r)
		}
	})

	info, err := client.CheckToken(t.Context())
	require.NoError(t, err)
	require.True(t, info.FineGrained)
	require.Empty(t, info.Scopes)
}

func TestCheckTokenRejectsUnauthorized(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	})

	_, err := client.CheckToken(t.Context())
	require.ErrorContains(t, err, "denied")
}

func TestFindPRForBranch(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !isExpectedPRQuery(r.URL.Query()) {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`[{"number":42,"html_url":"https://github.com/octo/repo/pull/42","head":{"sha":"abc123"}}]`))
	})

	pr, err := client.FindPRForBranch(t.Context(), "octo", "repo", "spor")
	require.NoError(t, err)
	require.Equal(t, 42, pr.Number)
	require.Equal(t, "abc123", pr.HeadSHA)
	require.Contains(t, pr.HTMLURL, "pull/42")
}

func TestFindPRForBranchReportsMissing(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})

	_, err := client.FindPRForBranch(t.Context(), "octo", "repo", "spor")
	require.ErrorContains(t, err, "no open pull request")
}

func TestPollChecksClassifiesRunsAndStatuses(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/octo/repo/commits/abc/check-runs":
			_, _ = w.Write([]byte(`{"check_runs":[
				{"id":1,"name":"pass","status":"completed","conclusion":"success","details_url":"https://x/1","app":{"slug":"github-actions"}},
				{"id":2,"name":"fail","status":"completed","conclusion":"failure","details_url":"https://x/2","app":{"slug":"github-actions"}},
				{"id":3,"name":"run","status":"in_progress","conclusion":"","details_url":"https://x/3","app":{"slug":"other"}}
			]}`))
		case "/repos/octo/repo/commits/abc/status":
			_, _ = w.Write([]byte(`{"state":"pending","statuses":[
				{"context":"lint","state":"success","target_url":"https://x/lint"}
			]}`))
		default:
			http.NotFound(w, r)
		}
	})

	outcome, err := client.PollChecks(t.Context(), "octo", "repo", gh.PullRequest{Number: 7, HeadSHA: "abc"})
	require.NoError(t, err)
	require.False(t, outcome.Complete)
	require.Len(t, outcome.Passed, 2)
	require.Len(t, outcome.Failed, 1)
	require.Len(t, outcome.Pending, 1)
	require.Equal(t, "fail", outcome.Failed[0].Name)
}

func TestFailureLogsDownloadsFailedJobs(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/octo/repo/actions/runs":
			if r.URL.Query().Get("head_sha") != "abc" {
				http.Error(w, "bad query", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":99}]}`))
		case "/repos/octo/repo/actions/runs/99/jobs":
			_, _ = w.Write([]byte(`{"jobs":[
				{"id":5,"name":"fail","conclusion":"failure","html_url":"https://x/job"},
				{"id":6,"name":"ok","conclusion":"success","html_url":"https://x/job2"}
			]}`))
		case "/repos/octo/repo/actions/jobs/5/logs":
			_, _ = w.Write([]byte("boom\ntraceback\n"))
		default:
			http.NotFound(w, r)
		}
	})

	logs, err := client.FailureLogs(t.Context(), "octo", "repo", "abc", []string{"fail", "missing"})
	require.NoError(t, err)
	require.Contains(t, logs["fail"], "boom")
	require.NotContains(t, logs, "missing")
}

func isExpectedPRQuery(query url.Values) bool {
	if query.Get("head") != "octo:spor" {
		return false
	}
	return query.Get("state") == "open"
}
