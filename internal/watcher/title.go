package watcher

import (
	"context"

	"github.com/jordanbyron/agent-inbox/internal/recipe"
)

// titleJob reads the title a launched agent's harness has given its
// conversation, and retitles the agent's tab to match.
type titleJob struct {
	key    string
	titles recipe.TitleFinder
	query  recipe.TitleQuery
	// had is the title the record holds, and shown the tab label it was
	// given, which a tab still wearing it can be retitled from.
	had, shown string
	sessionID  string
	windowID   string
}

// titleJob is the job for a, and false for an agent whose harness gives
// no titles or whose tab is closed.
func (w *watcher) titleJob(a agent) (titleJob, bool) {
	if !a.live() || !a.launched || a.record.SessionID == "" {
		return titleJob{}, false
	}
	r, ok := w.cfg.Recipes.Named(a.harness)
	if !ok || r.Titles == nil {
		return titleJob{}, false
	}
	return titleJob{
		key:       a.key,
		titles:    r.Titles,
		query:     recipe.TitleQuery{Home: w.finder.home, SessionID: a.record.SessionID},
		had:       a.record.Title,
		shown:     a.record.Label(),
		sessionID: a.sessionID,
		windowID:  a.block.windowID,
	}, true
}

func (j titleJob) id() string { return "title " + j.key }

func (j titleJob) run(ctx context.Context, f *finder) finding {
	title, ok, err := j.titles.FindTitle(ctx, j.query)
	if err != nil {
		f.warn(ctx, "reading an agent's title", "agent", j.key, "err", err)
	}
	if !ok || title == j.had {
		return finding{key: j.key}
	}
	if err := j.retitleTab(ctx, f, title); err != nil {
		f.warn(ctx, "retitling an agent's tab", "agent", j.key, "err", err)
	}
	return finding{key: j.key, title: title}
}

// retitleTab relabels the agent's tab, unless someone renamed it by hand.
func (j titleJob) retitleTab(ctx context.Context, f *finder, title string) error {
	view, err := f.rex.View(ctx, j.sessionID)
	if err != nil {
		return err
	}
	for _, w := range view.Windows {
		if w.ID == j.windowID && w.Label == j.shown {
			return f.rex.SetWindowLabel(ctx, j.sessionID, j.windowID, title)
		}
	}
	return nil
}
