package triage_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/triage"
)

func TestObserve(t *testing.T) {
	cases := []struct {
		name   string
		before triage.Entry
		agent  triage.Agent
		at     time.Time
		want   triage.Entry
	}{
		{
			name:  "a first sighting records the state and when it began",
			agent: triage.Agent{Status: triage.StatusWorking},
			at:    now,
			want:  triage.Entry{LastState: triage.StatusWorking, StateSince: now},
		},
		{
			name:   "the same state leaves state since alone",
			before: triage.Entry{LastState: triage.StatusWorking, StateSince: now},
			agent:  triage.Agent{Status: triage.StatusWorking},
			at:     now.Add(100 * time.Second),
			want:   triage.Entry{LastState: triage.StatusWorking, StateSince: now},
		},
		{
			name:   "a new state moves state since",
			before: triage.Entry{LastState: triage.StatusWorking, StateSince: now},
			agent:  triage.Agent{Status: triage.StatusDone},
			at:     now.Add(200 * time.Second),
			want:   triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(200 * time.Second)},
		},
		{
			name:   "an elapsed snooze ends",
			before: triage.Entry{WakeAt: now.Add(-time.Second), LastState: triage.StatusWorking, StateSince: now.Add(-time.Hour)},
			agent:  triage.Agent{Status: triage.StatusWorking},
			at:     now,
			want:   triage.Entry{LastState: triage.StatusWorking, StateSince: now.Add(-time.Hour)},
		},
		{
			name:   "a snooze that has not run out holds",
			before: triage.Entry{WakeAt: now.Add(15 * time.Minute), LastState: triage.StatusWorking, StateSince: now.Add(-time.Hour)},
			agent:  triage.Agent{Status: triage.StatusWorking},
			at:     now.Add(time.Minute),
			want:   triage.Entry{WakeAt: now.Add(15 * time.Minute), LastState: triage.StatusWorking, StateSince: now.Add(-time.Hour)},
		},
		{
			name:   "a snooze until woken holds for a month",
			before: triage.Entry{UntilWoken: true, LastState: triage.StatusWorking, StateSince: now},
			agent:  triage.Agent{Status: triage.StatusWorking},
			at:     now.Add(30 * 24 * time.Hour),
			want:   triage.Entry{UntilWoken: true, LastState: triage.StatusWorking, StateSince: now},
		},
		{
			name:   "becoming blocked ends a snooze",
			before: triage.Entry{WakeAt: now.Add(15 * time.Minute), LastState: triage.StatusWorking, StateSince: now.Add(-400 * time.Second)},
			agent:  triage.Agent{Status: triage.StatusBlocked},
			at:     now,
			want:   triage.Entry{LastState: triage.StatusBlocked, StateSince: now},
		},
		{
			name:   "failing ends a snooze until woken",
			before: triage.Entry{UntilWoken: true, LastState: triage.StatusWorking, StateSince: now.Add(-400 * time.Second)},
			agent:  triage.Agent{Status: triage.StatusError},
			at:     now,
			want:   triage.Entry{LastState: triage.StatusError, StateSince: now},
		},
		{
			name:   "an agent already blocked when snoozed stays snoozed",
			before: triage.Entry{WakeAt: now.Add(15 * time.Minute), LastState: triage.StatusBlocked, StateSince: now.Add(-10 * time.Minute)},
			agent:  triage.Agent{Status: triage.StatusBlocked},
			at:     now.Add(time.Minute),
			want:   triage.Entry{WakeAt: now.Add(15 * time.Minute), LastState: triage.StatusBlocked, StateSince: now.Add(-10 * time.Minute)},
		},
		{
			name:   "trouble on a blocked agent's pull request keeps its snooze",
			before: triage.Entry{WakeAt: now.Add(24 * time.Hour), LastState: triage.StatusBlocked, StateSince: now.Add(-10 * time.Minute)},
			agent:  triage.Agent{Status: triage.StatusBlocked, Began: began, PullRequests: []triage.PullRequest{withHealth(triage.HealthConflicting)}},
			at:     now,
			want:   triage.Entry{WakeAt: now.Add(24 * time.Hour), LastState: triage.StatusBlocked, StateSince: now, PRHealth: triage.HealthConflicting},
		},
		{
			name:   "an agent snoozed before it was first observed stays snoozed",
			before: triage.Entry{WakeAt: now.Add(time.Hour)},
			agent:  triage.Agent{Status: triage.StatusBlocked},
			at:     now.Add(5 * time.Second),
			want:   triage.Entry{WakeAt: now.Add(time.Hour), LastState: triage.StatusBlocked, StateSince: now.Add(5 * time.Second)},
		},
		{
			name:   "the same state keeps an acknowledge",
			before: triage.Entry{AcknowledgedAt: now, LastVisitedAt: now, LastState: triage.StatusBlocked, StateSince: now.Add(-time.Minute)},
			agent:  triage.Agent{Status: triage.StatusBlocked},
			at:     now.Add(30 * time.Second),
			want:   triage.Entry{AcknowledgedAt: now, LastVisitedAt: now, LastState: triage.StatusBlocked, StateSince: now.Add(-time.Minute)},
		},
		{
			name:   "a new state lifts an acknowledge and keeps the visit",
			before: triage.Entry{AcknowledgedAt: now, LastVisitedAt: now, LastState: triage.StatusBlocked, StateSince: now.Add(-time.Minute)},
			agent:  triage.Agent{Status: triage.StatusWorking},
			at:     now.Add(30 * time.Second),
			want:   triage.Entry{LastVisitedAt: now, LastState: triage.StatusWorking, StateSince: now.Add(30 * time.Second)},
		},
		{
			name:   "a new state keeps a restore",
			before: triage.Entry{RevivedAt: now, RevivedPRs: []string{merged.URL}, LastState: triage.StatusBlocked, StateSince: now.Add(-time.Minute)},
			agent:  triage.Agent{Status: triage.StatusWorking},
			at:     now.Add(30 * time.Second),
			want:   triage.Entry{RevivedAt: now, RevivedPRs: []string{merged.URL}, LastState: triage.StatusWorking, StateSince: now.Add(30 * time.Second)},
		},
		{
			name:   "a new state keeps a settle",
			before: triage.Entry{SettledAt: now, LastState: triage.StatusWorking, StateSince: now.Add(-time.Minute)},
			agent:  triage.Agent{Status: triage.StatusBlocked},
			at:     now.Add(30 * time.Second),
			want:   triage.Entry{SettledAt: now, LastState: triage.StatusBlocked, StateSince: now.Add(30 * time.Second)},
		},
		{
			name:   "a clean pull request changes nothing",
			before: triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(-10 * time.Minute), AcknowledgedAt: now.Add(-5 * time.Minute)},
			agent:  triage.Agent{Status: triage.StatusDone, Began: began, PullRequests: []triage.PullRequest{withHealth(triage.HealthClean)}},
			at:     now,
			want:   triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(-10 * time.Minute), AcknowledgedAt: now.Add(-5 * time.Minute)},
		},
		{
			name:   "a pull request gh has not described leaves the recorded trouble alone",
			before: triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(-10 * time.Minute), PRHealth: triage.HealthFailing, AcknowledgedAt: now.Add(-5 * time.Minute)},
			agent:  triage.Agent{Status: triage.StatusDone, Began: began, PullRequests: []triage.PullRequest{unknown}},
			at:     now,
			want:   triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(-10 * time.Minute), PRHealth: triage.HealthFailing, AcknowledgedAt: now.Add(-5 * time.Minute)},
		},
		{
			name:  "a first sighting with no pull request records no trouble",
			agent: triage.Agent{Status: triage.StatusWorking},
			at:    now,
			want:  triage.Entry{LastState: triage.StatusWorking, StateSince: now},
		},
		{
			name:   "times are kept to the second",
			before: triage.Entry{LastState: triage.StatusWorking, StateSince: now},
			agent:  triage.Agent{Status: triage.StatusDone},
			at:     now.Add(1500 * time.Millisecond),
			want:   triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(time.Second)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := tc.before
			entry.Observe(tc.agent, tc.at)
			require.Equal(t, tc.want, entry)
		})
	}
}

