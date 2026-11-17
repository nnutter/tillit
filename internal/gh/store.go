package gh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/99designs/keyring"
)

// Ring abstracts the keyring backend so tests can substitute a fake.
type Ring interface {
	Get(key string) (keyring.Item, error)
	Set(item keyring.Item) error
	Remove(key string) error
}

// OpenRing opens the operating-system keyring holding Spor secrets.
// It is a variable so tests can substitute a fake backend.
var OpenRing = func() (Ring, error) {
	return keyring.Open(keyring.Config{ServiceName: ServiceName})
}

// Source names where a resolved token came from.
const (
	SourceEnvGH     = "GH_TOKEN"
	SourceEnvGitHub = "GITHUB_TOKEN"
	SourceKeyring   = "keyring"
)

// ResolvedToken is a token together with the place it was found.
type ResolvedToken struct {
	Token  string
	Source string
}

// ResolveToken returns the GitHub token for read-only API use.
//
// It checks GH_TOKEN, then GITHUB_TOKEN, then the system keyring.
// An empty token with no error means no token is configured.
func ResolveToken(ring Ring) (ResolvedToken, error) {
	if token := strings.TrimSpace(os.Getenv("GH_TOKEN")); token != "" {
		return ResolvedToken{Token: token, Source: SourceEnvGH}, nil
	}
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		return ResolvedToken{Token: token, Source: SourceEnvGitHub}, nil
	}
	if ring == nil {
		return ResolvedToken{}, nil
	}
	item, err := ring.Get(TokenKey)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return ResolvedToken{}, nil
	}
	if err != nil {
		return ResolvedToken{}, fmt.Errorf("read token from keyring: %w", err)
	}
	if len(item.Data) == 0 {
		return ResolvedToken{}, nil
	}
	return ResolvedToken{Token: string(item.Data), Source: SourceKeyring}, nil
}

// StoreToken saves the token in the system keyring.
func StoreToken(ring Ring, token string) error {
	if ring == nil {
		return fmt.Errorf("no keyring backend is available")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token is empty")
	}
	if err := ring.Set(keyring.Item{Key: TokenKey, Data: []byte(token), Label: "Spor GitHub read-only token"}); err != nil {
		return fmt.Errorf("store token in keyring: %w", err)
	}
	return nil
}

// DeleteToken removes the token from the system keyring.
func DeleteToken(ring Ring) error {
	if ring == nil {
		return fmt.Errorf("no keyring backend is available")
	}
	err := ring.Remove(TokenKey)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove token from keyring: %w", err)
	}
	return nil
}

// GHTokenFunc runs `gh auth token` and returns its output.
// It is a variable so tests can substitute a fake.
var GHTokenFunc = func(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "auth", "token")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("run gh auth token: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ImportGHToken copies the token that the gh CLI already holds into the keyring.
func ImportGHToken(ctx context.Context, ring Ring) (ResolvedToken, error) {
	token, err := GHTokenFunc(ctx)
	if err != nil {
		return ResolvedToken{}, err
	}
	if token == "" {
		return ResolvedToken{}, fmt.Errorf("gh CLI holds no token; run gh auth login first")
	}
	if err := StoreToken(ring, token); err != nil {
		return ResolvedToken{}, err
	}
	return ResolvedToken{Token: token, Source: SourceKeyring}, nil
}
