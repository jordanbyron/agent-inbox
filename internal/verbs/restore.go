package verbs

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jordanbyron/agent-inbox/internal/recipe"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// Restore resumes a launched agent with no block in a new tab, with the
// same label and in the same directory, and clears its settle. It acts on
// the target itself, so a closed child comes back on its own.
func (in *Inbox) Restore(ctx context.Context, t Target) (Agent, error) {
	_, target, err := in.target(ctx, t)
	if err != nil {
		return Agent{}, err
	}
	if !target.launched {
		return Agent{}, fmt.Errorf("%s was found running in Rex, not started by agent-inbox, so there is nothing to restore it from", target.key)
	}
	all, err := in.places(ctx)
	if err != nil {
		return Agent{}, err
	}
	if _, running := all.placeOf(target); running {
		return Agent{}, fmt.Errorf("%s is already running", target.record.Label())
	}
	return in.restore(ctx, target)
}

// Open shows the target's own tab, restoring it first when it is a settled
// launched agent. A Rex app's refusal comes back as Rex worded it.
func (in *Inbox) Open(ctx context.Context, t Target) (Agent, error) {
	st, target, err := in.target(ctx, t)
	if err != nil {
		return Agent{}, err
	}
	all, err := in.places(ctx)
	if err != nil {
		return Agent{}, err
	}
	if p, running := all.placeOf(target); running {
		return describe(st, target), in.Rex.Show(ctx, p.session, p.block)
	}
	if !target.launched {
		return Agent{}, fmt.Errorf("%s has no block to show", target.key)
	}
	a, started, err := in.resume(ctx, target)
	if err != nil {
		return Agent{}, err
	}
	return a, in.Rex.Show(ctx, started.SessionID, started.BlockID)
}

func (in *Inbox) restore(ctx context.Context, a agent) (Agent, error) {
	out, _, err := in.resume(ctx, a)
	return out, err
}

// resume never starts the agent anywhere but its own directory: a harness
// resumed elsewhere would find no conversation, or the wrong one.
func (in *Inbox) resume(ctx context.Context, a agent) (Agent, rex.Started, error) {
	rec := a.record
	r, ok := in.Config.Recipes.Named(rec.Harness)
	if !ok {
		return Agent{}, rex.Started{}, fmt.Errorf("restoring %s: no recipe for harness %q", rec.Label(), rec.Harness)
	}
	if _, err := os.Stat(rec.Dir); err != nil {
		what := "directory"
		if rec.Worktree != "" {
			what = "worktree"
		}
		return Agent{}, rex.Started{}, fmt.Errorf("restoring %s: its %s %s is gone", rec.Label(), what, rec.Dir)
	}
	sessionID := rec.SessionID
	if sessionID == "" {
		found, ok, err := r.FindSessionID(ctx, recipe.SessionQuery{Home: in.Home, Dir: rec.Dir, Began: rec.Began})
		if err != nil {
			return Agent{}, rex.Started{}, fmt.Errorf("restoring %s: %w", rec.Label(), err)
		}
		if ok {
			sessionID = found
		}
	}
	opts := rex.StartOptions{
		SessionID:   rec.RexSession,
		Command:     r.ResumeArgs(recipe.Values{SessionID: sessionID, Name: rec.Name, Dir: rec.Dir}),
		Dir:         rec.Dir,
		BlockLabel:  BlockLabel(a.key),
		WindowLabel: rec.Label(),
	}
	started, err := in.Rex.Start(ctx, opts)
	// The session it ran in may have been closed since; a new one beats
	// refusing.
	if errors.Is(err, rex.ErrNotFound) && opts.SessionID != "" {
		opts.SessionID = ""
		started, err = in.Rex.Start(ctx, opts)
	}
	if err != nil {
		return Agent{}, rex.Started{}, fmt.Errorf("restoring %s in Rex: %w", rec.Label(), err)
	}

	var out Agent
	err = in.Store.Update(func(st *state.State) error {
		if err := stillThere(st, a); err != nil {
			return err
		}
		rec := st.Records[a.key]
		rec.SessionID = sessionID
		rec.RexSession = started.SessionID
		st.Records[a.key] = rec
		e := st.Entries[a.key]
		e.Wake(triage.Agent{PullRequests: familyPullRequests(st, a)}, in.Now())
		st.Entries[a.key] = e
		out = describe(st, a)
		return nil
	})
	return out, started, err
}
