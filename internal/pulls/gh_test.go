package pulls_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

var now = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

type ghFunc func(dir string, args []string) ([]byte, error)

func (f ghFunc) Run(_ context.Context, dir string, args ...string) ([]byte, error) {
	return f(dir, args)
}

func replying(out string, err error) ghFunc {
	return func(string, []string) ([]byte, error) { return []byte(out), err }
}

func testdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(b)
}

func fieldValue(args []string, key string) string {
	for i := 1; i < len(args); i++ {
		if args[i-1] == "-f" || args[i-1] == "-F" {
			if k, v, ok := strings.Cut(args[i], "="); ok && k == key {
				return v
			}
		}
	}
	return ""
}

func TestHealth(t *testing.T) {
	const success = `{"__typename": "CheckRun", "name": "test", "conclusion": "SUCCESS"}`
	failing := `{"__typename": "StatusContext", "context": "ci", "state": "FAILURE"}, ` + success
	run := func(workflow, conclusion, started string) string {
		return `{"__typename": "CheckRun", "name": "PR Checks", "conclusion": "` + conclusion + `", "startedAt": "` + started +
			`", "checkSuite": {"workflowRun": {"workflow": {"name": "` + workflow + `"}}}}`
	}
	merged := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		node   string
		checks string
		want   state.PullRequest
	}{
		{
			name:   "nothing wrong",
			node:   `"state": "OPEN", "reviewDecision": "", "mergeable": "MERGEABLE"`,
			checks: success,
			want:   state.PullRequest{State: state.PRStateOpen, Health: triage.HealthClean},
		},
		{
			name:   "a failing commit status",
			node:   `"state": "OPEN", "mergeable": "MERGEABLE"`,
			checks: failing,
			want:   state.PullRequest{State: state.PRStateOpen, Health: triage.HealthFailing},
		},
		{
			name:   "a check still running",
			node:   `"state": "OPEN"`,
			checks: `{"__typename": "CheckRun", "name": "test", "conclusion": null}`,
			want:   state.PullRequest{State: state.PRStateOpen, Health: triage.HealthClean},
		},
		{
			name: "changes requested",
			node: `"state": "OPEN", "reviewDecision": "CHANGES_REQUESTED"`,
			want: state.PullRequest{State: state.PRStateOpen, Health: triage.HealthChangesRequested},
		},
		{
			name:   "failing checks outrank changes requested",
			node:   `"state": "OPEN", "reviewDecision": "CHANGES_REQUESTED"`,
			checks: failing,
			want:   state.PullRequest{State: state.PRStateOpen, Health: triage.HealthFailing},
		},
		{
			name: "a conflict",
			node: `"state": "OPEN", "mergeable": "CONFLICTING"`,
			want: state.PullRequest{State: state.PRStateOpen, Health: triage.HealthConflicting},
		},
		{
			name: "mergeability not worked out yet",
			node: `"state": "OPEN", "mergeable": "UNKNOWN"`,
			want: state.PullRequest{State: state.PRStateOpen, Health: triage.HealthClean},
		},
		{
			name: "a draft with a conflict",
			node: `"state": "OPEN", "isDraft": true, "mergeable": "CONFLICTING"`,
			want: state.PullRequest{State: state.PRStateDraft, Health: triage.HealthConflicting},
		},
		{
			name: "merged, with no health and when it merged",
			node: `"state": "MERGED", "closedAt": "2026-09-28T14:00:00Z", "mergeable": "CONFLICTING"`,
			want: state.PullRequest{State: state.PRStateMerged, ResolvedAt: merged},
		},
		{
			name: "closed",
			node: `"state": "CLOSED", "closedAt": "2026-09-28T14:00:00Z"`,
			want: state.PullRequest{State: state.PRStateClosed, ResolvedAt: merged},
		},
		{
			name: "open, with no rollup and no close time",
			node: `"state": "OPEN", "closedAt": null`,
			want: state.PullRequest{State: state.PRStateOpen, Health: triage.HealthClean},
		},
		{
			name:   "a canceled run a newer one passed",
			node:   `"state": "OPEN"`,
			checks: run("PR Checks", "CANCELLED", "2026-10-16T13:16:02Z") + ", " + run("PR Checks", "SUCCESS", "2026-10-16T13:40:00Z"), //nolint:misspell // GitHub's spelling of the enum
			want:   state.PullRequest{State: state.PRStateOpen, Health: triage.HealthClean},
		},
		{
			name:   "a passing run a newer one failed",
			node:   `"state": "OPEN"`,
			checks: run("PR Checks", "SUCCESS", "2026-10-16T13:16:02Z") + ", " + run("PR Checks", "FAILURE", "2026-10-16T13:40:00Z"),
			want:   state.PullRequest{State: state.PRStateOpen, Health: triage.HealthFailing},
		},
		{
			name:   "a failure in another workflow of the same name",
			node:   `"state": "OPEN"`,
			checks: run("Lint", "FAILURE", "2026-10-16T13:00:00Z") + ", " + run("PR Checks", "SUCCESS", "2026-10-16T13:40:00Z"),
			want:   state.PullRequest{State: state.PRStateOpen, Health: triage.HealthFailing},
		},
		{
			name: "a commit status a newer one replaced",
			node: `"state": "OPEN"`,
			checks: `{"__typename": "StatusContext", "context": "ci", "state": "ERROR", "createdAt": "2026-10-16T13:00:00Z"}, ` +
				`{"__typename": "StatusContext", "context": "ci", "state": "SUCCESS", "createdAt": "2026-10-16T13:40:00Z"}`,
			want: state.PullRequest{State: state.PRStateOpen, Health: triage.HealthClean},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rollup := "null"
			if tt.checks != "" {
				rollup = `{"contexts": {"nodes": [` + tt.checks + `]}}`
			}
			out := `{"data": {"repository": {"pr9": {"number": 9, "url": "https://github.com/o/r/pull/9", "title": "t", ` +
				tt.node + `, "commits": {"nodes": [{"commit": {"statusCheckRollup": ` + rollup + `}}]}}}}}`
			pr := state.PullRequest{URL: "https://github.com/o/r/pull/9", Number: 9, Source: state.SourceCapture}

			got, err := pulls.NewRefresher(replying(out, nil)).Refresh(t.Context(), []state.PullRequest{pr}, now)

			require.NoError(t, err)
			want := tt.want
			want.URL, want.Number, want.Title, want.Source = pr.URL, 9, "t", state.SourceCapture
			require.Equal(t, []state.PullRequest{want}, got)
		})
	}
}

func TestLiveGH(t *testing.T) {
	if os.Getenv("AGENT_INBOX_LIVE_GH") != "1" {
		t.Skip("set AGENT_INBOX_LIVE_GH=1 to ask GitHub about this repository's own branches")
	}
	root, err := filepath.Abs("../..")
	require.NoError(t, err)

	found, err := pulls.FindByBranch(t.Context(), pulls.GH{}, root, []string{"scaffold", "triage", "state"})

	require.NoError(t, err)
	for _, pr := range found {
		t.Logf("%s #%d %s %s %q", pr.URL, pr.Number, pr.State, pr.Health, pr.Title)
	}
	numbers := make([]int, 0, len(found))
	for _, pr := range found {
		numbers = append(numbers, pr.Number)
	}
	require.Equal(t, []int{1, 3, 4}, numbers)

	refreshed, err := pulls.NewRefresher(pulls.GH{}).Refresh(t.Context(), found, time.Now())
	require.NoError(t, err)
	require.Equal(t, found, refreshed)
}
