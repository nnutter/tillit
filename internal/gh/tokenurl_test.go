package gh_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/gh"
)

func TestNewTokenURLPrefillsTemplate(t *testing.T) {
	rawURL := gh.NewTokenURL(gh.WatchTokenTemplate("octo"))
	require.True(t, strings.HasPrefix(rawURL, "https://github.com/settings/personal-access-tokens/new?"), rawURL)
	require.Contains(t, rawURL, "target_name=octo")
	require.Contains(t, rawURL, "expires_in=90")
	require.Contains(t, rawURL, "name=Spor+PR+watcher")
	for _, permission := range []string{"actions=read", "checks=read", "contents=read", "metadata=read", "pull_requests=read", "statuses=read"} {
		require.Contains(t, rawURL, permission)
	}
}

func TestNewTokenURLEncodesFields(t *testing.T) {
	rawURL := gh.NewTokenURL(gh.TokenTemplate{Name: "a & b", Description: "read: write?"})
	require.Contains(t, rawURL, "name=a+%26+b")
	require.Contains(t, rawURL, "description=read%3A+write%3F")
	require.NotContains(t, rawURL, "target_name")
	require.NotContains(t, rawURL, "expires_in")
}

func TestWatchTokenTemplatePermissions(t *testing.T) {
	template := gh.WatchTokenTemplate("")
	require.Empty(t, template.Target)
	require.Equal(t, map[string]string{
		"actions":       "read",
		"checks":        "read",
		"contents":      "read",
		"metadata":      "read",
		"pull_requests": "read",
		"statuses":      "read",
	}, template.Permissions)
}
