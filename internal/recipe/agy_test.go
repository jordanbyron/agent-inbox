package recipe_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/recipe"
)

func TestAgySessions(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("needs the sqlite3 program")
	}
	began := time.Date(2026, 11, 24, 1, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	other := t.TempDir()
	cases := []struct {
		name          string
		conversations []conversation
		want          string
	}{
		{name: "an empty table", want: ""},
		{
			name: "the one in the agent's directory",
			conversations: []conversation{
				{id: "mine", workspaces: []string{dir}, modified: began.Add(time.Second)},
				{id: "elsewhere", workspaces: []string{other}, modified: began.Add(time.Minute)},
			},
			want: "mine",
		},
		{
			name: "the most recently modified",
			conversations: []conversation{
				{id: "first", workspaces: []string{dir}, modified: began.Add(time.Minute)},
				{id: "second", workspaces: []string{other, dir}, modified: began.Add(time.Hour)},
			},
			want: "second",
		},
		{
			name:          "not one last modified before the agent began",
			conversations: []conversation{{id: "old", workspaces: []string{dir}, modified: began.Add(-time.Second)}},
			want:          "",
		},
		{
			name:          "not one with no workspace",
			conversations: []conversation{{id: "none", modified: began.Add(time.Second)}},
			want:          "",
		},
		{
			name: "not a sub-agent's",
			conversations: []conversation{
				{id: "root", workspaces: []string{dir}, modified: began.Add(time.Second)},
				{id: "child", workspaces: []string{dir}, modified: began.Add(time.Minute), parent: "root"},
			},
			want: "root",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			writeAgyDB(t, home, c.conversations)

			id, ok, err := recipe.AgySessions{SQLite: "sqlite3"}.FindSessionID(t.Context(), recipe.SessionQuery{Home: home, Dir: dir, Began: began})

			require.NoError(t, err)
			require.Equal(t, c.want != "", ok)
			require.Equal(t, c.want, id)
		})
	}
}

func TestAgySessionsWithoutSQLiteOrDatabase(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("needs the sqlite3 program")
	}
	dir := t.TempDir()
	home := t.TempDir()
	writeAgyDB(t, home, []conversation{{id: "mine", workspaces: []string{dir}, modified: time.Now()}})
	cases := []struct {
		name   string
		sqlite string
		home   string
	}{
		{name: "no sqlite3", sqlite: filepath.Join(t.TempDir(), "sqlite3"), home: home},
		{name: "no database", sqlite: "sqlite3", home: t.TempDir()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := recipe.SessionQuery{Home: c.home, Dir: dir, Began: time.Now().Add(-time.Hour)}

			id, ok, err := recipe.AgySessions{SQLite: c.sqlite}.FindSessionID(t.Context(), q)

			require.NoError(t, err)
			require.False(t, ok)
			require.Empty(t, id)
		})
	}
}

type conversation struct {
	id         string
	workspaces []string
	modified   time.Time
	parent     string
}

// writeAgyDB makes the database with agy's own schema for the columns read,
// and its values as agy writes them: a JSON array of file:// URIs, or "".
func writeAgyDB(t *testing.T, home string, conversations []conversation) {
	t.Helper()
	dir := filepath.Join(home, ".gemini", "antigravity-cli")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	sql := []string{"CREATE TABLE `conversation_summaries` (`conversation_id` text, `last_modified_time` datetime NOT NULL, " +
		"`workspace_uris` text NOT NULL, `parent_conversation_id` text NOT NULL DEFAULT \"\", PRIMARY KEY (`conversation_id`));"}
	for _, c := range conversations {
		uris := ""
		if len(c.workspaces) > 0 {
			quoted := make([]string, len(c.workspaces))
			for i, w := range c.workspaces {
				quoted[i] = fmt.Sprintf("%q", "file://"+w)
			}
			uris = "[" + strings.Join(quoted, ",") + "]"
		}
		sql = append(sql, fmt.Sprintf("INSERT INTO conversation_summaries VALUES ('%s', '%s', '%s', '%s');",
			c.id, c.modified.UTC().Format("2006-01-02 15:04:05.000000-07:00"), uris, c.parent))
	}
	cmd := exec.CommandContext(t.Context(), "sqlite3", filepath.Join(dir, "conversation_summaries.db"))
	cmd.Stdin = strings.NewReader(strings.Join(sql, "\n"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
