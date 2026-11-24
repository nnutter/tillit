package daemonlog_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/tillit/internal/daemonlog"
)

func TestLogRotatesOversizedWritesAndKeepsNewestBytes(t *testing.T) {
	const size = 5 * 1024 * 1024
	gitDir := t.TempDir()
	log, err := daemonlog.Open(gitDir)
	require.NoError(t, err)
	data := make([]byte, 0, 4*size+123)
	for _, b := range []byte{'a', 'b', 'c', 'd'} {
		data = append(data, bytes.Repeat([]byte{b}, size)...)
	}
	data = append(data, bytes.Repeat([]byte{'e'}, 123)...)
	n, err := log.Write(data)
	require.NoError(t, err)
	require.Equal(t, len(data), n)
	require.NoError(t, log.Close())

	path := filepath.Join(gitDir, "tillit", "daemon.log")
	for _, tc := range []struct {
		suffix string
		byte   byte
		size   int
	}{{"", 'e', 123}, {".1", 'd', size}, {".2", 'c', size}, {".3", 'b', size}} {
		info, err := os.Stat(path + tc.suffix)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		content, err := os.ReadFile(path + tc.suffix)
		require.NoError(t, err)
		require.Equal(t, bytes.Repeat([]byte{tc.byte}, tc.size), content)
	}
	_, err = os.Stat(path + ".4")
	require.ErrorIs(t, err, os.ErrNotExist)

	log, err = daemonlog.Open(gitDir)
	require.NoError(t, err)
	n, err = log.Write([]byte("restart"))
	require.NoError(t, err)
	require.Equal(t, len("restart"), n)
	require.NoError(t, log.Close())
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, append(bytes.Repeat([]byte{'e'}, 123), []byte("restart")...), content)
}
