package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/cli"
)

func TestAutoSubmitCommand(t *testing.T) {
	command, _, err := cli.New("test").Find([]string{"auto-submit"})
	require.NoError(t, err)
	require.Equal(t, "auto-submit", command.Name())
	_, err = runCLI(t, cleanRepo(t), "auto-submit", "unexpected")
	require.ErrorContains(t, err, "unknown command")
}
