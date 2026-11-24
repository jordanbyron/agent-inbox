package watcher

import (
	"path/filepath"
	"slices"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// rows builds the snapshot's rows from the agents and the state, by
// section. A family's members all go in the family's section, root first.
func rows(agents []agent, st *state.State, now time.Time, foldWorking bool) map[triage.Section][]snapshot.Row {
	out := map[triage.Section][]snapshot.Row{}
	for _, fam := range families(agents) {
		members := fam.members()
		triaged := make([]triage.Row, len(members))
		sections := make([]triage.Section, len(members))
		for i, a := range members {
			entry := st.Entries[a.key]
			triaged[i] = triage.Row{Agent: a.triage(entry, st.PullRequests[a.key]), Entry: entry}
			sections[i] = triaged[i].Section(now, foldWorking)
		}
		section := familySection(sections[0], sections[1:])
		for i, a := range members {
			row := snapshotRow(a, triaged[i], pulls.Effective(st.PullRequests[a.key]))
			row.Closed = i > 0 && !a.live() && fam.root.live()
			out[section] = append(out[section], row)
		}
	}
	return out
}

func snapshotRow(a agent, r triage.Row, prs []state.PullRequest) snapshot.Row {
	row := snapshot.Row{
		Key:           a.key,
		Label:         label(r.Entry.Alias, a.record.Name, a.status.title, a.dir(), a.harness),
		Alias:         r.Entry.Alias,
		Harness:       a.harness,
		Status:        r.Agent.Status,
		Kind:          snapshot.Kind(a.status.kind),
		Detail:        a.status.msg,
		Dir:           a.dir(),
		Unread:        r.Unread(),
		Read:          r.Read(),
		PinnedAt:      r.Entry.PinnedAt,
		WakeAt:        r.Entry.WakeAt,
		UntilWoken:    r.Entry.UntilWoken,
		StateSince:    r.Entry.StateSince,
		Began:         a.record.Began,
		WaitingOnWork: r.Agent.WaitingOnWork,
		PullRequests:  headlineFirst(r.Agent, prs),
		Launched:      a.launched,
		Parent:        a.record.Parent,
	}
	if a.live() {
		row.RexSession, row.RexWindow, row.RexBlock = a.sessionID, a.block.windowID, a.block.id
	}
	return row
}

// label is what a row is called: the alias you gave it, else the name it
// was launched with, else the title it reports, else its directory's name.
// The harness's name is the last resort, so no row is blank.
func label(alias, name, title, dir, harness string) string {
	for _, l := range []string{alias, name, title} {
		if l != "" {
			return l
		}
	}
	if dir != "" {
		return filepath.Base(dir)
	}
	return harness
}

func headlineFirst(a triage.Agent, prs []state.PullRequest) []state.PullRequest {
	head, ok := a.Headline()
	i := slices.IndexFunc(prs, func(pr state.PullRequest) bool { return pr.URL == head.URL })
	if !ok || i <= 0 {
		return prs
	}
	out := append([]state.PullRequest{prs[i]}, prs[:i]...)
	return append(out, prs[i+1:]...)
}
