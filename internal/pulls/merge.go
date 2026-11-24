package pulls

import (
	"slices"

	"github.com/jordanbyron/agent-inbox/internal/state"
)

var sourceRank = map[state.Source]int{
	state.SourceCapture: 1,
	state.SourceBranch:  2,
	state.SourceLinked:  3,
}

// Merge adds what was just found for one agent to what it remembers, new
// ones last. One found again keeps its resolved-at, and linked outranks
// branch, which outranks capture, as its source.
func Merge(remembered, found []state.PullRequest) []state.PullRequest {
	merged := make([]state.PullRequest, 0, len(remembered)+len(found))
	index := map[string]int{}
	for _, pr := range slices.Concat(remembered, found) {
		i, ok := index[pr.URL]
		if !ok {
			index[pr.URL] = len(merged)
			merged = append(merged, pr)
			continue
		}
		merged[i] = combine(merged[i], pr)
	}
	return merged
}

func combine(old, found state.PullRequest) state.PullRequest {
	pr := old
	if found.State != state.PRStateUnknown {
		pr = found
		pr.ResolvedAt = old.ResolvedAt
		if pr.ResolvedAt.IsZero() {
			pr.ResolvedAt = found.ResolvedAt
		}
	}
	pr.Source = old.Source
	if sourceRank[found.Source] > sourceRank[old.Source] {
		pr.Source = found.Source
	}
	return pr
}

// Effective returns the pull requests that speak for an agent: the linked
// ones if it has any, or else all of them.
func Effective(prs []state.PullRequest) []state.PullRequest {
	var linked []state.PullRequest
	for _, pr := range prs {
		if pr.Source == state.SourceLinked {
			linked = append(linked, pr)
		}
	}
	if linked != nil {
		return linked
	}
	return prs
}
