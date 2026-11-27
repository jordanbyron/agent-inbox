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

var (
	working = rex.StatusRecord{State: "working", App: "claude-code"}
	idle    = rex.StatusRecord{State: "idle", App: "claude-code"}
	blocked = rex.StatusRecord{State: "blocked", Kind: "permission", App: "claude-code", Msg: "approve Bash: ls"}
)

const (
	needsYou = string(triage.SectionNeedsYou)
	active   = string(triage.SectionActive)
	settled  = string(triage.SectionSettled)
	snoozed  = string(triage.SectionSnoozed)
	pinned   = string(triage.SectionPinned)
)

// launched is a launched agent's record, begun an hour before t0, with no
// worktree.
func launched(session, dir string) state.Record {
	return state.Record{Harness: "claude", Name: "fix-login", Dir: dir, RexSession: session, Began: t0.Add(-time.Hour)}
}

// inWorktree is launched for an agent working in its own worktree at dir.
func inWorktree(session, dir string) state.Record {
	r := launched(session, dir)
	r.Worktree, r.Branch = dir, "fix-login"
	return r
}

func hasStatus(status triage.Status) func(snapshot.Row) bool {
	return func(r snapshot.Row) bool { return r.Status == status }
}

func headline(state state.PRState) func(snapshot.Row) bool {
	return func(r snapshot.Row) bool { pr, ok := r.Headline(); return ok && pr.State == state }
}

// The spec's "one agent's life", steps 3 to 6.
func TestOneAgentsLife(t *testing.T) {
	h := newHarness(t)
	repo := newRepo(t, "fix-login")
	work := h.rex.AddSession("work")
	editor, _ := h.tab(work, "")
	tab, block := h.tab(work, "agent-inbox:a1")
	app := h.rex.AddApp("Rex", work)
	h.edit(func(st *state.State) { st.Records["a1"] = inWorktree(work, repo) })
	h.start()

	row, section := h.waitRow("a1", hasStatus(triage.StatusStarting))
	require.Equal(t, active, section)
	require.Equal(t, "fix-login", row.Label)
	require.True(t, row.Launched)
	require.Equal(t, repo, row.Dir)
	require.Equal(t, []string{work, tab, block}, []string{row.RexSession, row.RexWindow, row.RexBlock})

	// 3. Claude reports working, then blocked on a permission prompt.
	h.status(work, block, working)
	h.waitRow("a1", hasStatus(triage.StatusWorking))
	h.clock.Advance(time.Minute)
	h.status(work, block, blocked)
	row = h.waitSection("a1", needsYou)
	require.Equal(t, triage.StatusBlocked, row.Status)
	require.Equal(t, snapshot.KindPermission, row.Kind)
	require.Equal(t, "approve Bash: ls", row.Detail)
	require.True(t, row.Unread)
	require.Equal(t, triage.StatusBlocked, h.state().Entries["a1"].LastState)

	// 4. You show the tab: the agent is seen and moves to Active.
	h.clock.Advance(time.Minute)
	require.NoError(t, h.rex.Display(app, work, tab))
	row = h.waitSection("a1", active)
	require.True(t, row.Read)
	entry := h.state().Entries["a1"]
	require.Equal(t, h.clock.Now(), entry.AcknowledgedAt.UTC())
	require.Equal(t, h.clock.Now(), entry.LastVisitedAt.UTC())

	// 5. The agent opens a pull request from its branch and ends its turn.
	h.gh.set(912, ghPR{state: "OPEN"}, "fix-login")
	h.status(work, block, working)
	h.waitRow("a1", hasStatus(triage.StatusWorking))
	h.status(work, block, idle)
	row, _ = h.waitRow("a1", headline(state.PRStateOpen))
	require.Equal(t, 912, row.PullRequests[0].Number)
	require.Equal(t, []state.PullRequest{{
		URL: prURL(912), Number: 912, Title: "PR 912", State: state.PRStateOpen,
		Health: triage.HealthClean, Source: state.SourceBranch,
	}}, h.state().PullRequests["a1"])

	// 6. It merges while you look elsewhere: the tab closes and it settles.
	require.NoError(t, h.rex.Display(app, work, editor))
	h.gh.set(912, ghPR{state: "MERGED", closedAt: t0.Add(2 * time.Minute)}, "fix-login")
	h.clock.Advance(pulls.RefreshAfter)
	row = h.waitSection("a1", settled)
	require.Empty(t, row.RexBlock)
	require.Equal(t, []string{tab}, h.rex.ClosedWindows())
	require.Equal(t, h.clock.Now(), h.state().Entries["a1"].SettledAt.UTC())
	h.waitLog("closed a tab")
	require.Contains(t, h.log.String(), "#912 merged")
}

