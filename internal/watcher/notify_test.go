package watcher_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/notify"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
	"github.com/jordanbyron/agent-inbox/internal/watcher"
)

func TestNotificationTransitions(t *testing.T) {
	ask := notify.Notification{Title: "fix-login", Body: "approve Bash: ls", Group: "a1"}
	done := notify.Notification{Title: "fix-login", Body: "finished", Group: "a1"}
	blocked := watcher.Moment{UnseenAsk: ask, WhenDone: done, RootStatus: triage.StatusBlocked}
	atWork := watcher.Moment{WhenDone: done, RootStatus: triage.StatusWorking, AnyWorking: true}
	idle := watcher.Moment{WhenDone: done, RootStatus: triage.StatusIdle}
	with := func(m watcher.Moment, change func(*watcher.Moment)) watcher.Moment {
		change(&m)
		return m
	}
	cases := []struct {
		name          string
		before, after watcher.Moment
		new           bool
		want          []notify.Notification
	}{
		{name: "starts needing you", before: atWork, after: blocked, want: []notify.Notification{ask}},
		{name: "still needs you", before: blocked, after: blocked},
		{name: "a new family already blocked", after: blocked, new: true, want: []notify.Notification{ask}},
		{name: "finishes", before: atWork, after: idle, want: []notify.Notification{done}},
		{
			name: "finishes done", before: atWork,
			after: with(idle, func(m *watcher.Moment) { m.RootStatus = triage.StatusDone }),
			want:  []notify.Notification{done},
		},
		{name: "a new family idle has not finished", after: idle, new: true},
		{name: "idle from idle", before: idle, after: idle},
		{
			name: "finishes while you look at it", before: atWork,
			after: with(idle, func(m *watcher.Moment) { m.AnySeen = true }),
		},
		{
			name: "the root stops while a child works", before: atWork,
			after: with(idle, func(m *watcher.Moment) { m.AnyWorking = true }),
		},
		{
			name: "blocked while snoozed", before: atWork,
			after: with(blocked, func(m *watcher.Moment) { m.SnoozedOrSettled = true }),
		},
		{
			name: "finishes while snoozed", before: atWork,
			after: with(idle, func(m *watcher.Moment) { m.SnoozedOrSettled = true }),
		},
		{
			name:   "a snooze runs out on a family already blocked",
			before: with(blocked, func(m *watcher.Moment) { m.SnoozedOrSettled = true }),
			after:  blocked,
		},
		{name: "seen and so no longer needing you", before: blocked, after: idle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := map[string]watcher.Moment{"a1": c.before}
			if c.new {
				before = map[string]watcher.Moment{}
			}

			got := watcher.Notifications(before, map[string]watcher.Moment{"a1": c.after})

			require.Equal(t, c.want, got)
		})
	}
}

func familyWithNotifyOn(t *testing.T) (h *harness, work, rootTab, rootBlock, childBlock string) {
	t.Helper()
	h = newHarness(t)
	h.cfg.Notify = true
	h.notifier = newFakeNotifier()
	work = h.rex.AddSession("work")
	rootTab, rootBlock = h.tab(work, "agent-inbox:a1")
	_, childBlock = h.tab(work, "agent-inbox:a2")
	h.edit(func(st *state.State) {
		st.Records["a1"] = launched(work, "/r/app")
		child := launched(work, "/r/app-child")
		child.Name, child.Parent = "write-tests", "a1"
		st.Records["a2"] = child
	})
	return h, work, rootTab, rootBlock, childBlock
}

func (h *harness) waitNotification() notify.Notification {
	h.t.Helper()
	select {
	case n := <-h.notifier.sent:
		return n
	case <-time.After(waitFor):
		h.t.Fatalf("no notification came; log:\n%s", h.log)
		return notify.Notification{}
	}
}

func (h *harness) requireNoNotification() {
	h.t.Helper()
	select {
	case n := <-h.notifier.sent:
		h.t.Fatalf("an unexpected notification: %+v", n)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestNotifiesWhenAFamilyStartsNeedingYou(t *testing.T) {
	h, work, _, rootBlock, _ := familyWithNotifyOn(t)
	h.start()
	h.status(work, rootBlock, working)
	h.waitRow("a1", hasStatus(triage.StatusWorking))

	h.status(work, rootBlock, blocked)

	require.Equal(t, notify.Notification{Title: "fix-login", Body: "approve Bash: ls", Group: "a1"}, h.waitNotification())
	h.waitLog("sent a notification")
	h.clock.Advance(time.Second)
	h.waitSection("a1", needsYou)
	h.requireNoNotification()
}

func TestNotifiesForABlockedChildByItsName(t *testing.T) {
	h, work, _, rootBlock, childBlock := familyWithNotifyOn(t)
	h.start()
	h.status(work, rootBlock, working)
	h.status(work, childBlock, working)
	h.waitRow("a2", hasStatus(triage.StatusWorking))

	h.status(work, childBlock, rex.StatusRecord{State: "blocked", Kind: "question", App: "claude-code"})

	require.Equal(t, notify.Notification{Title: "write-tests", Body: "question", Group: "a1"}, h.waitNotification())
}

func TestNotifiesWhenAFamilyFinishes(t *testing.T) {
	h, work, _, rootBlock, childBlock := familyWithNotifyOn(t)
	h.start()
	h.status(work, childBlock, idle)
	h.status(work, rootBlock, working)
	h.waitRow("a1", hasStatus(triage.StatusWorking))

	h.status(work, rootBlock, rex.StatusRecord{State: "done", App: "claude-code", Msg: "Opened #42."})

	require.Equal(t, notify.Notification{Title: "fix-login", Body: "Opened #42.", Group: "a1"}, h.waitNotification())
}

func TestNoNotificationForWhatHeldAtStartup(t *testing.T) {
	h, work, _, rootBlock, _ := familyWithNotifyOn(t)
	h.status(work, rootBlock, blocked)
	h.start()
	h.waitSection("a1", needsYou)

	h.clock.Advance(time.Second)
	h.waitLog("watching")
	h.requireNoNotification()
}

func TestNoNotificationWhileYouLook(t *testing.T) {
	h, work, rootTab, rootBlock, _ := familyWithNotifyOn(t)
	h.rex.AddApp("Rex", work)
	require.NoError(t, h.rex.SetActiveWindow(work, rootTab))
	h.start()
	h.status(work, rootBlock, working)
	h.waitRow("a1", hasStatus(triage.StatusWorking))

	h.status(work, rootBlock, blocked)
	h.waitRow("a1", hasStatus(triage.StatusBlocked))
	h.status(work, rootBlock, idle)
	h.waitRow("a1", hasStatus(triage.StatusIdle))

	h.requireNoNotification()
}

func TestNoNotificationForASnoozedFamily(t *testing.T) {
	h, work, _, rootBlock, _ := familyWithNotifyOn(t)
	h.start()
	h.status(work, rootBlock, working)
	h.waitRow("a1", hasStatus(triage.StatusWorking))
	h.entry("a1", func(e *triage.Entry) { e.Snooze(t0.Add(time.Hour)) })
	h.waitSection("a1", snoozed)

	h.status(work, rootBlock, idle)
	h.waitRow("a1", hasStatus(triage.StatusIdle))

	h.requireNoNotification()
}
