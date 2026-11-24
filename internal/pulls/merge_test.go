package pulls_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

func TestMerge(t *testing.T) {
	pr := func(n int, source state.Source) state.PullRequest {
		return state.PullRequest{URL: "https://github.com/o/r/pull/" + strconv.Itoa(n), Number: n, Source: source}
	}
	known := func(p state.PullRequest, st state.PRState, h triage.Health) state.PullRequest {
		p.State, p.Health, p.Title = st, h, "t"
		return p
	}
	resolved := func(p state.PullRequest, at time.Time) state.PullRequest {
		p.State, p.ResolvedAt = state.PRStateMerged, at
		return p
	}
	tests := []struct {
		name       string
		remembered []state.PullRequest
		found      []state.PullRequest
		want       []state.PullRequest
	}{
		{
			name:  "into nothing remembered, de-duplicated in order",
			found: []state.PullRequest{pr(2, state.SourceBranch), pr(1, state.SourceCapture), pr(2, state.SourceBranch)},
			want:  []state.PullRequest{pr(2, state.SourceBranch), pr(1, state.SourceCapture)},
		},
		{
			name:       "new ones go after the remembered ones",
			remembered: []state.PullRequest{pr(3, state.SourceCapture)},
			found:      []state.PullRequest{pr(1, state.SourceBranch), pr(3, state.SourceCapture)},
			want:       []state.PullRequest{pr(3, state.SourceCapture), pr(1, state.SourceBranch)},
		},
		{
			name:       "linked wins over how it was found before",
			remembered: []state.PullRequest{known(pr(1, state.SourceBranch), state.PRStateOpen, triage.HealthFailing)},
			found:      []state.PullRequest{pr(1, state.SourceLinked)},
			want:       []state.PullRequest{known(pr(1, state.SourceLinked), state.PRStateOpen, triage.HealthFailing)},
		},
		{
			name:       "a capture does not demote a linked one",
			remembered: []state.PullRequest{pr(1, state.SourceLinked)},
			found:      []state.PullRequest{pr(1, state.SourceCapture)},
			want:       []state.PullRequest{pr(1, state.SourceLinked)},
		},
		{
			name:       "a branch outranks a capture",
			remembered: []state.PullRequest{pr(1, state.SourceCapture)},
			found:      []state.PullRequest{known(pr(1, state.SourceBranch), state.PRStateOpen, triage.HealthClean)},
			want:       []state.PullRequest{known(pr(1, state.SourceBranch), state.PRStateOpen, triage.HealthClean)},
		},
		{
			name:       "what gh newly said replaces what was remembered",
			remembered: []state.PullRequest{known(pr(1, state.SourceBranch), state.PRStateOpen, triage.HealthClean)},
			found:      []state.PullRequest{known(pr(1, state.SourceBranch), state.PRStateOpen, triage.HealthConflicting)},
			want:       []state.PullRequest{known(pr(1, state.SourceBranch), state.PRStateOpen, triage.HealthConflicting)},
		},
		{
			name:       "a bare link found again keeps what gh said",
			remembered: []state.PullRequest{known(pr(1, state.SourceCapture), state.PRStateOpen, triage.HealthFailing)},
			found:      []state.PullRequest{pr(1, state.SourceCapture)},
			want:       []state.PullRequest{known(pr(1, state.SourceCapture), state.PRStateOpen, triage.HealthFailing)},
		},
		{
			name:       "an existing entry keeps its resolved-at",
			remembered: []state.PullRequest{resolved(pr(1, state.SourceBranch), now.Add(-time.Hour))},
			found:      []state.PullRequest{resolved(pr(1, state.SourceBranch), now)},
			want:       []state.PullRequest{resolved(pr(1, state.SourceBranch), now.Add(-time.Hour))},
		},
		{
			name:       "an entry without one takes the found one's",
			remembered: []state.PullRequest{known(pr(1, state.SourceBranch), state.PRStateMerged, triage.HealthUnknown)},
			found:      []state.PullRequest{resolved(pr(1, state.SourceBranch), now)},
			want:       []state.PullRequest{resolved(pr(1, state.SourceBranch), now)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, pulls.Merge(tt.remembered, tt.found))
		})
	}
}

func TestEffective(t *testing.T) {
	branch := state.PullRequest{URL: "https://github.com/o/r/pull/856", Source: state.SourceBranch}
	capture := state.PullRequest{URL: "https://github.com/o/r/pull/866", Source: state.SourceCapture}
	linked := state.PullRequest{URL: "https://github.com/o/r/pull/1", Source: state.SourceLinked}
	tests := []struct {
		name string
		prs  []state.PullRequest
		want []state.PullRequest
	}{
		{name: "none", prs: nil, want: nil},
		{name: "all of them without a link", prs: []state.PullRequest{branch, capture}, want: []state.PullRequest{branch, capture}},
		{name: "a link overrides the rest", prs: []state.PullRequest{branch, linked, capture}, want: []state.PullRequest{linked}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, pulls.Effective(tt.prs))
		})
	}
}
