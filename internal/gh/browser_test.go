package gh

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenCommand(t *testing.T) {
	name, args := openCommand("darwin", "https://example.com")
	require.Equal(t, "open", name)
	require.Equal(t, []string{"https://example.com"}, args)

	name, args = openCommand("linux", "https://example.com")
	require.Equal(t, "xdg-open", name)
	require.Equal(t, []string{"https://example.com"}, args)

	name, args = openCommand("windows", "https://example.com")
	require.Equal(t, "rundll32", name)
	require.Equal(t, []string{"url.dll,FileProtocolHandler", "https://example.com"}, args)
}
