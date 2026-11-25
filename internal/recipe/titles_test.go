package recipe_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/recipe"
)

const titledSession = "4b7c533a-1f64-4467-9f3c-316f4f695756"

func writeTranscript(t *testing.T, home string, lines ...string) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", "-Users-dev-code-app")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := strings.Join(lines, "\n") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, titledSession+".jsonl"), []byte(body), 0o600))
}

func TestClaudeTitles(t *testing.T) {
	user := `{"type":"user","message":{"content":"{\"type\":\"ai-title\",\"aiTitle\":\"not a title\"}"}}`
	cases := []struct {
		name   string
		lines  []string
		want   string
		titled bool
	}{
		{
			name: "the latest ai-title",
			lines: []string{
				`{"type":"ai-title","aiTitle":"Clone the repo","sessionId":"x"}`,
				user,
				`{"type":"ai-title","aiTitle":"Fix the login redirect","sessionId":"x"}`,
				user,
			},
			want:   "Fix the login redirect",
			titled: true,
		},
		{
			name: "a rename after the ai-title",
			lines: []string{
				`{"type":"ai-title","aiTitle":"Clone the repo","sessionId":"x"}`,
				`{"type":"custom-title","customTitle":"billing retries","sessionId":"x"}`,
			},
			want:   "billing retries",
			titled: true,
		},
		{name: "no title yet", lines: []string{user}},
		{name: "a last line without a newline", lines: []string{`{"type":"ai-title","aiTitle":"Half"`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			writeTranscript(t, home, c.lines...)
			title, titled, err := recipe.ClaudeTitles{}.FindTitle(t.Context(), recipe.TitleQuery{Home: home, SessionID: titledSession})
			require.NoError(t, err)
			require.Equal(t, c.titled, titled)
			require.Equal(t, c.want, title)
		})
	}

	t.Run("no transcript", func(t *testing.T) {
		_, titled, err := recipe.ClaudeTitles{}.FindTitle(t.Context(), recipe.TitleQuery{Home: t.TempDir(), SessionID: titledSession})
		require.NoError(t, err)
		require.False(t, titled)
	})
}