// Rex.app attaches to every session, so the active window of a session it
// isn't displaying must not count as seen.
func TestSeenIsTheTabAnAppDisplays(t *testing.T) {
	h := newHarness(t)
	work := h.rex.AddSession("work")
	tab, block := h.tab(work, "agent-inbox:a1")
	other := h.rex.AddSession("other")
	elsewhere, _ := h.tab(other, "")
	app := h.rex.AddApp("Rex", work, other)
	require.NoError(t, h.rex.SetStatus(work, block, blocked))
	h.edit(func(st *state.State) { st.Records["a1"] = launched(work, "/r/app") })
	h.start()

	row := h.waitSection("a1", needsYou)
	require.False(t, row.Read, "with nothing displayed yet, nothing is seen, though the tab is its session's active one")

	require.NoError(t, h.rex.Display(app, other, elsewhere))
	again := blocked
	again.Msg = "approve Bash: pwd"
	h.status(work, block, again)
	_, section := h.waitRow("a1", func(r snapshot.Row) bool { return r.Detail == again.Msg })
	require.Equal(t, needsYou, section, "the app shows another session")

	require.NoError(t, h.rex.Show(t.Context(), work, block))
	row = h.waitSection("a1", active)
	require.True(t, row.Read)
	require.Equal(t, tab, row.RexWindow)

	require.NoError(t, h.rex.Display(app, other, elsewhere))
	h.clock.Advance(time.Minute)
	h.status(work, block, working)
	h.status(work, block, blocked)
	h.waitSection("a1", needsYou)
}

func TestFoundAgentComesAndGoes(t *testing.T) {
	h := newHarness(t)
	work := h.rex.AddSession("work")
	_, shell := h.tab(work, "")
	h.start()
	require.Empty(t, h.waitSnapshot(func(snapshot.Snapshot) bool { return true }).Rows(), "a plain shell is no agent")

	h.status(work, shell, rex.StatusRecord{State: "working", App: "claude-code", Title: "Tidy the README"})
	row, section := h.waitRow(shell, hasStatus(triage.StatusWorking))
	require.Equal(t, active, section)
	require.False(t, row.Launched)
	require.Equal(t, "claude", row.Harness)
	require.Equal(t, "Tidy the README", row.Label)
	require.Contains(t, h.state().Entries, shell)

	h.entry(shell, func(e *triage.Entry) { e.Alias = "readme" })
	h.waitRow(shell, func(r snapshot.Row) bool { return r.Label == "readme" })

	require.NoError(t, h.rex.ExitBlock(work, shell, 0))
	h.waitSnapshot(func(s snapshot.Snapshot) bool { return len(s.Rows()) == 0 })
	require.NotContains(t, h.state().Entries, shell, "a found agent's entry goes with its block")
}

func TestBuildToolIsNoAgent(t *testing.T) {
	h := newHarness(t)
	work := h.rex.AddSession("work")
	_, build := h.tab(work, "")
	_, agent := h.tab(work, "")
	h.start()

	h.status(work, build, rex.StatusRecord{State: "working", App: "cargo"})
	h.status(work, agent, idle)
	h.waitRow(agent, hasStatus(triage.StatusIdle))
	require.Len(t, h.waitSnapshot(func(snapshot.Snapshot) bool { return true }).Rows(), 1)
}

