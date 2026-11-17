// Package gh monitors GitHub pull-request checks without replacing local validation.
//
// It stores a read-only GitHub token in the operating-system keyring,
// reports the token status and scopes, finds the pull request for the
// current feature branch, and polls its checks while fetching failure logs.
package gh

// TokenKey is the keyring item key holding the GitHub token.
const TokenKey = "github-token"

// ServiceName is the keyring service name holding Spor secrets.
const ServiceName = "spor"

// APIEndpoint is the default GitHub REST API base URL.
const APIEndpoint = "https://api.github.com"
