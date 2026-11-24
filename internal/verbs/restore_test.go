package verbs_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/recipe"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/rex/rextest"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
	"github.com/jordanbyron/agent-inbox/internal/verbs"
)

func TestRestoreResumesInTheSameLabelAndDirectory(t *testing.T) {
	w := newWorld(t)
	dir := t.TempDir()
	w.launch(t, "a1", state.Record{Name: "fix-login", SessionID: "s-1", Dir: dir}, false)
	merged := state.PullRequest{URL: "https://github.com/o/r/pull/1", State: state.PRStateMerged, ResolvedAt: began}
	require.NoError(t, w.store.Update(func(st *state.State) error {
		st.Entries["a1"] = triage.Entry{SettledAt: began}
		st.PullRequests["a1"] = []state.PullRequest{merged}
		return nil
	}))

	_, err := w.inbox.Restore(t.Context(), verbs.Target{Key: "a1"})

	require.NoError(t, err)
	require.Equal(t, []rex.StartOptions{{
		SessionID:   w.session,
		Command:     []string{"fake", "--resume", "s-1"},
		Dir:         dir,
		BlockLabel:  "agent-inbox:a1",
		WindowLabel: "fix-login",
	}}, w.rex.Started())
	e := w.entry(t, "a1")
	require.True(t, e.SettledAt.IsZero())
	require.Equal(t, []string{merged.URL}, e.RevivedPRs)
}

func TestRestoreLooksTheSessionIDUp(t *testing.T) {
	cases := []struct {
		name  string
		found string
		want  []string
	}{
		{name: "found", found: "s-9", want: []string{"late", "resume", "s-9"}},
		{name: "not found", found: "", want: []string{"late", "resume", "--last"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			dir := t.TempDir()
			w.launch(t, "a1", state.Record{Harness: "late", Dir: dir}, false)
			w.finder.id = c.found

			_, err := w.inbox.Restore(t.Context(), verbs.Target{Key: "a1"})

			require.NoError(t, err)
			require.Equal(t, []recipe.SessionQuery{{Home: "/home/test", Dir: dir, Began: began}}, w.finder.asked)
			require.Equal(t, c.want, w.rex.Started()[0].Command)
			require.Equal(t, c.found, w.state(t).Records["a1"].SessionID)
		})
	}
}

func TestRestoreInANewSessionWhenItsOwnIsGone(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "a1", state.Record{SessionID: "s-1"}, false)
	require.NoError(t, w.rex.RemoveSession(w.session))

	_, err := w.inbox.Restore(t.Context(), verbs.Target{Key: "a1"})

	require.NoError(t, err)
	require.Len(t, w.rex.Started(), 1)
	require.NotEqual(t, w.session, w.state(t).Records["a1"].RexSession)
}

func TestRestoreRefuses(t *testing.T) {
	cases := []struct {
		name   string
		target func(t *testing.T, w *world) verbs.Target
		want   string
	}{
		{
			name: "a missing worktree",
			target: func(t *testing.T, w *world) verbs.Target {
				gone := t.TempDir()
				require.NoError(t, os.Remove(gone))
				w.launch(t, "a1", state.Record{Dir: gone, Worktree: gone, Branch: "b"}, false)
				return verbs.Target{Key: "a1"}
			},
			want: "its worktree",
		},
		{
			name: "a found agent",
			target: func(t *testing.T, w *world) verbs.Target {
				return verbs.Target{Block: w.find(t)}
			},
			want: "nothing to restore it from",
		},
		{
			name: "a running agent",
			target: func(t *testing.T, w *world) verbs.Target {
				w.launch(t, "a1", state.Record{}, true)
				return verbs.Target{Key: "a1"}
			},
			want: "already running",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			target := c.target(t, w)

			_, err := w.inbox.Restore(t.Context(), target)

			require.ErrorContains(t, err, c.want)
			require.Empty(t, w.rex.Started())
		})
	}
}

