package pulls_test

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

func openNode(n int, health string) string {
	return `{"number": ` + strconv.Itoa(n) + `, "url": "https://github.com/o/r/pull/` + strconv.Itoa(n) + `", "title": "t", "state": "OPEN", "mergeable": "` + health + `"}`
}

func TestDue(t *testing.T) {
	merged := state.PullRequest{URL: "https://github.com/o/r/pull/1", State: state.PRStateMerged, ResolvedAt: now.Add(-time.Hour)}
	mergedNoDate := state.PullRequest{URL: "https://github.com/o/r/pull/2", State: state.PRStateMerged}
	open := state.PullRequest{URL: "https://github.com/o/r/pull/3", State: state.PRStateOpen}
	unknown := state.PullRequest{URL: "https://github.com/o/r/pull/4"}
	prs := []state.PullRequest{merged, mergedNoDate, open, unknown}
	r := pulls.NewRefresher(replying(`{"data": {"repository": {}}}`, nil))

	require.Equal(t, []state.PullRequest{mergedNoDate, open, unknown}, r.Due(prs, now),
		"everything is due but what gh has said resolved, and when")

	_, err := r.Refresh(t.Context(), []state.PullRequest{open}, now)
	require.ErrorIs(t, err, pulls.ErrNotFound)

	require.Equal(t, []state.PullRequest{mergedNoDate, unknown}, r.Due(prs, now.Add(pulls.RefreshAfter-time.Second)),
		"one asked about within the window waits, even when gh could not say")
	require.Equal(t, []state.PullRequest{mergedNoDate, open, unknown}, r.Due(prs, now.Add(pulls.RefreshAfter)))
}

func TestRefreshAsksUntilGHSaysWhenItResolved(t *testing.T) {
	calls := 0
	gh := ghFunc(func(string, []string) ([]byte, error) {
		calls++
		return []byte(`{"data": {"repository": {"pr885": {"number": 885, "url": "https://github.com/o/r/pull/885",
			"state": "MERGED", "closedAt": "2026-11-23T11:59:00Z"}}}}`), nil
	})
	r := pulls.NewRefresher(gh)
	prs := []state.PullRequest{{URL: "https://github.com/o/r/pull/885", State: state.PRStateMerged}}

	prs, err := r.Refresh(t.Context(), prs, now)
	require.NoError(t, err)
	require.Equal(t, now.Add(-time.Minute), prs[0].ResolvedAt.UTC())

	_, err = r.Refresh(t.Context(), prs, now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, calls, "a pull request resolved, and known when, is never asked about again")
}

func TestRefreshPicksUpAChangeOfHealthOnTheNextWindow(t *testing.T) {
	health := "MERGEABLE"
	gh := ghFunc(func(string, []string) ([]byte, error) {
		return []byte(`{"data": {"repository": {"pr1": ` + openNode(1, health) + `}}}`), nil
	})
	r := pulls.NewRefresher(gh)
	prs := []state.PullRequest{{URL: "https://github.com/o/r/pull/1", Source: state.SourceLinked}}

	prs, err := r.Refresh(t.Context(), prs, now)
	require.NoError(t, err)
	require.Equal(t, triage.HealthClean, prs[0].Health)

	health = "CONFLICTING"
	prs, err = r.Refresh(t.Context(), prs, now.Add(pulls.RefreshAfter/2))
	require.NoError(t, err)
	require.Equal(t, triage.HealthClean, prs[0].Health, "inside the window nothing is asked")

	prs, err = r.Refresh(t.Context(), prs, now.Add(pulls.RefreshAfter))
	require.NoError(t, err)
	require.Equal(t, triage.HealthConflicting, prs[0].Health)
	require.Equal(t, state.SourceLinked, prs[0].Source, "a refresh keeps how the pull request was found")
}

