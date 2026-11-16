package gh_test

import (
	"context"
	"errors"
	"testing"

	"github.com/99designs/keyring"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/gh"
)

type fakeRing struct {
	items map[string][]byte
	get   error
	set   error
}

func newFakeRing() *fakeRing {
	return &fakeRing{items: map[string][]byte{}}
}

func (f *fakeRing) Get(key string) (keyring.Item, error) {
	if f.get != nil {
		return keyring.Item{}, f.get
	}
	data, ok := f.items[key]
	if !ok {
		return keyring.Item{}, keyring.ErrKeyNotFound
	}
	return keyring.Item{Key: key, Data: data}, nil
}

func (f *fakeRing) Remove(key string) error {
	delete(f.items, key)
	return nil
}

func (f *fakeRing) Set(item keyring.Item) error {
	if f.set != nil {
		return f.set
	}
	f.items[item.Key] = item.Data
	return nil
}

func TestResolveTokenPrefersEnv(t *testing.T) {
	t.Setenv("GH_TOKEN", "env-token")
	t.Setenv("GITHUB_TOKEN", "other-token")
	ring := newFakeRing()
	ring.items[gh.TokenKey] = []byte("keyring-token")

	resolved, err := gh.ResolveToken(ring)
	require.NoError(t, err)
	require.Equal(t, "env-token", resolved.Token)
	require.Equal(t, gh.SourceEnvGH, resolved.Source)
}

func TestResolveTokenFallsBackToKeyring(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	ring := newFakeRing()
	ring.items[gh.TokenKey] = []byte("keyring-token")

	resolved, err := gh.ResolveToken(ring)
	require.NoError(t, err)
	require.Equal(t, "keyring-token", resolved.Token)
	require.Equal(t, gh.SourceKeyring, resolved.Source)
}

func TestResolveTokenReportsMissing(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	resolved, err := gh.ResolveToken(newFakeRing())
	require.NoError(t, err)
	require.Empty(t, resolved.Token)
	require.Empty(t, resolved.Source)
}

func TestResolveTokenReportsKeyringErrors(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	ring := newFakeRing()
	ring.get = errors.New("keyring boom")

	_, err := gh.ResolveToken(ring)
	require.ErrorContains(t, err, "keyring")
}

func TestStoreAndDeleteToken(t *testing.T) {
	ring := newFakeRing()

	require.NoError(t, gh.StoreToken(ring, "  secret  "))
	resolved, err := gh.ResolveToken(ring)
	require.NoError(t, err)
	require.Equal(t, "secret", resolved.Token)

	require.NoError(t, gh.DeleteToken(ring))
	resolved, err = gh.ResolveToken(ring)
	require.NoError(t, err)
	require.Empty(t, resolved.Token)
}

func TestStoreTokenRejectsEmpty(t *testing.T) {
	require.ErrorContains(t, gh.StoreToken(newFakeRing(), "   "), "empty")
}

func TestImportGHToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	old := gh.GHTokenFunc
	t.Cleanup(func() { gh.GHTokenFunc = old })
	gh.GHTokenFunc = func(_ context.Context) (string, error) { return "gh-token", nil }

	ring := newFakeRing()
	resolved, err := gh.ImportGHToken(t.Context(), ring)
	require.NoError(t, err)
	require.Equal(t, "gh-token", resolved.Token)
	require.Equal(t, gh.SourceKeyring, resolved.Source)

	stored, err := gh.ResolveToken(ring)
	require.NoError(t, err)
	require.Equal(t, "gh-token", stored.Token)
}

func TestImportGHTokenReportsGHFailure(t *testing.T) {
	old := gh.GHTokenFunc
	t.Cleanup(func() { gh.GHTokenFunc = old })
	gh.GHTokenFunc = func(_ context.Context) (string, error) { return "", errors.New("gh not logged in") }

	_, err := gh.ImportGHToken(t.Context(), newFakeRing())
	require.ErrorContains(t, err, "gh")
}