func TestOpenShowsTheTargetItself(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "root", state.Record{}, true)
	kid := w.launch(t, "kid", state.Record{Parent: "root"}, true)

	_, err := w.inbox.Open(t.Context(), verbs.Target{Key: "kid"})

	require.NoError(t, err)
	require.Equal(t, []string{kid}, w.rex.Shown())
}

func TestOpenRestoresASettledAgentFirst(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "a1", state.Record{SessionID: "s-1"}, false)
	require.NoError(t, w.store.Update(func(st *state.State) error {
		st.Entries["a1"] = triage.Entry{SettledAt: began}
		return nil
	}))

	_, err := w.inbox.Open(t.Context(), verbs.Target{Key: "a1"})

	require.NoError(t, err)
	require.Len(t, w.rex.Started(), 1)
	require.Len(t, w.rex.Shown(), 1)
	require.Equal(t, []string{verbs.BlockLabel("a1")}, w.labels(t))
	require.True(t, w.entry(t, "a1").SettledAt.IsZero())
}

func TestOpenPassesRexsRefusalOnAsIs(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "a1", state.Record{}, true)
	w.rex.AddApp("Rex", w.session)
	w.rex.SetRemoteControl(false)

	_, err := w.inbox.Open(t.Context(), verbs.Target{Key: "a1"})

	require.ErrorIs(t, err, rex.ErrRemoteControlOff)
	require.ErrorContains(t, err, rextest.RemoteControlRefusal)
}

func TestOpenAndCopyAPullRequest(t *testing.T) {
	w := newWorld(t)
	w.system.installed = []string{"wl-copy", "xclip"}
	url := "https://github.com/o/r/pull/1"

	require.NoError(t, w.inbox.OpenPullRequest(t.Context(), url))
	require.NoError(t, w.inbox.CopyPullRequest(t.Context(), url))

	require.Len(t, w.system.ran, 2)
	require.Equal(t, url, w.system.ran[0][1])
	require.Contains(t, []string{"open", "xdg-open"}, w.system.ran[0][0])
	require.Equal(t, []string{"wl-copy"}, w.system.ran[1])
	require.Equal(t, url, w.system.stdin[1])
}

func TestCopyWithNoClipboardProgram(t *testing.T) {
	w := newWorld(t)
	require.ErrorContains(t, w.inbox.CopyPullRequest(t.Context(), "u"), "no clipboard program")
}

func TestListActions(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "a1", state.Record{SessionID: "s-1"}, true)
	actions := w.inbox.ListActions(t.Context())
	later := began.Add(5 * time.Hour)

	require.NoError(t, actions.TogglePin("a1"))
	require.NoError(t, actions.Alias("a1", "login"))
	require.NoError(t, actions.Link("a1", "o/r#3"))
	require.NoError(t, actions.Snooze("a1", later))
	e := w.entry(t, "a1")
	require.True(t, e.Pinned())
	require.Equal(t, "login", e.Alias)
	require.Equal(t, "https://github.com/o/r/pull/3", e.LinkedPR)
	require.Equal(t, later, e.WakeAt)

	require.NoError(t, actions.SnoozeUntilWoken("a1"))
	require.True(t, w.entry(t, "a1").UntilWoken)
	require.NoError(t, actions.Wake("a1"))
	require.False(t, w.entry(t, "a1").UntilWoken)

	require.NoError(t, actions.Open("a1"))
	require.Len(t, w.rex.Shown(), 1)
	require.NoError(t, actions.Settle("a1"))
	require.Empty(t, w.labels(t))
	require.NoError(t, actions.Wake("a1"), "u on a settled row restores it")
	require.Equal(t, []string{verbs.BlockLabel("a1")}, w.labels(t))
	require.NoError(t, actions.Delete("a1"))
	require.Empty(t, w.state(t).Records)

	require.ErrorContains(t, actions.New(), "not wired yet")
	require.ErrorContains(t, actions.Wake("a1"), `no agent "a1"`)
}
