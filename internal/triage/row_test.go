package triage_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/triage"
)

var (
	now   = time.Unix(1_789_600_000, 0)
	began = time.Unix(1_789_400_000, 0)
)

var (
	merged        = triage.PullRequest{URL: "https://github.com/acme/app/pull/1", Known: true, ResolvedAt: now.Add(-time.Minute)}
	open          = triage.PullRequest{URL: "https://github.com/acme/app/pull/2", Known: true}
	mergedUndated = triage.PullRequest{URL: "https://github.com/acme/app/pull/3", Known: true}
	unknown       = triage.PullRequest{URL: "https://github.com/acme/app/pull/4"}
	old           = triage.PullRequest{URL: "https://github.com/acme/app/pull/5", Known: true, ResolvedAt: began.Add(-24 * time.Hour)}
)

func withHealth(health triage.Health) triage.PullRequest {
	return triage.PullRequest{URL: "https://github.com/acme/app/pull/7", Known: true, Health: health}
}

func TestSection(t *testing.T) {
	cases := []struct {
		name  string
		agent triage.Agent
		entry triage.Entry
		at    time.Time
		want  triage.Section
	}{
		{
			name:  "an agent with no entry yet is Active",
			agent: triage.Agent{Status: triage.StatusWorking, HasBlock: true},
			want:  triage.SectionActive,
		},
		{
			name:  "a blocked agent needs you",
			agent: triage.Agent{Status: triage.StatusBlocked, HasBlock: true},
			want:  triage.SectionNeedsYou,
		},
		{
			name:  "an agent in error needs you",
			agent: triage.Agent{Status: triage.StatusError, HasBlock: true},
			want:  triage.SectionNeedsYou,
		},
		{
			name:  "an agent in error for days with nothing else needs you",
			agent: triage.Agent{Status: triage.StatusError, HasBlock: true},
			entry: triage.Entry{LastState: triage.StatusError, StateSince: now.Add(-72 * time.Hour)},
			want:  triage.SectionNeedsYou,
		},
		{
			name:  "a freshly finished agent is Active",
			agent: triage.Agent{Status: triage.StatusDone, HasBlock: true},
			entry: triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(-time.Minute)},
			want:  triage.SectionActive,
		},
		{
			name:  "a finished agent quiet for a day is still Active",
			agent: triage.Agent{Status: triage.StatusDone, HasBlock: true},
			entry: triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(-24 * time.Hour)},
			want:  triage.SectionActive,
		},
		{
			name:  "a finished agent with no block is Settled",
			agent: triage.Agent{Status: triage.StatusDone},
			want:  triage.SectionSettled,
		},
		{
			name:  "a working agent with no block is Settled",
			agent: triage.Agent{Status: triage.StatusWorking},
			want:  triage.SectionSettled,
		},
		{
			name:  "a blocked agent with no block is Settled, not Needs you",
			agent: triage.Agent{Status: triage.StatusBlocked},
			entry: triage.Entry{LastState: triage.StatusBlocked, StateSince: now.Add(-time.Minute)},
			want:  triage.SectionSettled,
		},
		{
			name:  "a pinned working agent is Pinned",
			agent: triage.Agent{Status: triage.StatusWorking, HasBlock: true},
			entry: triage.Entry{PinnedAt: now},
			want:  triage.SectionPinned,
		},
		{
			name:  "a pin outranks Needs you",
			agent: triage.Agent{Status: triage.StatusBlocked, HasBlock: true},
			entry: triage.Entry{PinnedAt: now, LastState: triage.StatusBlocked},
			want:  triage.SectionPinned,
		},
		{
			name:  "a pin outranks Settled",
			agent: triage.Agent{Status: triage.StatusDone},
			entry: triage.Entry{PinnedAt: now, SettledAt: now, LastState: triage.StatusDone},
			want:  triage.SectionPinned,
		},
		{
			name:  "a snoozed agent is Snoozed",
			agent: triage.Agent{Status: triage.StatusWorking, HasBlock: true},
			entry: triage.Entry{WakeAt: now.Add(15 * time.Minute), LastState: triage.StatusWorking},
			want:  triage.SectionSnoozed,
		},
		{
			name:  "a snooze outranks a pin",
			agent: triage.Agent{Status: triage.StatusWorking, HasBlock: true},
			entry: triage.Entry{PinnedAt: now, WakeAt: now.Add(15 * time.Minute)},
			want:  triage.SectionSnoozed,
		},
		{
			name:  "a pin is back once the snooze runs out",
			agent: triage.Agent{Status: triage.StatusWorking, HasBlock: true},
			entry: triage.Entry{PinnedAt: now, WakeAt: now.Add(15 * time.Minute)},
			at:    now.Add(15*time.Minute + time.Second),
			want:  triage.SectionPinned,
		},
		{
			name:  "a snooze whose time has passed is Active",
			agent: triage.Agent{Status: triage.StatusWorking, HasBlock: true},
			entry: triage.Entry{WakeAt: now.Add(-time.Second), LastState: triage.StatusWorking},
			want:  triage.SectionActive,
		},
		{
			name:  "a snooze until woken holds for a month",
			agent: triage.Agent{Status: triage.StatusWorking, HasBlock: true},
			entry: triage.Entry{UntilWoken: true, LastState: triage.StatusWorking},
			at:    now.Add(30 * 24 * time.Hour),
			want:  triage.SectionSnoozed,
		},
		{
			name:  "a snooze outranks a resolved pull request",
			agent: triage.Agent{Status: triage.StatusDone, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{merged}},
			entry: triage.Entry{WakeAt: now.Add(15 * time.Minute), LastState: triage.StatusDone, StateSince: now.Add(-time.Hour)},
			want:  triage.SectionSnoozed,
		},
		{
			name:  "an acknowledged blocked agent is Active",
			agent: triage.Agent{Status: triage.StatusBlocked, HasBlock: true},
			entry: triage.Entry{AcknowledgedAt: now, LastState: triage.StatusBlocked, StateSince: now.Add(-time.Minute)},
			want:  triage.SectionActive,
		},
		{
			name:  "a blocked agent with an open pull request needs you",
			agent: triage.Agent{Status: triage.StatusBlocked, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{open}},
			entry: triage.Entry{LastState: triage.StatusBlocked, StateSince: now.Add(-24 * time.Hour)},
			want:  triage.SectionNeedsYou,
		},
		{
			name:  "a blocked agent whose only pull request resolved before it began needs you",
			agent: triage.Agent{Status: triage.StatusBlocked, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{old}},
			want:  triage.SectionNeedsYou,
		},
		{
			name:  "a blocked agent whose pull request gh has not dated needs you",
			agent: triage.Agent{Status: triage.StatusBlocked, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{mergedUndated}},
			want:  triage.SectionNeedsYou,
		},
		{
			name:  "an agent in error with a merged pull request needs you",
			agent: triage.Agent{Status: triage.StatusError, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{merged}},
			want:  triage.SectionNeedsYou,
		},
		{
			name:  "a restored blocked agent with a merged pull request needs you",
			agent: triage.Agent{Status: triage.StatusBlocked, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{merged}},
			entry: triage.Entry{LastState: triage.StatusBlocked, StateSince: now.Add(-time.Minute), RevivedAt: now, RevivedPRs: []string{merged.URL}},
			want:  triage.SectionNeedsYou,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := tc.at
			if at.IsZero() {
				at = now
			}
			row := triage.Row{Agent: tc.agent, Entry: tc.entry}
			require.Equal(t, tc.want, row.Section(at, false))
		})
	}
}