func TestRefreshAsksOncePerRepository(t *testing.T) {
	var owners, queries []string
	gh := ghFunc(func(_ string, args []string) ([]byte, error) {
		owners = append(owners, fieldValue(args, "owner")+"/"+fieldValue(args, "name"))
		queries = append(queries, fieldValue(args, "query"))
		if fieldValue(args, "owner") == "a" {
			return []byte(`{"data": {"repository": {"pr1": {"number": 1, "url": "https://github.com/a/x/pull/1", "state": "OPEN"},
				"pr2": {"number": 2, "url": "https://github.com/a/x/pull/2", "state": "CLOSED", "closedAt": "2026-11-23T11:00:00Z"}}}}`), nil
		}
		return []byte(`{"data": {"repository": {"pr3": {"number": 3, "url": "https://github.com/b/y/pull/3", "state": "OPEN", "isDraft": true}}}}`), nil
	})
	prs := []state.PullRequest{
		{URL: "https://github.com/a/x/pull/1", Source: state.SourceBranch},
		{URL: "https://github.com/b/y/pull/3", Source: state.SourceCapture},
		{URL: "https://github.com/a/x/pull/2", Source: state.SourceBranch},
	}

	got, err := pulls.NewRefresher(gh).Refresh(t.Context(), prs, now)

	require.NoError(t, err)
	require.Equal(t, []string{"a/x", "b/y"}, owners)
	require.Contains(t, queries[0], "pr1: pullRequest(number: 1)")
	require.Contains(t, queries[0], "pr2: pullRequest(number: 2)")
	require.Equal(t, []state.PullRequest{
		{URL: "https://github.com/a/x/pull/1", Number: 1, State: state.PRStateOpen, Health: triage.HealthClean, Source: state.SourceBranch},
		{URL: "https://github.com/b/y/pull/3", Number: 3, State: state.PRStateDraft, Health: triage.HealthClean, Source: state.SourceCapture},
		{
			URL: "https://github.com/a/x/pull/2", Number: 2, State: state.PRStateClosed,
			ResolvedAt: time.Date(2026, 11, 23, 11, 0, 0, 0, time.UTC), Source: state.SourceBranch,
		},
	}, got)
	require.Equal(t, state.PRStateUnknown, prs[0].State, "the pull requests passed in are left as they were")
}

func TestRefreshKeepsWhatGHCouldNotDescribe(t *testing.T) {
	unknown := state.PullRequest{URL: "https://github.com/cli/cli/pull/99999999", Number: 99999999, Source: state.SourceCapture}
	prs := []state.PullRequest{
		{URL: "https://github.com/cli/cli/pull/15100", Source: state.SourceBranch},
		{URL: "https://github.com/cli/cli/pull/15145", Source: state.SourceBranch},
		unknown,
	}
	exit := errors.New("exit status 1: gh: could not resolve to a PullRequest with the number of 99999999")

	got, err := pulls.NewRefresher(replying(testdata(t, "numbers.json"), exit)).Refresh(t.Context(), prs, now)

	require.ErrorIs(t, err, exit)
	require.Len(t, got, 3)
	require.Equal(t, triage.HealthChangesRequested, got[0].Health)
	require.Equal(t, "Revoke CLI OAuth tokens on logout", got[0].Title)
	require.Equal(t, state.PRStateMerged, got[1].State)
	require.Equal(t, time.Date(2026, 11, 23, 18, 24, 26, 0, time.UTC), got[1].ResolvedAt)
	require.Equal(t, unknown, got[2])
}

func TestRefreshWhenGHFails(t *testing.T) {
	prs := []state.PullRequest{{URL: "https://github.com/o/r/pull/1", Number: 1, State: state.PRStateOpen, Health: triage.HealthFailing}}
	tests := []struct {
		name    string
		out     string
		err     error
		wantErr error
	}{
		{name: "no auth", err: errors.New("gh auth login")},
		{name: "offline", err: errors.New("dial tcp: lookup api.github.com: no such host")},
		{name: "an answer that is not JSON", out: "garbage"},
		{name: "an answer without the pull request", out: `{"data": {"repository": {"pr1": null}}}`, wantErr: pulls.ErrNotFound},
		{name: "no such repository", out: `{"data": {"repository": null}}`, wantErr: pulls.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pulls.NewRefresher(replying(tt.out, tt.err)).Refresh(t.Context(), prs, now)

			require.Error(t, err)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			}
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}
			require.Equal(t, prs, got)
		})
	}
}