func TestObservePullRequestTroubleIsAStateChange(t *testing.T) {
	done := func(health triage.Health) triage.Agent {
		return triage.Agent{Status: triage.StatusDone, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{withHealth(health)}}
	}
	entry := triage.Entry{LastState: triage.StatusDone, StateSince: now.Add(-10 * time.Minute), AcknowledgedAt: now.Add(-5 * time.Minute)}

	entry.Observe(done(triage.HealthFailing), now.Add(10*time.Second))
	require.Equal(t, triage.HealthFailing, entry.PRHealth)
	require.Equal(t, now.Add(10*time.Second), entry.StateSince)
	require.True(t, entry.AcknowledgedAt.IsZero())

	entry.Observe(done(triage.HealthFailing), now.Add(20*time.Second))
	require.Equal(t, now.Add(10*time.Second), entry.StateSince)

	entry.Observe(done(triage.HealthClean), now.Add(30*time.Second))
	require.Equal(t, triage.HealthUnknown, entry.PRHealth)
	require.Equal(t, now.Add(30*time.Second), entry.StateSince)
}

func TestSeenPullRequestTroubleHoldsUntilItChanges(t *testing.T) {
	done := func(health triage.Health) triage.Row {
		return triage.Row{Agent: triage.Agent{Status: triage.StatusDone, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{withHealth(health)}}}
	}
	var entry triage.Entry
	observe := func(row triage.Row, at time.Time) triage.Section {
		entry.Observe(row.Agent, at)
		row.Entry = entry
		return row.Section(at, false)
	}

	observe(done(triage.HealthUnknown), now)
	require.Equal(t, triage.SectionNeedsYou, observe(done(triage.HealthFailing), now.Add(10*time.Second)))
	entry.Acknowledge(now.Add(20 * time.Second))
	require.Equal(t, triage.SectionActive, observe(done(triage.HealthFailing), now.Add(30*time.Second)))
	observe(done(triage.HealthClean), now.Add(40*time.Second))
	require.Equal(t, triage.SectionNeedsYou, observe(done(triage.HealthChangesRequested), now.Add(50*time.Second)))
}

