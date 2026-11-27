package verbs

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/snoozetime"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// Snooze is when a snoozed agent wakes: at a time, or only when woken.
type Snooze struct {
	Until      time.Time
	UntilWoken bool
}

// ParseSnooze reads a snooze as typed: until-woken, or anything snoozetime
// reads, such as 15m, 1h or tomorrow.
func ParseSnooze(text string, now time.Time) (Snooze, error) {
	if strings.TrimSpace(text) == "until-woken" {
		return Snooze{UntilWoken: true}, nil
	}
	until, ok := snoozetime.Parse(text, now)
	if !ok {
		return Snooze{}, fmt.Errorf("can't read %q as a snooze: try 15m, 1h, tomorrow, fri 5pm or until-woken", text)
	}
	return Snooze{Until: until}, nil
}

// Snooze hides the target's family until s.
func (in *Inbox) Snooze(ctx context.Context, t Target, s Snooze) (Agent, error) {
	return in.editRoot(ctx, t, func(e *triage.Entry, _ []triage.PullRequest) {
		if s.UntilWoken {
			e.SnoozeUntilWoken()
		} else {
			e.Snooze(s.Until)
		}
	})
}

// Wake ends the family's snooze. On a launched agent with no block, which
// is a settled row or a closed child, it restores that agent instead, as u
// does in the list; restored says which happened.
func (in *Inbox) Wake(ctx context.Context, t Target) (a Agent, restored bool, err error) {
	st, target, err := in.target(ctx, t)
	if err != nil {
		return Agent{}, false, err
	}
	if target.launched {
		all, err := in.places(ctx)
		if err != nil {
			return Agent{}, false, err
		}
		if _, running := all.placeOf(target); !running {
			a, err := in.restore(ctx, target)
			return a, err == nil, err
		}
	}
	a, err = in.edit(root(st, target), func(e *triage.Entry, prs []triage.PullRequest) {
		e.Wake(triage.Agent{PullRequests: prs}, in.Now())
	})
	return a, false, err
}

// TogglePin pins the target's family, or unpins it, and reports which.
func (in *Inbox) TogglePin(ctx context.Context, t Target) (a Agent, pinned bool, err error) {
	a, err = in.editRoot(ctx, t, func(e *triage.Entry, _ []triage.PullRequest) {
		e.TogglePin(in.Now())
		pinned = e.Pinned()
	})
	return a, pinned, err
}

// Alias names the target's family, and an empty name clears the alias.
func (in *Inbox) Alias(ctx context.Context, t Target, name string) (Agent, error) {
	return in.editRoot(ctx, t, func(e *triage.Entry, _ []triage.PullRequest) {
		e.Alias = strings.TrimSpace(name)
	})
}

// Link sets the pull request that speaks for the target's family, typed as
// pulls.Link reads it, and empty input clears the link. It returns the
// link's URL.
func (in *Inbox) Link(ctx context.Context, t Target, input string) (a Agent, url string, err error) {
	st, target, err := in.target(ctx, t)
	if err != nil {
		return Agent{}, "", err
	}
	top := root(st, target)
	var linked []state.PullRequest
	if strings.TrimSpace(input) != "" {
		dir := top.record.Dir
		if !top.launched {
			dir = top.row.Dir
		}
		pr, err := pulls.Link(ctx, in.GH, dir, input)
		if err != nil {
			return Agent{}, "", fmt.Errorf("linking %s: %w", input, err)
		}
		linked = []state.PullRequest{pr}
		url = pr.URL
	}
	err = in.Store.Update(func(st *state.State) error {
		if err := stillThere(st, top); err != nil {
			return err
		}
		e := st.Entries[top.key]
		e.LinkedPR = url
		st.Entries[top.key] = e
		// Only one link is kept, so linking another replaces the last.
		unlinked := slices.DeleteFunc(slices.Clone(st.PullRequests[top.key]), func(pr state.PullRequest) bool {
			return pr.Source == state.SourceLinked
		})
		st.PullRequests[top.key] = pulls.Merge(unlinked, linked)
		if len(st.PullRequests[top.key]) == 0 {
			delete(st.PullRequests, top.key)
		}
		a = describe(st, top)
		return nil
	})
	return a, url, err
}

// Settle closes the tab of every member of the target's family and settles
// them. A found agent has nothing to restore it from, so once its tab is
// closed it is forgotten.
func (in *Inbox) Settle(ctx context.Context, t Target) (Agent, error) {
	st, target, err := in.target(ctx, t)
	if err != nil {
		return Agent{}, err
	}
	top := root(st, target)
	members := family(st, top)
	if err := in.closeBlocks(ctx, members); err != nil {
		return Agent{}, fmt.Errorf("settling %s: %w", top.key, err)
	}
	var a Agent
	err = in.Store.Update(func(st *state.State) error {
		a = describe(st, top)
		if !top.launched {
			delete(st.Entries, top.key)
			delete(st.PullRequests, top.key)
			return nil
		}
		for _, m := range members {
			if _, ok := st.Records[m.key]; !ok {
				continue
			}
			e := st.Entries[m.key]
			e.Settle(in.Now())
			st.Entries[m.key] = e
		}
		return nil
	})
	return a, err
}

func (in *Inbox) editRoot(ctx context.Context, t Target, fn func(*triage.Entry, []triage.PullRequest)) (Agent, error) {
	st, target, err := in.target(ctx, t)
	if err != nil {
		return Agent{}, err
	}
	return in.edit(root(st, target), fn)
}

func (in *Inbox) edit(a agent, fn func(*triage.Entry, []triage.PullRequest)) (Agent, error) {
	var out Agent
	err := in.Store.Update(func(st *state.State) error {
		if err := stillThere(st, a); err != nil {
			return err
		}
		e := st.Entries[a.key]
		fn(&e, familyPullRequests(st, a))
		st.Entries[a.key] = e
		out = describe(st, a)
		return nil
	})
	return out, err
}

// stillThere catches a delete that landed between reading the state and
// writing it back, which would otherwise leave an entry with no record.
func stillThere(st *state.State, a agent) error {
	if _, ok := st.Records[a.key]; a.launched && !ok {
		return fmt.Errorf("agent %s was deleted meanwhile", a.key)
	}
	return nil
}

// familyPullRequests are the pull requests of every member of a's family,
// since auto-settle weighs them together.
func familyPullRequests(st *state.State, a agent) []triage.PullRequest {
	var prs []triage.PullRequest
	for _, m := range family(st, root(st, a)) {
		for _, pr := range st.PullRequests[m.key] {
			prs = append(prs, pr.Triage())
		}
	}
	return prs
}
