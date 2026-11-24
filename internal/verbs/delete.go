package verbs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

// Delete closes the tabs of the target's family, removes its worktrees and
// branches, and forgets its records, entries and pull requests. A worktree
// with work in it that nothing else holds stops the delete before anything
// is touched. The harness's own transcripts are left alone.
func (in *Inbox) Delete(ctx context.Context, t Target) (a Agent, members int, err error) {
	st, target, err := in.target(ctx, t)
	if err != nil {
		return Agent{}, 0, err
	}
	top := root(st, target)
	family := family(st, top)
	a = describe(st, top)

	var worktrees []agent
	for _, m := range family {
		if m.record.Worktree == "" {
			continue
		}
		// A worktree removed by hand has nothing left to lose, so it doesn't
		// hold the delete up; its branch stays, in case it has work on it.
		if _, err := os.Stat(m.record.Worktree); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := worktree.Removable(ctx, in.Git, m.record.Worktree, m.record.Branch); err != nil {
			return Agent{}, 0, fmt.Errorf("not deleting %s: %w", a.Name, err)
		}
		worktrees = append(worktrees, m)
	}

	if err := in.closeBlocks(ctx, family); err != nil {
		return Agent{}, 0, fmt.Errorf("deleting %s: %w", a.Name, err)
	}
	for _, m := range worktrees {
		if err := worktree.Remove(ctx, in.Git, m.record.Worktree, m.record.Branch); err != nil {
			return Agent{}, 0, fmt.Errorf("deleting %s: %w", a.Name, err)
		}
	}
	err = in.Store.Update(func(st *state.State) error {
		for _, m := range family {
			delete(st.Records, m.key)
			delete(st.Entries, m.key)
			delete(st.PullRequests, m.key)
		}
		return nil
	})
	return a, len(family), err
}
