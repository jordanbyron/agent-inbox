// Package reap chooses and deletes the families that have been settled
// long enough to be forgotten.
package reap

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/state"
)

// After is how long a family stays settled before it is reaped.
const After = 14 * 24 * time.Hour

// RetryAfter is how long a family that refused to be deleted is left alone
// before it is tried again.
const RetryAfter = 24 * time.Hour

// Family is a family due to be reaped, named by its root.
type Family struct {
	Key           string
	Name          string
	LastSettledAt time.Time
	Members       int
}

// Due returns the families in st that are due at now, oldest first: every
// member settled, the last of them After ago or more, and none pinned or
// snoozed. A found agent is forgotten when its block closes, so only
// launched families are ever due.
func Due(st *state.State, now time.Time) []Family {
	byRoot := map[string]*Family{}
	disqualified := map[string]bool{}
	for key := range st.Records {
		root := rootOf(st, key)
		fam := byRoot[root]
		if fam == nil {
			fam = &Family{Key: root, Name: name(st, root)}
			byRoot[root] = fam
		}
		fam.Members++
		entry := st.Entries[key]
		if entry.SettledAt.IsZero() || entry.Pinned() || entry.Snoozed(now) {
			disqualified[root] = true
		}
		if entry.SettledAt.After(fam.LastSettledAt) {
			fam.LastSettledAt = entry.SettledAt
		}
	}
	var due []Family
	for root, fam := range byRoot {
		if !disqualified[root] && now.Sub(fam.LastSettledAt) >= After {
			due = append(due, *fam)
		}
	}
	slices.SortFunc(due, func(a, b Family) int {
		return cmp.Or(a.LastSettledAt.Compare(b.LastSettledAt), cmp.Compare(a.Key, b.Key))
	})
	return due
}

// It follows parents as verbs does, since Delete acts on the root verbs finds.
func rootOf(st *state.State, key string) string {
	seen := map[string]bool{key: true}
	for {
		parent := st.Records[key].Parent
		if _, ok := st.Records[parent]; !ok || seen[parent] {
			return key
		}
		seen[parent] = true
		key = parent
	}
}

func name(st *state.State, key string) string {
	if alias := st.Entries[key].Alias; alias != "" {
		return alias
	}
	return cmp.Or(st.Records[key].Name, key)
}

// Deleter deletes a family by its root's key, with delete's refusals.
// *verbs.Inbox is the real one.
type Deleter interface {
	DeleteFamily(ctx context.Context, key string) error
}

// Result is what became of one family a sweep went after. Err is the
// refusal or failure that kept it.
type Result struct {
	Family
	Err error
}

// Reaper sweeps the due families, and leaves one that refused alone for
// RetryAfter rather than asking again at every sweep. Only one goroutine
// may use it.
type Reaper struct {
	Deleter Deleter
	refused map[string]time.Time
}

// Sweep deletes every family due in st at now, except those still backing
// off, and says what became of each. One refusal doesn't stop the rest.
func (r *Reaper) Sweep(ctx context.Context, st *state.State, now time.Time) []Result {
	if r.refused == nil {
		r.refused = map[string]time.Time{}
	}
	var results []Result
	for _, fam := range Due(st, now) {
		if at, ok := r.refused[fam.Key]; ok && now.Sub(at) < RetryAfter {
			continue
		}
		err := r.Deleter.DeleteFamily(ctx, fam.Key)
		if err != nil {
			r.refused[fam.Key] = now
		} else {
			delete(r.refused, fam.Key)
		}
		results = append(results, Result{Family: fam, Err: err})
	}
	return results
}