func TestSectionPullRequestRule(t *testing.T) {
	finished := triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(-time.Minute)}
	cases := []struct {
		name           string
		agent          triage.Agent
		entry          triage.Entry
		wantPRNeedsYou bool
		want           triage.Section
	}{
		{
			name:           "failing checks on a finished agent's pull request need you",
			agent:          triage.Agent{Status: triage.StatusDone, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{withHealth(triage.HealthFailing)}},
			entry:          finished,
			wantPRNeedsYou: true,
			want:           triage.SectionNeedsYou,
		},
		{
			name:           "changes requested on an idle agent's pull request need you",
			agent:          triage.Agent{Status: triage.StatusIdle, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{withHealth(triage.HealthChangesRequested)}},
			entry:          finished,
			wantPRNeedsYou: true,
			want:           triage.SectionNeedsYou,
		},
		{
			name:  "a conflict is Active",
			agent: triage.Agent{Status: triage.StatusDone, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{withHealth(triage.HealthConflicting)}},
			entry: finished,
			want:  triage.SectionActive,
		},
		{
			name:  "a clean pull request is Active",
			agent: triage.Agent{Status: triage.StatusDone, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{withHealth(triage.HealthClean)}},
			entry: finished,
			want:  triage.SectionActive,
		},
		{
			name:  "a pull request gh has not described is Active",
			agent: triage.Agent{Status: triage.StatusDone, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{withHealth(triage.HealthUnknown)}},
			entry: finished,
			want:  triage.SectionActive,
		},
		{
			name:  "failing checks wait while the agent is still working",
			agent: triage.Agent{Status: triage.StatusWorking, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{withHealth(triage.HealthFailing)}},
			entry: finished,
			want:  triage.SectionActive,
		},
		{
			name:           "failing checks are Active once the tab has been seen",
			agent:          triage.Agent{Status: triage.StatusDone, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{withHealth(triage.HealthFailing)}},
			entry:          triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(-time.Minute), AcknowledgedAt: now.Add(-30 * time.Second)},
			wantPRNeedsYou: true,
			want:           triage.SectionActive,
		},
		{
			name:           "failing checks on an agent with no block are Settled",
			agent:          triage.Agent{Status: triage.StatusDone, Began: began, PullRequests: []triage.PullRequest{withHealth(triage.HealthFailing)}},
			entry:          triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(-time.Minute), SettledAt: now.Add(-30 * time.Second)},
			wantPRNeedsYou: true,
			want:           triage.SectionSettled,
		},
		{
			name: "the troubled pull request is the one that counts",
			agent: triage.Agent{Status: triage.StatusDone, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{
				withHealth(triage.HealthClean), withHealth(triage.HealthFailing),
			}},
			entry:          finished,
			wantPRNeedsYou: true,
			want:           triage.SectionNeedsYou,
		},
		{
			name:           "failing checks need you before any entry is recorded",
			agent:          triage.Agent{Status: triage.StatusDone, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{withHealth(triage.HealthFailing)}},
			wantPRNeedsYou: true,
			want:           triage.SectionNeedsYou,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := triage.Row{Agent: tc.agent, Entry: tc.entry}
			require.Equal(t, tc.wantPRNeedsYou, row.PRNeedsYou())
			require.Equal(t, tc.want, row.Section(now, false))
		})
	}
}

