package watcher

import (
	"context"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/notify"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// Notifier sends a desktop notification. notify.Desktop is the real one.
type Notifier interface {
	Notify(ctx context.Context, n notify.Notification) error
}

// Bounded, and dropped beyond, so a slow notifier never holds up the loop.
const pendingNotifications = 16

type moment struct {
	unseenAsk        notify.Notification
	whenDone         notify.Notification
	rootStatus       triage.Status
	anyWorking       bool
	anySeen          bool
	snoozedOrSettled bool
}

func (m moment) needsYou() bool {
	return m.unseenAsk.Title != ""
}

func moments(agents []agent, st *state.State, now time.Time) map[string]moment {
	out := map[string]moment{}
	for _, fam := range families(agents) {
		root := fam.root
		rootEntry := st.Entries[root.key]
		m := moment{
			whenDone:         notify.Notification{Title: agentLabel(root, rootEntry), Body: root.status.msg, Group: root.key},
			rootStatus:       root.status.status,
			snoozedOrSettled: !root.live() || rootEntry.Snoozed(now),
		}
		if m.whenDone.Body == "" {
			m.whenDone.Body = "finished"
		}
		for _, a := range fam.members() {
			if !a.live() {
				continue
			}
			entry := st.Entries[a.key]
			m.anySeen = m.anySeen || a.seen
			m.anyWorking = m.anyWorking || a.status.status == triage.StatusWorking
			row := triage.Row{Agent: a.triage(entry, st.PullRequests[a.key]), Entry: entry}
			if !m.needsYou() && (row.Agent.NeedsYou() || row.PRNeedsYou()) && !entry.Acknowledged() {
				m.unseenAsk = notify.Notification{Title: agentLabel(a, entry), Body: whatItNeeds(a, row), Group: root.key}
			}
		}
		out[root.key] = m
	}
	return out
}

func agentLabel(a agent, entry triage.Entry) string {
	return label(entry.Alias, a.record.Label(), a.status.title, a.dir(), a.harness)
}

func whatItNeeds(a agent, row triage.Row) string {
	switch pr, hasPR := row.Agent.Headline(); {
	case a.status.msg != "":
		return a.status.msg
	case a.status.kind != "":
		return a.status.kind
	case hasPR && !row.Agent.NeedsYou():
		return prTrouble[pr.Health]
	default:
		return string(row.Agent.Status)
	}
}

var prTrouble = map[triage.Health]string{
	triage.HealthFailing:          "checks failing",
	triage.HealthChangesRequested: "changes requested",
}

// notifications are what after earns over before. A family that only
// stopped being snoozed is not news: a snooze running out on a family
// already blocked says nothing.
func notifications(before, after map[string]moment) []notify.Notification {
	var out []notify.Notification
	for _, key := range sortedKeys(after) {
		now := after[key]
		was, existed := before[key]
		switch {
		case now.snoozedOrSettled:
		case now.needsYou() && !was.needsYou():
			out = append(out, now.unseenAsk)
		case existed && finished(was, now) && !now.anySeen:
			out = append(out, now.whenDone)
		}
	}
	return out
}

func finished(was, now moment) bool {
	stopped := now.rootStatus == triage.StatusDone || now.rootStatus == triage.StatusIdle
	return was.rootStatus == triage.StatusWorking && stopped && !now.needsYou() && !now.anyWorking
}

// The first observation only remembers, so nothing that already held when
// the watcher started is announced.
func (w *watcher) notifyChanges(now time.Time) {
	after := moments(w.agents, w.st, now)
	if w.moments != nil {
		for _, n := range notifications(w.moments, after) {
			select {
			case w.notes <- n:
			default:
				w.log.Warn("dropped a notification; too many waiting", "title", n.Title)
			}
		}
	}
	w.moments = after
}

func (w *watcher) sendNotifications(ctx context.Context, notifier Notifier) {
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-w.notes:
			if err := notifier.Notify(ctx, n); err != nil {
				w.log.Warn("sending a notification", "title", n.Title, "err", err)
				continue
			}
			w.log.Info("sent a notification", "title", n.Title, "body", n.Body)
		}
	}
}
