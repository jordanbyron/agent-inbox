package recipe_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/recipe"
)

func TestCodexSessions(t *testing.T) {
	began := time.Date(2026, 11, 23, 23, 30, 0, 0, time.Local)
	dir := t.TempDir()
	other := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(dir, link))
	cases := []struct {
		name     string
		rollouts []rollout
		dir      string
		want     string
	}{
		{name: "no sessions at all", want: ""},
		{
			name: "the one in the agent's directory",
			rollouts: []rollout{
				{id: "mine", cwd: dir, created: began.Add(2 * time.Second)},
				{id: "elsewhere", cwd: other, created: began.Add(3 * time.Second)},
			},
			want: "mine",
		},
		{
			name: "the newest of several",
			rollouts: []rollout{
				{id: "first", cwd: dir, created: began.Add(time.Second)},
				{id: "second", cwd: dir, created: began.Add(time.Minute)},
			},
			want: "second",
		},
		{
			name:     "not one from before the agent began",
			rollouts: []rollout{{id: "old", cwd: dir, created: began.Add(-time.Second)}},
			want:     "",
		},
		{
			name:     "one created as the agent began",
			rollouts: []rollout{{id: "same", cwd: dir, created: began}},
			want:     "same",
		},
		{
			name:     "in the next day's directory",
			rollouts: []rollout{{id: "late", cwd: dir, created: began.Add(time.Hour)}},
			want:     "late",
		},
		{
			name:     "through a symlinked directory",
			rollouts: []rollout{{id: "linked", cwd: dir, created: began.Add(time.Second)}},
			dir:      link,
			want:     "linked",
		},
		{
			name: "skipping a rollout still being written",
			rollouts: []rollout{
				{id: "good", cwd: dir, created: began.Add(time.Second)},
				{id: "partial", cwd: dir, created: began.Add(time.Minute), partial: true},
			},
			want: "good",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			for _, r := range c.rollouts {
				r.write(t, home)
			}
			agentDir := dir
			if c.dir != "" {
				agentDir = c.dir
			}

			id, ok, err := recipe.CodexSessions{}.FindSessionID(t.Context(), recipe.SessionQuery{Home: home, Dir: agentDir, Began: began})

			require.NoError(t, err)
			require.Equal(t, c.want != "", ok)
			require.Equal(t, c.want, id)
		})
	}
}

type rollout struct {
	id      string
	cwd     string
	created time.Time
	partial bool
}

// write lays the rollout out as Codex does: in a directory for the local
// date, named for the local time, its first line the session's meta.
func (r rollout) write(t *testing.T, home string) {
	t.Helper()
	local := r.created.Local()
	day := filepath.Join(home, ".codex", "sessions", local.Format("2006"), local.Format("01"), local.Format("02"))
	require.NoError(t, os.MkdirAll(day, 0o700))
	name := fmt.Sprintf("rollout-%s-%s.jsonl", local.Format("2006-01-02T15-04-05"), r.id)
	meta := fmt.Sprintf(`{"timestamp":%q,"type":"session_meta","payload":{"id":%q,"timestamp":%q,"cwd":%q,"originator":"codex_cli_rs"}}`,
		r.created.UTC().Format(time.RFC3339Nano), r.id, r.created.UTC().Format(time.RFC3339Nano), r.cwd)
	if r.partial {
		meta = meta[:len(meta)/2]
	}
	content := meta + "\n" + `{"type":"response_item","payload":{}}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(day, name), []byte(content), 0o600))
}
