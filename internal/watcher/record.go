package watcher

import (
	"slices"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/state"
)

// recorder brings the state file up to date with what Rex shows. It keeps
// what it needs from one observation to the next: which agents were live
// and which were seen. Only the watcher's loop uses it.
type recorder struct {
	// live is nil until the first observation, which then also tidies up
	// after whatever closed while no watcher was running.
	live      map[string]bool
	seen      map[string]bool
	rechecked map[string]bool
}

// observation is one pass of the recorder over the state, made inside
// Store.Update and kept only if the update succeeds.
type observation struct {
	live    map[string]bool
	seen    map[string]bool
	recheck map[string]bool
}

// observe records the agents in st: each live agent's state, an
// acknowledgment for each one you are looking at, the pull requests found
// since last time, and what became of the agents whose block closed.
func (r *recorder) observe(st *state.State, agents []agent, found []finding, now time.Time) observation {
	obs := observation{live: map[string]bool{}, seen: map[string]bool{}, recheck: map[string]bool{}}
	for _, a := range agents {
		if a.live() {
			obs.live[a.key] = true
		}
		if a.seen {
			obs.seen[a.key] = true
		}
	}

	r.forgetClosed(st, obs.live, obs.recheck, now)
	// Counted as live, an agent to recheck is looked at again next time.
	for key := range obs.recheck {
		obs.live[key] = true
	}
	for _, f := range found {
		// A found agent whose block closed while its job ran is gone.
		if _, launched := st.Records[f.key]; f.key == "" || launched || obs.live[f.key] {
			f.mergeInto(st)
		}
	}
	for key, entry := range st.Entries {
		st.PullRequests[key] = linked(st.PullRequests[key], entry.LinkedPR)
	}
	for _, a := range agents {
		if !a.live() {
			continue
		}
		entry := st.Entries[a.key]
		entry.Observe(a.triage(entry, st.PullRequests[a.key]), now)
		if a.seen && (!r.seen[a.key] || !entry.Acknowledged()) {
			entry.Acknowledge(now)
		}
		st.Entries[a.key] = entry
	}
	for key, prs := range st.PullRequests {
		if len(prs) == 0 {
			delete(st.PullRequests, key)
		}
	}
	return obs
}

// commit keeps what an observation saw, once its update is written.
func (r *recorder) commit(obs observation) {
	r.live, r.seen, r.rechecked = obs.live, obs.seen, nil
}

// forgetClosed settles a launched agent whose block closed, and drops all
// that was kept for a found agent whose block closed: it is known only by
// that block, so nothing could bring it back.
func (r *recorder) forgetClosed(st *state.State, live, recheck map[string]bool, now time.Time) {
	closed := func(key string) bool { return r.live[key] && !live[key] }
	if r.live == nil {
		// At startup nothing is known of what was live, so whatever is
		// remembered and not live now closed while no watcher was running.
		closed = func(key string) bool { return !live[key] }
	}
	keys := slices.Concat(sortedKeys(st.Entries), sortedKeys(st.PullRequests))
	for _, key := range keys {
		if !closed(key) {
			continue
		}
		if _, launched := st.Records[key]; !launched {
			delete(st.Entries, key)
			delete(st.PullRequests, key)
			continue
		}
		entry := st.Entries[key]
		// An entry never observed is an agent still being started.
		if !entry.SettledAt.IsZero() || entry.LastState == "" {
			continue
		}
		// A restore writes the state after it opens the new tab, so the
		// tab's events may not have reached the watcher yet.
		if !entry.RevivedAt.IsZero() && !r.rechecked[key] {
			recheck[key] = true
			continue
		}
		entry.Settle(now)
		st.Entries[key] = entry
	}
}

// linked makes the hand-linked pull request one of prs, and drops one that
// was linked before and isn't now.
func linked(prs []state.PullRequest, url string) []state.PullRequest {
	prs = slices.DeleteFunc(slices.Clone(prs), func(pr state.PullRequest) bool {
		return pr.Source == state.SourceLinked && pr.URL != url
	})
	if url == "" {
		return prs
	}
	pr, ok := linkedPullRequest(url)
	if !ok {
		return prs
	}
	return pulls.Merge(prs, []state.PullRequest{pr})
}

func linkedPullRequest(url string) (state.PullRequest, bool) {
	found := pulls.FindInText(url)
	if len(found) != 1 || found[0].URL != url {
		return state.PullRequest{}, false
	}
	pr := found[0]
	pr.Source = state.SourceLinked
	return pr, true
}
