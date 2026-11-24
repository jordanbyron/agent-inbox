package watcher

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// closing is a tab auto-settle closes, and why.
type closing struct {
	key       string
	sessionID string
	windowID  string
	reason    string
}

// toAutoSettle returns the tabs to close for every family whose work has
// landed. Only a launched root can settle its family: a found agent has no
// start time, so none of its pull requests can be known to be its own.
func toAutoSettle(agents []agent, st *state.State, now time.Time) []closing {
	var out []closing
	for _, fam := range families(agents) {
		root := fam.root
		if !root.launched || !root.live() || slices.ContainsFunc(fam.members(), agent.anyWorking) {
			continue
		}
		entry := st.Entries[root.key]
		var children []triage.Agent
		shown := root.seen
		var prs []state.PullRequest
		for _, a := range fam.members() {
			prs = append(prs, pulls.Effective(st.PullRequests[a.key])...)
			if a.key != root.key {
				children = append(children, a.triage(st.Entries[a.key], st.PullRequests[a.key]))
				shown = shown || a.seen
			}
		}
		row := triage.Row{
			Agent: familyAgent(root.triage(entry, st.PullRequests[root.key]), children),
			Entry: entry,
		}
		if !row.ShouldAutoSettle(now, shown) {
			continue
		}
		reason := "its pull requests resolved: " + resolved(prs)
		for _, a := range fam.members() {
			if a.live() {
				out = append(out, closing{key: a.key, sessionID: a.sessionID, windowID: a.block.windowID, reason: reason})
			}
		}
	}
	return out
}

// resolved names the resolved pull requests, as in "#912 merged".
func resolved(prs []state.PullRequest) string {
	var names []string
	seen := map[string]bool{}
	for _, pr := range prs {
		if pr.ResolvedAt.IsZero() || seen[pr.URL] {
			continue
		}
		seen[pr.URL] = true
		names = append(names, fmt.Sprintf("#%d %s", pr.Number, strings.ToLower(string(pr.State))))
	}
	return strings.Join(names, ", ")
}
