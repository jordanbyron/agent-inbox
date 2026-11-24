// Package verbs holds what you do to an agent: start, restore, settle,
// snooze, wake, pin, alias, link, delete and open it. Each operation reads
// the state, acts, writes through the store and returns.
package verbs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

// LabelPrefix starts the block label of every agent agent-inbox launched.
const LabelPrefix = "agent-inbox:"

// BlockLabel returns the label of the block a launched agent runs in.
func BlockLabel(id string) string {
	return LabelPrefix + id
}

// Rex is the part of rex.Client the verbs use.
type Rex interface {
	Sessions(ctx context.Context) ([]rex.Session, error)
	View(ctx context.Context, sessionID string) (rex.View, error)
	Start(ctx context.Context, opts rex.StartOptions) (rex.Started, error)
	CloseBlock(ctx context.Context, sessionID, blockID string) error
	CloseWindow(ctx context.Context, sessionID, windowID string) error
	Show(ctx context.Context, sessionID, blockID string) error
	OpenLayer(ctx context.Context, opts rex.LayerOptions) (rex.Layer, error)
	Park(ctx context.Context, opts rex.ParkOptions) (string, error)
}

// Inbox is everything the verbs need. Every field is required except
// Snapshot, which may be nil before the watcher has written one.
type Inbox struct {
	Store  *state.Store
	Rex    Rex
	Config config.Config
	Git    worktree.Git
	GH     pulls.Runner
	System System
	Now    func() time.Time
	// Snapshot returns the watcher's last snapshot, or an empty one when
	// there is none yet.
	Snapshot func() (snapshot.Snapshot, error)
	// Getenv reads the environment, for the pane a verb runs from.
	Getenv func(string) string
	// Home holds the harnesses' own files, where a session id is looked up.
	Home string
	// Form is the command that shows the new-agent form: this binary's own
	// absolute path and "new".
	Form []string
	// Parked is the command for the inbox parked on the left of each new
	// agent's tab. Empty parks none.
	Parked []string
}

// Target says which agent a verb is aimed at: an agent key, or a block id.
// With neither, it is the agent in the Rex block the command runs from.
type Target struct {
	Key   string
	Block string
}

// Agent is the agent a verb acted on, as the caller names it.
type Agent struct {
	Key string
	// Name is the alias if one is set, else the agent's name.
	Name     string
	Launched bool
}

type agent struct {
	key      string
	launched bool
	record   state.Record
	row      snapshot.Row
}

type place struct {
	session string
	window  string
	block   string
	label   string
	alone   bool
}

type places struct {
	byBlock map[string]place
	byLabel map[string]place
}

func (in *Inbox) target(ctx context.Context, t Target) (*state.State, agent, error) {
	st, err := in.Store.Read()
	if err != nil {
		return nil, agent{}, err
	}
	a, err := in.resolve(ctx, st, t)
	return st, a, err
}

func (in *Inbox) resolve(ctx context.Context, st *state.State, t Target) (agent, error) {
	switch {
	case t.Key != "" && t.Block != "":
		return agent{}, errors.New("give an agent or a block, not both")
	case t.Key != "":
		return in.byKey(st, t.Key)
	case t.Block != "":
		return in.byBlock(ctx, st, t.Block)
	}
	pane, ok := rex.PaneFromEnv(in.Getenv)
	if !ok {
		return agent{}, errors.New("no agent given: name one, pass --block, or run this from an agent's Rex block")
	}
	return in.byBlock(ctx, st, pane.BlockID)
}

func (in *Inbox) byKey(st *state.State, key string) (agent, error) {
	if rec, ok := st.Records[key]; ok {
		return agent{key: key, launched: true, record: rec}, nil
	}
	snap, err := in.snapshot()
	if err != nil {
		return agent{}, err
	}
	if row, ok := snap.Find(key); ok && !row.Launched {
		return agent{key: key, row: row}, nil
	}
	return agent{}, fmt.Errorf("no agent %q", key)
}

