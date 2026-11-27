package recipe_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/recipe"
)

// agyLog is a run's log: a fixture under testdata/agy, made from agy
// 1.5.14's own logs, with the workspace filled in.
type agyLog struct {
	fixture   string
	began     time.Time
	workspace string
}

func TestAgySessions(t *testing.T) {
	began := time.Date(2026, 11, 23, 21, 47, 15, 500_000_000, time.Local)
	dir := t.TempDir()
	other := t.TempDir()
	cases := []struct {
		name string
		logs []agyLog
		want string
	}{
		{name: "no logs", want: ""},
		{
			name: "the run in the agent's directory",
			logs: []agyLog{{"interactive.log", began, dir}},
			want: "56fc796c-36fc-4ce8-a6ea-d2b478877c19",
		},
		{
			name: "one in print mode",
			logs: []agyLog{{"print.log", began.Add(time.Second), dir}},
			want: "8137cad9-9705-4bcd-b85c-d09597117b43",
		},
		{
			name: "the newest of several",
			logs: []agyLog{{"interactive.log", began, dir}, {"print.log", began.Add(time.Minute), dir}},
			want: "8137cad9-9705-4bcd-b85c-d09597117b43",
		},
		{
			name: "not a run elsewhere",
			logs: []agyLog{{"interactive.log", began, dir}, {"print.log", began.Add(time.Minute), other}},
			want: "56fc796c-36fc-4ce8-a6ea-d2b478877c19",
		},
		{
			name: "not one that created no conversation",
			logs: []agyLog{{"interactive.log", began, dir}, {"no_conversation.log", began.Add(time.Minute), dir}},
			want: "56fc796c-36fc-4ce8-a6ea-d2b478877c19",
		},
		{
			name: "not one from before the agent began",
			logs: []agyLog{{"interactive.log", began.Add(-time.Second), dir}},
			want: "",
		},
		{
			name: "the directory compared with symlinks resolved",
			logs: []agyLog{{"interactive.log", began, resolved(t, dir)}},
			want: "56fc796c-36fc-4ce8-a6ea-d2b478877c19",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			for _, l := range c.logs {
				writeAgyLog(t, home, l)
			}

			id, ok, err := recipe.AgySessions{}.FindSessionID(t.Context(), recipe.SessionQuery{Home: home, Dir: dir, Began: began})

			require.NoError(t, err)
			require.Equal(t, c.want != "", ok)
			require.Equal(t, c.want, id)
		})
	}
}

func TestAgySessionsIgnoresOtherFiles(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	logs := filepath.Join(home, ".gemini", "antigravity-cli", "log")
	require.NoError(t, os.MkdirAll(logs, 0o700))
	for _, name := range []string{"acp-server-20261123_214715-67570.log", "cli-latest.log"} {
		require.NoError(t, os.WriteFile(filepath.Join(logs, name), []byte("workspaceDirs=["+dir+"] \n"), 0o600))
	}

	_, ok, err := recipe.AgySessions{}.FindSessionID(t.Context(), recipe.SessionQuery{Home: home, Dir: dir, Began: time.Time{}})

	require.NoError(t, err)
	require.False(t, ok)
}

func resolved(t *testing.T, dir string) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	return path
}

func writeAgyLog(t *testing.T, home string, l agyLog) {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("testdata", "agy", l.fixture))
	require.NoError(t, err)
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "log")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	name := l.began.Format("cli-20060102_150405.log")
	body := strings.ReplaceAll(string(text), "{dir}", l.workspace)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
}
