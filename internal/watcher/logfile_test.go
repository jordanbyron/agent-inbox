package watcher_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/watcher"
)

func TestLogRotates(t *testing.T) {
	dir := t.TempDir()
	log, err := watcher.OpenLog(dir)
	require.NoError(t, err)
	line := append(bytes.Repeat([]byte("x"), 1023), '\n')
	for range watcher.MaxLogBytes / len(line) {
		_, err := log.Write(line)
		require.NoError(t, err)
	}
	require.NoError(t, log.Close())
	require.NoFileExists(t, watcher.LogPath(dir)+".1")

	log, err = watcher.OpenLog(dir)
	require.NoError(t, err)
	_, err = log.Write([]byte("one more\n"))
	require.NoError(t, err)
	require.NoError(t, log.Close())

	old, err := os.ReadFile(watcher.LogPath(dir) + ".1")
	require.NoError(t, err)
	require.Len(t, old, watcher.MaxLogBytes)
	current, err := os.ReadFile(watcher.LogPath(dir))
	require.NoError(t, err)
	require.Equal(t, "one more\n", string(current))
}