func TestSectionWorkingFold(t *testing.T) {
	working := triage.Entry{LastState: triage.StatusWorking, StateSince: now.Add(-time.Minute)}
	cases := []struct {
		name        string
		agent       triage.Agent
		entry       triage.Entry
		foldWorking bool
		want        triage.Section
	}{
		{
			name:  "a working agent is Active without the fold",
			agent: triage.Agent{Status: triage.StatusWorking, HasBlock: true},
			entry: working,
			want:  triage.SectionActive,
		},
		{
			name:        "a working agent is Working with the fold",
			agent:       triage.Agent{Status: triage.StatusWorking, HasBlock: true},
			entry:       working,
			foldWorking: true,
			want:        triage.SectionWorking,
		},
		{
			name:        "a finished agent comes out of the fold",
			agent:       triage.Agent{Status: triage.StatusDone, HasBlock: true},
			entry:       working,
			foldWorking: true,
			want:        triage.SectionActive,
		},
		{
			name:        "an agent waiting on work it started stays out of the fold",
			agent:       triage.Agent{Status: triage.StatusWorking, HasBlock: true, WaitingOnWork: true},
			entry:       working,
			foldWorking: true,
			want:        triage.SectionActive,
		},
		{
			name:        "a pinned working agent stays Pinned",
			agent:       triage.Agent{Status: triage.StatusWorking, HasBlock: true},
			entry:       triage.Entry{PinnedAt: now},
			foldWorking: true,
			want:        triage.SectionPinned,
		},
		{
			name:        "a snoozed working agent stays Snoozed",
			agent:       triage.Agent{Status: triage.StatusWorking, HasBlock: true},
			entry:       triage.Entry{WakeAt: now.Add(15 * time.Minute)},
			foldWorking: true,
			want:        triage.SectionSnoozed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := triage.Row{Agent: tc.agent, Entry: tc.entry}
			require.Equal(t, tc.want, row.Section(now, tc.foldWorking))
		})
	}
}

