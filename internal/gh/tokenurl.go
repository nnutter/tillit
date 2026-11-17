package gh

import (
	"maps"
	"net/url"
	"slices"
)

// TokenTemplate describes a pre-filled fine-grained token creation page.
// Field names follow the documented query parameters for
// https://github.com/settings/personal-access-tokens/new.
type TokenTemplate struct {
	Name        string
	Description string
	// Target is the resource owner slug. Empty means the current user.
	Target string
	// ExpiresIn is days until expiration, or "none". Empty means the default.
	ExpiresIn string
	// Permissions maps a permission slug to read, write, or admin.
	Permissions map[string]string
}

// NewTokenURL returns the token creation URL with the template pre-filled.
func NewTokenURL(template TokenTemplate) string {
	query := url.Values{}
	if template.Name != "" {
		query.Set("name", template.Name)
	}
	if template.Description != "" {
		query.Set("description", template.Description)
	}
	if template.Target != "" {
		query.Set("target_name", template.Target)
	}
	if template.ExpiresIn != "" {
		query.Set("expires_in", template.ExpiresIn)
	}
	for _, permission := range slices.Sorted(maps.Keys(template.Permissions)) {
		query.Set(permission, template.Permissions[permission])
	}
	return "https://github.com/settings/personal-access-tokens/new?" + query.Encode()
}

// WatchTokenTemplate returns the pre-filled template for watching pull requests.
// The token only needs read access to checks and their logs.
func WatchTokenTemplate(target string) TokenTemplate {
	return TokenTemplate{
		Name:        "Spor PR watcher",
		Description: "Read-only token for spor gh watch-pr: pull request checks and failure logs.",
		ExpiresIn:   "90",
		Target:      target,
		Permissions: map[string]string{
			"actions":       "read",
			"checks":        "read",
			"contents":      "read",
			"metadata":      "read",
			"pull_requests": "read",
			"statuses":      "read",
		},
	}
}
