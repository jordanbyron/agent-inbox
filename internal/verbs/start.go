package verbs

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jordanbyron/agent-inbox/internal/recipe"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

// StartRequest is what a new agent is started with.
type StartRequest struct {
	Prompt string
	// Harness names the recipe. Empty is the first recipe, Claude's unless
	// the config replaces it.
	Harness string
	// Name is filled from the prompt when empty.
	Name string
	Dir  string
	// Session is the id or label of the Rex session to start in. Empty
	// starts a new session.
	Session     string
	Model       string
	Effort      string
	Permissions string
	// Worktree asks for a worktree, which is made only when Dir is in a git
	// repository.
	Worktree bool
	Show     bool
}

// Start writes the new agent's record, makes its worktree and starts it in
// a new Rex tab, in that order. A failure undoes the steps before it. When
// only showing the tab fails, the agent is running and returned along with
// the error.
func (in *Inbox) Start(ctx context.Context, req StartRequest) (Agent, error) {
	r, err := in.recipe(req.Harness)
	if err != nil {
		return Agent{}, err
	}
	dir, err := filepath.Abs(req.Dir)
	if err != nil {
		return Agent{}, fmt.Errorf("starting an agent: %w", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Agent{}, fmt.Errorf("starting an agent: %s is not a directory", dir)
	}
	sessionID, err := in.sessionID(ctx, req.Session)
	if err != nil {
		return Agent{}, err
	}
	parent, err := in.parent(ctx)
	if err != nil {
		return Agent{}, err
	}
	rec := state.Record{
		Harness:    r.Name,
		Name:       req.Name,
		Dir:        dir,
		Parent:     parent,
		RexSession: sessionID,
		Began:      in.Now(),
	}
	if rec.Name == "" {
		rec.Name = worktree.NameFromPrompt(req.Prompt)
	}
	if r.TakesSessionID() {
		rec.SessionID = recipe.NewSessionID()
	}

	id, err := in.addRecord(rec)
	if err != nil {
		return Agent{}, err
	}
	forget := func(err error) error {
		return undone(err, in.Store.Update(func(st *state.State) error {
			delete(st.Records, id)
			return nil
		}))
	}

	if req.Worktree && worktree.InRepo(ctx, in.Git, dir) {
		wt, err := worktree.Create(ctx, in.Git, dir, rec.Name, in.Config.Worktree)
		if err != nil {
			return Agent{}, forget(err)
		}
		rec.Worktree, rec.Branch, rec.Dir = wt.Path, wt.Branch, wt.Path
		if err := in.putRecord(id, rec); err != nil {
			return Agent{}, forget(undone(err, worktree.Remove(ctx, in.Git, wt.Path, wt.Branch)))
		}
	}
	removeWorktree := func(err error) error {
		if rec.Worktree == "" {
			return err
		}
		return undone(err, worktree.Remove(ctx, in.Git, rec.Worktree, rec.Branch))
	}

	started, err := in.Rex.Start(ctx, rex.StartOptions{
		SessionID: sessionID,
		Command: r.StartArgs(recipe.Values{
			SessionID:   rec.SessionID,
			Name:        rec.Name,
			Model:       req.Model,
			Effort:      req.Effort,
			Permissions: req.Permissions,
			Prompt:      req.Prompt,
			Dir:         rec.Dir,
		}),
		Dir:         rec.Dir,
		BlockLabel:  BlockLabel(id),
		WindowLabel: rec.Name,
	})
	if err != nil {
		return Agent{}, forget(removeWorktree(fmt.Errorf("starting %s in Rex: %w", rec.Name, err)))
	}
	rec.RexSession = started.SessionID
	if err := in.putRecord(id, rec); err != nil {
		closeErr := in.Rex.CloseWindow(ctx, started.SessionID, started.WindowID)
		return Agent{}, forget(removeWorktree(undone(err, closeErr)))
	}

	a := Agent{Key: id, Name: rec.Name, Launched: true}
	if req.Show {
		if err := in.Rex.Show(ctx, started.SessionID, started.BlockID); err != nil {
			return a, err
		}
	}
	return a, nil
}

func (in *Inbox) recipe(name string) (recipe.Recipe, error) {
	if name == "" {
		if len(in.Config.Recipes) == 0 {
			return recipe.Recipe{}, errors.New("no recipes: the config has none and the built-ins are gone")
		}
		return in.Config.Recipes[0], nil
	}
	r, ok := in.Config.Recipes.Named(name)
	if !ok {
		var names []string
		for _, known := range in.Config.Recipes {
			names = append(names, known.Name)
		}
		return recipe.Recipe{}, fmt.Errorf("no harness %q: the recipes are %s", name, strings.Join(names, ", "))
	}
	return r, nil
}

// The record keeps the session's id, since a label can change.
func (in *Inbox) sessionID(ctx context.Context, idOrLabel string) (string, error) {
	if idOrLabel == "" {
		return "", nil
	}
	sessions, err := in.Rex.Sessions(ctx)
	if err != nil {
		return "", fmt.Errorf("listing Rex sessions: %w", err)
	}
	for _, s := range sessions {
		if s.ID == idOrLabel || s.Label == idOrLabel {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("no Rex session %q", idOrLabel)
}

// Rex keeps no parent for a block, so the block this command runs in is
// the only way to learn one.
func (in *Inbox) parent(ctx context.Context) (string, error) {
	pane, ok := rex.PaneFromEnv(in.Getenv)
	if !ok {
		return "", nil
	}
	view, err := in.Rex.View(ctx, pane.SessionID)
	if err != nil {
		return "", fmt.Errorf("reading the Rex session this runs in: %w", err)
	}
	st, err := in.Store.Read()
	if err != nil {
		return "", err
	}
	for _, w := range view.Windows {
		for _, b := range w.Blocks {
			id, launched := strings.CutPrefix(b.Label, LabelPrefix)
			if _, known := st.Records[id]; b.ID == pane.BlockID && launched && known {
				return id, nil
			}
		}
	}
	return "", nil
}

func (in *Inbox) addRecord(rec state.Record) (string, error) {
	var id string
	err := in.Store.Update(func(st *state.State) error {
		for id == "" || hasKey(st, id) {
			id = newID()
		}
		st.Records[id] = rec
		return nil
	})
	return id, err
}

func (in *Inbox) putRecord(id string, rec state.Record) error {
	return in.Store.Update(func(st *state.State) error {
		if _, ok := st.Records[id]; !ok {
			return fmt.Errorf("agent %s was deleted meanwhile", id)
		}
		st.Records[id] = rec
		return nil
	})
}

func hasKey(st *state.State, id string) bool {
	_, record := st.Records[id]
	_, entry := st.Entries[id]
	return record || entry
}

// newID is six characters of lowercase base32: short enough to type, and
// letters and digits only, so it is safe in a block label.
func newID() string {
	return strings.ToLower(rand.Text()[:6])
}

func undone(err, undoErr error) error {
	if undoErr == nil {
		return err
	}
	return errors.Join(err, fmt.Errorf("undoing it: %w", undoErr))
}