func TestUnread(t *testing.T) {
	finished := triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(-time.Minute)}
	seenSince := finished
	seenSince.LastVisitedAt = now.Add(-30 * time.Second)
	seenBefore := finished
	seenBefore.LastVisitedAt = now.Add(-90 * time.Second)

	cases := []struct {
		name       string
		agent      triage.Agent
		entry      triage.Entry
		wantUnread bool
		wantRead   bool
	}{
		{"a done agent nobody has seen is unread", triage.Agent{Status: triage.StatusDone, HasBlock: true}, finished, true, false},
		{"an idle agent nobody has seen is unread", triage.Agent{Status: triage.StatusIdle, HasBlock: true}, finished, true, false},
		{"a blocked agent nobody has seen is unread", triage.Agent{Status: triage.StatusBlocked, HasBlock: true}, finished, true, false},
		{"a done agent with no entry yet is unread", triage.Agent{Status: triage.StatusDone, HasBlock: true}, triage.Entry{}, true, false},
		{"a done agent seen since it finished is read", triage.Agent{Status: triage.StatusDone, HasBlock: true}, seenSince, false, true},
		{"a blocked agent seen since is read", triage.Agent{Status: triage.StatusBlocked, HasBlock: true}, seenSince, false, true},
		{"a done agent last seen before it finished is unread", triage.Agent{Status: triage.StatusDone, HasBlock: true}, seenBefore, true, false},
		{"a working agent carries no mark", triage.Agent{Status: triage.StatusWorking, HasBlock: true}, triage.Entry{}, false, false},
		{"an agent that has not reported yet carries no mark", triage.Agent{Status: triage.StatusStarting, HasBlock: true}, triage.Entry{}, false, false},
		{"an agent with no block carries no mark", triage.Agent{Status: triage.StatusDone}, finished, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := triage.Row{Agent: tc.agent, Entry: tc.entry}
			require.Equal(t, tc.wantUnread, row.Unread())
			require.Equal(t, tc.wantRead, row.Read())
		})
	}
}

