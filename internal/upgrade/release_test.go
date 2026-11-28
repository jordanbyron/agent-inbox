package upgrade_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/upgrade"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, running string
		newer           bool
	}{
		{"v0.3.0", "v0.2.0", true},
		{"v0.2.1", "v0.2.0", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.10.0", "v0.9.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.2.0", "v0.3.0", false},
		{"", "v0.2.0", false},
		{"v0.3.0", "(devel)", false},
		{"v0.3.0", "v0.2.1-0.20261128000000-abcdef123456", false},
		{"v0.3.0-rc1", "v0.2.0", false},
	}
	for _, tc := range cases {
		t.Run(tc.latest+" over "+tc.running, func(t *testing.T) {
			require.Equal(t, tc.newer, upgrade.Newer(tc.latest, tc.running))
		})
	}
}

func TestGitHubLatest(t *testing.T) {
	t.Run("reads the tag", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"tag_name": "v0.3.0", "name": "v0.3.0"}`))
		}))
		defer server.Close()
		tag, err := upgrade.GitHub{URL: server.URL}.Latest(context.Background())
		require.NoError(t, err)
		require.Equal(t, "v0.3.0", tag)
	})

	t.Run("a refusal is an error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()
		_, err := upgrade.GitHub{URL: server.URL}.Latest(context.Background())
		require.ErrorContains(t, err, "403")
	})
}

func TestHomebrew(t *testing.T) {
	prefix := t.TempDir()
	keg := filepath.Join(prefix, "Cellar", "agent-inbox", "0.2.0", "bin")
	require.NoError(t, os.MkdirAll(keg, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(keg, "agent-inbox"), nil, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(prefix, "bin"), 0o755))
	link := filepath.Join(prefix, "bin", "agent-inbox")
	require.NoError(t, os.Symlink(filepath.Join(keg, "agent-inbox"), link))

	t.Run("a keg's binary names its prefix's brew", func(t *testing.T) {
		brew, ok := upgrade.Homebrew(link)
		require.True(t, ok)
		resolved, err := filepath.EvalSymlinks(prefix)
		require.NoError(t, err)
		require.Equal(t, filepath.Join(resolved, "bin", "brew"), brew)
	})

	t.Run("one from go install has none", func(t *testing.T) {
		gobin := filepath.Join(t.TempDir(), "agent-inbox")
		require.NoError(t, os.WriteFile(gobin, nil, 0o755))
		_, ok := upgrade.Homebrew(gobin)
		require.False(t, ok)
	})
}
