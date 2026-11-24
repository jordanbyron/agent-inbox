package verbs_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
	"github.com/jordanbyron/agent-inbox/internal/verbs"
)

func TestParseSnooze(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		text string
		want verbs.Snooze
	}{
		{text: "15m", want: verbs.Snooze{Until: now.Add(15 * time.Minute)}},
		{text: "1h", want: verbs.Snooze{Until: now.Add(time.Hour)}},
		{text: "tomorrow", want: verbs.Snooze{Until: time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)}},
		{text: "fri 5pm", want: verbs.Snooze{Until: time.Date(2026, 11, 27, 17, 0, 0, 0, time.UTC)}},
		{text: "until-woken", want: verbs.Snooze{UntilWoken: true}},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			got, err := verbs.ParseSnooze(c.text, now)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestParseSnoozeSaysWhatItReads(t *testing.T) {
	_, err := verbs.ParseSnooze("later", time.Now())
	require.ErrorContains(t, err, `can't read "later"`)
}

func TestSnooze(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "a1", state.Record{}, true)
	until := began.Add(3 * time.Hour)

	_, err := w.inbox.Snooze(t.Context(), verbs.Target{Key: "a1"}, verbs.Snooze{Until: until})

	require.NoError(t, err)
	require.Equal(t, until, w.entry(t, "a1").WakeAt)
	require.False(t, w.entry(t, "a1").UntilWoken)
}

func TestWakeEndsASnooze(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "a1", state.Record{}, true)
	_, err := w.inbox.Snooze(t.Context(), verbs.Target{Key: "a1"}, verbs.Snooze{UntilWoken: true})
	require.NoError(t, err)

	_, restored, err := w.inbox.Wake(t.Context(), verbs.Target{Key: "a1"})

	require.NoError(t, err)
	require.False(t, restored)
	e := w.entry(t, "a1")
	require.False(t, e.Snoozed(began.Add(time.Hour)))
	require.Equal(t, began.Add(time.Hour), e.RevivedAt)
	require.Empty(t, w.rex.Started())
}

func TestWakeRestoresASettledAgent(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "a1", state.Record{SessionID: "s-1"}, false)

	_, restored, err := w.inbox.Wake(t.Context(), verbs.Target{Key: "a1"})

	require.NoError(t, err)
	require.True(t, restored)
	require.Equal(t, []string{verbs.BlockLabel("a1")}, w.labels(t))
}

func TestTogglePin(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "a1", state.Record{}, true)

	_, pinned, err := w.inbox.TogglePin(t.Context(), verbs.Target{Key: "a1"})
	require.NoError(t, err)
	require.True(t, pinned)
	require.Equal(t, began.Add(time.Hour), w.entry(t, "a1").PinnedAt)

	_, pinned, err = w.inbox.TogglePin(t.Context(), verbs.Target{Key: "a1"})
	require.NoError(t, err)
	require.False(t, pinned)
	require.False(t, w.entry(t, "a1").Pinned())
}

func TestAliasAndClearIt(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "a1", state.Record{Name: "fix-login"}, true)

	a, err := w.inbox.Alias(t.Context(), verbs.Target{Key: "a1"}, "  login  ")
	require.NoError(t, err)
	require.Equal(t, "login", a.Name)

	a, err = w.inbox.Alias(t.Context(), verbs.Target{Key: "a1"}, "")
	require.NoError(t, err)
	require.Equal(t, "fix-login", a.Name)
	require.Empty(t, w.entry(t, "a1").Alias)
}

func TestLink(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "a1", state.Record{}, true)
	found := state.PullRequest{URL: "https://github.com/o/r/pull/1", Number: 1, Source: state.SourceBranch}
	require.NoError(t, w.store.Update(func(st *state.State) error {
		st.PullRequests["a1"] = []state.PullRequest{found}
		return nil
	}))

	_, url, err := w.inbox.Link(t.Context(), verbs.Target{Key: "a1"}, "o/r#7")
	require.NoError(t, err)
	require.Equal(t, "https://github.com/o/r/pull/7", url)
	require.Equal(t, url, w.entry(t, "a1").LinkedPR)
	linked := state.PullRequest{URL: url, Number: 7, Source: state.SourceLinked}
	require.Equal(t, []state.PullRequest{found, linked}, w.state(t).PullRequests["a1"])

	_, url, err = w.inbox.Link(t.Context(), verbs.Target{Key: "a1"}, "https://github.com/o/r/pull/8")
	require.NoError(t, err)
	require.Equal(t, []state.PullRequest{found, {URL: url, Number: 8, Source: state.SourceLinked}}, w.state(t).PullRequests["a1"])

	_, url, err = w.inbox.Link(t.Context(), verbs.Target{Key: "a1"}, "")
	require.NoError(t, err)
	require.Empty(t, url)
	require.Empty(t, w.entry(t, "a1").LinkedPR)
	require.Equal(t, []state.PullRequest{found}, w.state(t).PullRequests["a1"])
}

func TestLinkRefusesWhatIsNotAPullRequest(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "a1", state.Record{}, true)

	_, _, err := w.inbox.Link(t.Context(), verbs.Target{Key: "a1"}, "https://example.com")

	require.ErrorContains(t, err, "not a pull request link")
	require.Empty(t, w.state(t).Entries)
}

func TestSettleClosesEveryMembersTab(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "root", state.Record{}, true)
	w.launch(t, "kid", state.Record{Parent: "root"}, true)
	w.launch(t, "done", state.Record{Parent: "root"}, false)
	w.tab(t, "zsh", "shell")
	_, err := w.inbox.Snooze(t.Context(), verbs.Target{Key: "root"}, verbs.Snooze{UntilWoken: true})
	require.NoError(t, err)

	_, err = w.inbox.Settle(t.Context(), verbs.Target{Key: "kid"})

	require.NoError(t, err)
	require.Equal(t, []string{"shell"}, w.labels(t))
	require.Len(t, w.rex.ClosedWindows(), 2)
	st := w.state(t)
	for _, key := range []string{"root", "kid", "done"} {
		require.Equal(t, began.Add(time.Hour), st.Entries[key].SettledAt, key)
	}
	require.False(t, st.Entries["root"].UntilWoken)
}

func TestSettleLeavesYourOtherPanesInTheWindow(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "a1", state.Record{}, false)
	window, err := w.rex.AddWindow(w.session, "split")
	require.NoError(t, err)
	_, err = w.rex.AddBlock(w.session, window, "shell")
	require.NoError(t, err)
	block, err := w.rex.AddBlock(w.session, window, verbs.BlockLabel("a1"))
	require.NoError(t, err)

	_, err = w.inbox.Settle(t.Context(), verbs.Target{Key: "a1"})

	require.NoError(t, err)
	require.Equal(t, []string{block}, w.rex.ClosedBlocks())
	require.Equal(t, []string{"shell"}, w.labels(t))
}

func TestSettlingAFoundAgentForgetsIt(t *testing.T) {
	w := newWorld(t)
	block := w.find(t)
	_, err := w.inbox.Alias(t.Context(), verbs.Target{Block: block}, "mine")
	require.NoError(t, err)

	a, err := w.inbox.Settle(t.Context(), verbs.Target{Block: block})

	require.NoError(t, err)
	require.Equal(t, "mine", a.Name)
	require.Empty(t, w.labels(t))
	require.Equal(t, map[string]triage.Entry{}, w.state(t).Entries)
}