func TestShouldAutoSettle(t *testing.T) {
	recent := triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(-5 * time.Second)}
	agent := func(status triage.Status, prs ...triage.PullRequest) triage.Agent {
		return triage.Agent{Status: status, HasBlock: true, Began: began, PullRequests: prs}
	}
	resolvedLater := triage.PullRequest{URL: "https://github.com/acme/app/pull/9", Known: true, ResolvedAt: now}

	cases := []struct {
		name  string
		agent triage.Agent
		entry triage.Entry
		shown bool
		want  bool
	}{
		{name: "a finished agent whose pull request merged", agent: agent(triage.StatusDone, merged), entry: recent, want: true},
		{name: "an idle agent whose pull request merged", agent: agent(triage.StatusIdle, merged), entry: recent, want: true},
		{name: "a blocked agent whose pull request merged", agent: agent(triage.StatusBlocked, merged), entry: recent, want: true},
		{
			name:  "a blocked agent whose pull request merged after its tab was seen",
			agent: agent(triage.StatusBlocked, merged),
			entry: triage.Entry{LastState: triage.StatusBlocked, StateSince: now.Add(-30 * time.Second), AcknowledgedAt: now.Add(-10 * time.Second)},
			want:  true,
		},
		{name: "its own pull request settles it past an old one it mentioned", agent: agent(triage.StatusDone, old, merged), entry: recent, want: true},
		{name: "not a finished agent with no pull request", agent: agent(triage.StatusDone), entry: recent},
		{name: "not while its pull request is open", agent: agent(triage.StatusDone, open), entry: recent},
		{name: "not until every pull request has resolved", agent: agent(triage.StatusDone, merged, open), entry: recent},
		{name: "not while gh has not dated a merge, alongside an open one", agent: agent(triage.StatusDone, mergedUndated, open), entry: recent},
		{name: "not on a pull request whose state is unknown", agent: agent(triage.StatusDone, unknown), entry: recent},
		{name: "not while gh has not said when it resolved", agent: agent(triage.StatusBlocked, mergedUndated), entry: recent},
		{name: "not on a pull request resolved before the agent began", agent: agent(triage.StatusBlocked, old), entry: recent},
		{name: "not while it is working", agent: agent(triage.StatusWorking, merged), entry: recent},
		{name: "not when it is in error", agent: agent(triage.StatusError, merged), entry: recent},
		{name: "not before it has reported a status", agent: agent(triage.StatusStarting, merged), entry: recent},
		{
			name:  "not when nothing says when it began",
			agent: triage.Agent{Status: triage.StatusDone, HasBlock: true, PullRequests: []triage.PullRequest{merged}},
			entry: recent,
		},
		{name: "not while pinned", agent: agent(triage.StatusDone, merged), entry: triage.Entry{PinnedAt: now}},
		{name: "not while snoozed", agent: agent(triage.StatusDone, merged), entry: triage.Entry{WakeAt: now.Add(15 * time.Minute)}},
		{name: "not while snoozed until woken", agent: agent(triage.StatusDone, merged), entry: triage.Entry{UntilWoken: true}},
		{name: "once a snooze has run out", agent: agent(triage.StatusDone, merged), entry: triage.Entry{WakeAt: now.Add(-time.Second)}, want: true},
		{name: "not while its tab is shown", agent: agent(triage.StatusDone, merged), entry: recent, shown: true},
		{name: "not when it has no block to close", agent: triage.Agent{Status: triage.StatusDone, Began: began, PullRequests: []triage.PullRequest{merged}}},
		{
			name:  "not after a restore while the same pull requests have resolved",
			agent: agent(triage.StatusDone, merged),
			entry: triage.Entry{RevivedAt: now, RevivedPRs: []string{merged.URL}},
		},
		{
			name:  "after a restore once another pull request resolves",
			agent: agent(triage.StatusDone, merged, resolvedLater),
			entry: triage.Entry{RevivedAt: now, RevivedPRs: []string{merged.URL}},
			want:  true,
		},
		{
			name:  "after a restore with nothing resolved once its pull request resolves",
			agent: agent(triage.StatusDone, resolvedLater),
			entry: triage.Entry{RevivedAt: now},
			want:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := triage.Row{Agent: tc.agent, Entry: tc.entry}
			require.Equal(t, tc.want, row.ShouldAutoSettle(now, tc.shown))
		})
	}
}

func TestARestoreHoldsUntilANewPullRequestResolves(t *testing.T) {
	second := triage.PullRequest{URL: "https://github.com/acme/app/pull/9", Known: true}
	agent := triage.Agent{Status: triage.StatusDone, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{merged, second}}
	var entry triage.Entry
	entry.Settle(now)
	entry.Wake(agent, now.Add(time.Minute))

	agent.Status = triage.StatusWorking
	entry.Observe(agent, now.Add(2*time.Minute))
	agent.Status = triage.StatusDone
	entry.Observe(agent, now.Add(3*time.Minute))
	require.True(t, triage.Row{Agent: agent, Entry: entry}.Revived())

	agent.PullRequests[1].ResolvedAt = now.Add(4 * time.Minute)
	entry.Observe(agent, now.Add(5*time.Minute))
	row := triage.Row{Agent: agent, Entry: entry}
	require.False(t, row.Revived())
	require.True(t, row.ShouldAutoSettle(now.Add(5*time.Minute), false))
}
