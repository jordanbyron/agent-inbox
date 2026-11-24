package watcher_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// mergedAgent starts a watcher on one launched agent, idle in its own tab
// with pull request 912 already found and open, and the tab not shown.
func mergedAgent(t *testing.T) (h *harness, app, work, editor, tab string) {
	h = newHarness(t)
	work = h.rex.AddSession("work")
	editor, _ = h.tab(work, "")
	tab, block := h.tab(work, "agent-inbox:a1")
	app = h.rex.AddApp("Rex", work)
	require.NoError(t, h.rex.SetStatus(work, block, idle))
	h.gh.set(912, ghPR{state: "OPEN"}, "")
	h.edit(func(st *state.State) {
		st.Records["a1"] = launched(work, "/r/app")
		st.PullRequests["a1"] = []state.PullRequest{{URL: prURL(912), Number: 912, Source: state.SourceCapture}}
	})
	h.start()
	h.clock.Advance(time.Second)
	h.waitRow("a1", headline(state.PRStateOpen))
	h.gh.set(912, ghPR{state: "MERGED", closedAt: t0}, "")
	return h, app, work, editor, tab
}

func TestAutoSettleHeldWhileTabShown(t *testing.T) {
	h, app, work, editor, tab := mergedAgent(t)
	require.NoError(t, h.rex.Display(app, work, tab))
	h.waitRow("a1", func(r snapshot.Row) bool { return r.Read })

	h.clock.Advance(pulls.RefreshAfter)
	h.waitRow("a1", headline(state.PRStateMerged))
	require.Empty(t, h.rex.ClosedWindows(), "the tab you are looking at stays open")

	require.NoError(t, h.rex.Display(app, work, editor))
	h.waitSection("a1", settled)
	require.Equal(t, []string{tab}, h.rex.ClosedWindows())
}

func TestAutoSettleHeldWhilePinned(t *testing.T) {
	h, _, _, _, _ := mergedAgent(t)
	h.entry("a1", func(e *triage.Entry) { e.TogglePin(t0) })
	h.waitSection("a1", pinned)

	h.clock.Advance(pulls.RefreshAfter)
	h.waitRow("a1", headline(state.PRStateMerged))
	require.Empty(t, h.rex.ClosedWindows())
	require.True(t, h.state().Entries["a1"].SettledAt.IsZero())

	h.entry("a1", func(e *triage.Entry) { e.TogglePin(t0) })
	h.waitSection("a1", settled)
}

func TestAutoSettleNeverClosesFoundAgent(t *testing.T) {
	h := newHarness(t)
	work := h.rex.AddSession("work")
	editor, _ := h.tab(work, "")
	_, block := h.tab(work, "")
	app := h.rex.AddApp("Rex", work)
	require.NoError(t, h.rex.Display(app, work, editor))
	h.gh.set(77, ghPR{state: "MERGED", closedAt: t0}, "")
	h.rex.SetCapture(block, "Opened "+prURL(77)+"\n")
	require.NoError(t, h.rex.SetStatus(work, block, idle))
	h.start()

	h.waitRow(block, func(r snapshot.Row) bool { return len(r.PullRequests) == 1 })
	h.clock.Advance(time.Second)
	row, section := h.waitRow(block, headline(state.PRStateMerged))
	require.Equal(t, active, section)
	require.Equal(t, state.SourceCapture, row.PullRequests[0].Source)
	require.Empty(t, h.rex.ClosedWindows())
	require.NotContains(t, h.log.String(), "closed a tab")
}

// A blocked root is no reason to hold a tab, but a record at work or in
// error under it is.
func TestAutoSettleHeldWhileAnyRecordWorksOrFails(t *testing.T) {
	cases := []struct {
		name   string
		child  string
		closes bool
	}{
		{name: "a finished sub-agent", child: "done", closes: true},
		{name: "a sub-agent at work", child: "working"},
		{name: "a sub-agent in error", child: "error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			work := h.rex.AddSession("work")
			editor, _ := h.tab(work, "")
			tab, block := h.tab(work, "agent-inbox:a1")
			app := h.rex.AddApp("Rex", work)
			require.NoError(t, h.rex.SetStatus(work, block, blocked))
			require.NoError(t, h.rex.SetStatus(work, block, rex.StatusRecord{ID: "sub", State: c.child}))
			h.gh.set(912, ghPR{state: "OPEN"}, "")
			h.edit(func(st *state.State) {
				st.Records["a1"] = launched(work, "/r/app")
				st.PullRequests["a1"] = []state.PullRequest{{URL: prURL(912), Number: 912, Source: state.SourceCapture}}
			})
			h.start()
			require.NoError(t, h.rex.Display(app, work, editor))
			h.clock.Advance(time.Second)
			h.waitRow("a1", headline(state.PRStateOpen))

			h.gh.set(912, ghPR{state: "MERGED", closedAt: t0}, "")
			h.clock.Advance(pulls.RefreshAfter)
			if c.closes {
				h.waitSection("a1", settled)
				require.Equal(t, []string{tab}, h.rex.ClosedWindows())
				return
			}
			h.waitRow("a1", headline(state.PRStateMerged))
			require.Empty(t, h.rex.ClosedWindows())
		})
	}
}
