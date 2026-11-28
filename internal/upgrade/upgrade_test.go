package upgrade_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/upgrade"
)

type asker struct {
	version string
	err     error
	asked   int
}

func (a *asker) ask(string) (string, error) {
	a.asked++
	return a.version, a.err
}

func install(t *testing.T, path, contents string, at time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o755))
	require.NoError(t, os.Chtimes(path, at, at))
}

func TestVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-inbox")
	install(t, path, "v0.2.0", time.Unix(1_000, 0))
	a := &asker{version: "v0.3.0"}
	installed := upgrade.New(path, "v0.2.0", a.ask)

	t.Run("the binary that started is the running version, unasked", func(t *testing.T) {
		version, err := installed.Version()
		require.NoError(t, err)
		require.Equal(t, "v0.2.0", version)
		require.Zero(t, a.asked)
	})

	t.Run("one written over it is asked once", func(t *testing.T) {
		install(t, path, "v0.3.0!", time.Unix(2_000, 0))
		for range 2 {
			version, err := installed.Version()
			require.NoError(t, err)
			require.Equal(t, "v0.3.0", version)
		}
		require.Equal(t, 1, a.asked)
	})

	t.Run("a symlink pointed at another file is asked", func(t *testing.T) {
		cellar := filepath.Join(dir, "v0.4.0")
		install(t, cellar, "v0.3.0!", time.Unix(2_000, 0))
		require.NoError(t, os.Remove(path))
		require.NoError(t, os.Symlink(cellar, path))
		a.version = "v0.4.0"
		version, err := installed.Version()
		require.NoError(t, err)
		require.Equal(t, "v0.4.0", version)
	})

	t.Run("one that won't answer is asked again", func(t *testing.T) {
		install(t, path, "half-written", time.Unix(3_000, 0))
		a.err = errors.New("exec format error")
		_, err := installed.Version()
		require.Error(t, err)
		a.err, a.version = nil, "v0.5.0"
		version, err := installed.Version()
		require.NoError(t, err)
		require.Equal(t, "v0.5.0", version)
	})

	t.Run("a missing binary is an error", func(t *testing.T) {
		require.NoError(t, os.Remove(path))
		_, err := installed.Version()
		require.Error(t, err)
	})
}