func TestSeenBlockedAgentNeedsYouAgainAfterItsStateChanges(t *testing.T) {
	agent := triage.Agent{Status: triage.StatusBlocked, HasBlock: true}
	entry := triage.Entry{LastState: triage.StatusBlocked, StateSince: now.Add(-time.Minute)}
	entry.Acknowledge(now)
	require.Equal(t, triage.SectionActive, triage.Row{Agent: agent, Entry: entry}.Section(now, false))

	agent.Status = triage.StatusWorking
	entry.Observe(agent, now.Add(30*time.Second))
	agent.Status = triage.StatusBlocked
	entry.Observe(agent, now.Add(time.Minute))

	require.True(t, entry.AcknowledgedAt.IsZero())
	require.Equal(t, triage.SectionNeedsYou, triage.Row{Agent: agent, Entry: entry}.Section(now.Add(time.Minute), false))
}

func TestSnooze(t *testing.T) {
	var entry triage.Entry

	entry.Snooze(now.Add(time.Hour))
	require.Equal(t, triage.Entry{WakeAt: now.Add(time.Hour)}, entry)
	require.True(t, entry.Snoozed(now))

	entry.SnoozeUntilWoken()
	require.Equal(t, triage.Entry{UntilWoken: true}, entry)
	require.True(t, entry.Snoozed(now.Add(365*24*time.Hour)))

	entry.Snooze(now.Add(time.Hour))
	require.Equal(t, triage.Entry{WakeAt: now.Add(time.Hour)}, entry)
}