func (in *Inbox) byBlock(ctx context.Context, st *state.State, blockID string) (agent, error) {
	all, err := in.places(ctx)
	if err != nil {
		return agent{}, err
	}
	if p, ok := all.byBlock[blockID]; ok {
		if id, ok := strings.CutPrefix(p.label, LabelPrefix); ok {
			if rec, ok := st.Records[id]; ok {
				return agent{key: id, launched: true, record: rec}, nil
			}
		}
	}
	snap, err := in.snapshot()
	if err != nil {
		return agent{}, err
	}
	if row, ok := snap.Find(blockID); ok && !row.Launched {
		return agent{key: blockID, row: row}, nil
	}
	return agent{}, fmt.Errorf("block %s is not an agent", blockID)
}

func (in *Inbox) snapshot() (snapshot.Snapshot, error) {
	if in.Snapshot == nil {
		return snapshot.Snapshot{}, nil
	}
	snap, err := in.Snapshot()
	if err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("reading the snapshot: %w", err)
	}
	return snap, nil
}

// A found agent has no record to name a parent, so it is its own root.
func root(st *state.State, a agent) agent {
	if !a.launched {
		return a
	}
	seen := map[string]bool{a.key: true}
	for {
		parent, ok := st.Records[a.record.Parent]
		if !ok || seen[a.record.Parent] {
			return a
		}
		seen[a.record.Parent] = true
		a = agent{key: a.record.Parent, launched: true, record: parent}
	}
}

func family(st *state.State, top agent) []agent {
	if !top.launched {
		return []agent{top}
	}
	var members []agent
	for id, rec := range st.Records {
		member := agent{key: id, launched: true, record: rec}
		if id != top.key && root(st, member).key == top.key {
			members = append(members, member)
		}
	}
	slices.SortFunc(members, func(a, b agent) int {
		return cmp.Or(a.record.Began.Compare(b.record.Began), cmp.Compare(a.key, b.key))
	})
	return append([]agent{top}, members...)
}

func describe(st *state.State, a agent) Agent {
	name := st.Entries[a.key].Alias
	if name == "" {
		name = a.record.Name
	}
	if !a.launched && name == "" {
		name = a.row.Label
	}
	return Agent{Key: a.key, Name: name, Launched: a.launched}
}

// Rex can't look a block up by label, so finding a launched agent's block
// means reading every session's view.
func (in *Inbox) places(ctx context.Context) (places, error) {
	sessions, err := in.Rex.Sessions(ctx)
	if err != nil {
		return places{}, fmt.Errorf("listing Rex sessions: %w", err)
	}
	all := places{byBlock: map[string]place{}, byLabel: map[string]place{}}
	for _, s := range sessions {
		view, err := in.Rex.View(ctx, s.ID)
		// A session can go between listing it and asking for its view.
		if errors.Is(err, rex.ErrNotFound) {
			continue
		}
		if err != nil {
			return places{}, fmt.Errorf("reading Rex session %s: %w", s.Label, err)
		}
		for _, w := range view.Windows {
			labels := make([]string, len(w.Blocks))
			for i, b := range w.Blocks {
				labels[i] = b.Label
			}
			for _, b := range w.Blocks {
				p := place{session: s.ID, window: w.ID, block: b.ID, label: b.Label, alone: rex.OnlyAgentIn(labels)}
				all.byBlock[b.ID] = p
				if b.Label != "" {
					all.byLabel[b.Label] = p
				}
			}
		}
	}
	return all, nil
}

func (p places) placeOf(a agent) (place, bool) {
	if a.launched {
		found, ok := p.byLabel[BlockLabel(a.key)]
		return found, ok
	}
	found, ok := p.byBlock[a.key]
	return found, ok
}

func (in *Inbox) closeBlocks(ctx context.Context, members []agent) error {
	all, err := in.places(ctx)
	if err != nil {
		return err
	}
	for _, m := range members {
		p, ok := all.placeOf(m)
		if !ok {
			continue
		}
		err := rex.CloseAgent(ctx, in.Rex, p.session, p.window, p.block, p.alone)
		if err != nil && !errors.Is(err, rex.ErrNotFound) {
			return fmt.Errorf("closing the tab of %s: %w", m.key, err)
		}
	}
	return nil
}