func TestSnoozeWakesByTimer(t *testing.T) {
	h := newHarness(t)
	work := h.rex.AddSession("work")
	_, block := h.tab(work, "agent-inbox:a1")
	h.edit(func(st *state.State) { st.Records["a1"] = launched(work, "/r/app") })
	h.start()
	h.status(work, block, idle)
	h.waitSection("a1", active)

	h.entry("a1", func(e *triage.Entry) { e.Snooze(t0.Add(10 * time.Minute)) })
	h.waitSection("a1", snoozed)

	h.clock.Advance(10 * time.Minute)
	h.waitSection("a1", active)
	require.True(t, h.state().Entries["a1"].WakeAt.IsZero(), "the watcher records the wake")
}

func TestSnoozeWakesWhenBlocked(t *testing.T) {
	h := newHarness(t)
	work := h.rex.AddSession("work")
	_, block := h.tab(work, "agent-inbox:a1")
	h.edit(func(st *state.State) { st.Records["a1"] = launched(work, "/r/app") })
	h.start()
	h.status(work, block, working)
	h.waitRow("a1", hasStatus(triage.StatusWorking))

	h.entry("a1", func(e *triage.Entry) { e.SnoozeUntilWoken() })
	h.waitSection("a1", snoozed)

	h.status(work, block, blocked)
	h.waitSection("a1", needsYou)
	require.False(t, h.state().Entries["a1"].UntilWoken)
}

func TestVerbEditReachesSnapshot(t *testing.T) {
	h := newHarness(t)
	work := h.rex.AddSession("work")
	_, block := h.tab(work, "agent-inbox:a1")
	h.edit(func(st *state.State) { st.Records["a1"] = launched(work, "/r/app") })
	h.start()
	h.status(work, block, idle)
	h.waitSection("a1", active)

	h.entry("a1", func(e *triage.Entry) { e.TogglePin(t0) })
	row := h.waitSection("a1", pinned)
	require.Equal(t, t0, row.PinnedAt.UTC())

	h.entry("a1", func(e *triage.Entry) { e.Alias = "login" })
	h.waitRow("a1", func(r snapshot.Row) bool { return r.Label == "login" && r.Alias == "login" })
}

func TestFamilyWithBlockedChild(t *testing.T) {
	h := newHarness(t)
	work := h.rex.AddSession("work")
	_, rootBlock := h.tab(work, "agent-inbox:a1")
	_, childBlock := h.tab(work, "agent-inbox:a2")
	h.edit(func(st *state.State) {
		st.Records["a1"] = launched(work, "/r/app")
		child := launched(work, "/r/app-child")
		child.Name, child.Parent, child.Began = "write-tests", "a1", t0.Add(-30*time.Minute)
		st.Records["a2"] = child
	})
	h.start()

	h.status(work, rootBlock, working)
	h.status(work, childBlock, blocked)
	h.waitSnapshot(func(s snapshot.Snapshot) bool {
		rows := section(s, needsYou)
		return len(rows) == 2 && rows[0].Key == "a1" && rows[1].Key == "a2" && rows[0].Status == triage.StatusWorking
	})

	require.NoError(t, h.rex.ExitBlock(work, childBlock, 0))
	child := h.waitSection("a2", active)
	require.True(t, child.Closed)
	require.Equal(t, "a1", child.Parent)
	require.Equal(t, "a1", section(h.waitSnapshot(func(snapshot.Snapshot) bool { return true }), active)[0].Key)
	require.False(t, h.state().Entries["a2"].SettledAt.IsZero(), "a closed child settles")

	require.NoError(t, h.rex.ExitBlock(work, rootBlock, 0))
	h.waitSnapshot(func(s snapshot.Snapshot) bool {
		rows := section(s, settled)
		return len(rows) == 2 && rows[0].Key == "a1" && !rows[1].Closed
	})
}

func section(s snapshot.Snapshot, name string) []snapshot.Row {
	for _, sec := range s.Sections {
		if string(sec.Name) == name {
			return sec.Rows
		}
	}
	return nil
}