func TestWakeEndsASnoozeAndASettleAndRemembersWhatHadResolved(t *testing.T) {
	agent := triage.Agent{Status: triage.StatusDone, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{merged, open, old}}
	var entry triage.Entry
	entry.Snooze(now.Add(time.Hour))
	entry.Settle(now)

	entry.Wake(agent, now.Add(time.Second))

	require.Equal(t, triage.Entry{RevivedAt: now.Add(time.Second), RevivedPRs: []string{merged.URL, old.URL}}, entry)
}

func TestSettleEndsASnoozeAndAWake(t *testing.T) {
	agent := triage.Agent{Status: triage.StatusDone, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{merged}}
	var entry triage.Entry
	entry.Snooze(now.Add(time.Hour))
	entry.Wake(agent, now)

	entry.Settle(now.Add(time.Second))

	require.Equal(t, triage.Entry{SettledAt: now.Add(time.Second)}, entry)
	require.True(t, triage.Row{Agent: agent, Entry: entry}.ShouldAutoSettle(now.Add(time.Second), false))
}

func TestAcknowledgeStampsBothClocks(t *testing.T) {
	var entry triage.Entry

	entry.Acknowledge(now)

	require.Equal(t, triage.Entry{AcknowledgedAt: now, LastVisitedAt: now}, entry)
}

func TestTogglePin(t *testing.T) {
	var entry triage.Entry

	entry.TogglePin(now)
	require.Equal(t, triage.Entry{PinnedAt: now}, entry)
	require.True(t, entry.Pinned())

	entry.TogglePin(now.Add(5 * time.Second))
	require.Equal(t, triage.Entry{}, entry)
	require.False(t, entry.Pinned())
}

func TestAPinHoldsThroughASnoozeAndAWake(t *testing.T) {
	row := triage.Row{Agent: triage.Agent{Status: triage.StatusWorking, HasBlock: true}}
	row.Entry.TogglePin(now)

	row.Entry.Snooze(now.Add(time.Hour))
	require.Equal(t, triage.SectionSnoozed, row.Section(now, false))

	row.Entry.Wake(row.Agent, now)
	require.Equal(t, triage.SectionPinned, row.Section(now, false))
}

func TestEntryJSON(t *testing.T) {
	entry := triage.Entry{
		Alias:          "flaky",
		LinkedPR:       "https://github.com/acme/app/pull/7",
		WakeAt:         now.Add(time.Hour),
		LastState:      triage.StatusBlocked,
		StateSince:     now,
		PinnedAt:       now.Add(1 * time.Second),
		SettledAt:      now.Add(2 * time.Second),
		AcknowledgedAt: now.Add(3 * time.Second),
		LastVisitedAt:  now.Add(4 * time.Second),
		RevivedAt:      now.Add(5 * time.Second),
		RevivedPRs:     []string{merged.URL},
		PRHealth:       triage.HealthFailing,
	}

	data, err := json.Marshal(entry)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"alias": "flaky",
		"pr": "https://github.com/acme/app/pull/7",
		"wake_at": 1789603600,
		"last_state": "blocked",
		"state_since": 1789600000,
		"pinned_at": 1789600001,
		"settled_at": 1789600002,
		"acknowledged_at": 1789600003,
		"last_visited_at": 1789600004,
		"revived_at": 1789600005,
		"revived_prs": ["https://github.com/acme/app/pull/1"],
		"pr_health": "FAILING"
	}`, string(data))

	var back triage.Entry
	require.NoError(t, json.Unmarshal(data, &back))
	require.Equal(t, entry, back)
}

func TestEntryJSONLeavesOutWhatIsUnset(t *testing.T) {
	data, err := json.Marshal(triage.Entry{UntilWoken: true})
	require.NoError(t, err)
	require.JSONEq(t, `{"until_woken": true}`, string(data))

	data, err = json.Marshal(triage.Entry{})
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(data))
}

func TestEntryJSONRejectsAMalformedEntry(t *testing.T) {
	var entry triage.Entry

	err := json.Unmarshal([]byte(`{"wake_at": "soon"}`), &entry)

	require.ErrorContains(t, err, "reading triage entry")
}
