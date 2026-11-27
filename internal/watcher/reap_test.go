package watcher_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/reap"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
	"github.com/jordanbyron/agent-inbox/internal/watcher"
)

type fakeDeleter struct {
	mu      sync.Mutex
	refuse  map[string]bool
	deleted []string
}

func (d *fakeDeleter) DeleteFamily(_ context.Context, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleted = append(d.deleted, key)
	if d.refuse[key] {
		return errors.New("worktree /r/x has uncommitted changes")
	}
	return nil
}

func (d *fakeDeleter) asked() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.deleted)
}

func TestWatcherReapsWhatIsDue(t *testing.T) {
	cases := []struct {
		name   string
		reap   bool
		refuse bool
		log    string
		asked  []string
	}{
		{name: "reaps", reap: true, log: "reaped a family", asked: []string{"old"}},
		{name: "logs a refusal", reap: true, refuse: true, log: "kept a family reap is due to delete", asked: []string{"old"}},
		{name: "off by default", log: "watching"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			deleter := &fakeDeleter{refuse: map[string]bool{"old": c.refuse}}
			h.cfg.Reap, h.deleter = c.reap, deleter
			h.edit(func(st *state.State) {
				for _, key := range []string{"old", "new"} {
					st.Records[key] = launched("work", "/r/"+key)
				}
				st.Entries["old"] = triage.Entry{LastState: triage.StatusIdle, SettledAt: t0.Add(-reap.After)}
				st.Entries["new"] = triage.Entry{LastState: triage.StatusIdle, SettledAt: t0.Add(-time.Hour)}
			})

			h.start()

			h.waitLog(c.log)
			require.Equal(t, c.asked, deleter.asked())
		})
	}
}

// hungDeleter never finishes a delete until its context ends.
type hungDeleter struct{}

func (hungDeleter) DeleteFamily(ctx context.Context, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestAHungReapGivesUpAndTheWatcherGoesOn(t *testing.T) {
	watcher.ShortenTimeouts(t, 10*time.Millisecond)
	h := newHarness(t)
	h.cfg.Reap, h.deleter = true, hungDeleter{}
	h.edit(func(st *state.State) {
		st.Records["old"] = launched("work", "/r/old")
		st.Entries["old"] = triage.Entry{LastState: triage.StatusIdle, SettledAt: t0.Add(-reap.After)}
	})

	h.start()

	h.waitLog("kept a family reap is due to delete")
	h.heartbeat()
}
