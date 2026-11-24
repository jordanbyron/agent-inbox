package watcher

import (
	"cmp"
	"maps"
	"slices"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

// world is the watcher's picture of the Rex server: its sessions, their
// blocks and status records, and which sessions each app is attached to.
// Only the watcher's own loop touches it.
type world struct {
	sessions map[string]*session
	// apps holds the session ids each connected Rex app is attached to.
	apps map[string][]string
}

type session struct {
	id           string
	activeWindow string
	revision     int
	blocks       map[string]*block
}

type block struct {
	id       string
	label    string
	windowID string
	records  []rex.StatusRecord
	// dir is the block's working directory, read once it is a found agent.
	dir      string
	dirKnown bool
}

func newWorld() *world {
	return &world{sessions: map[string]*session{}, apps: map[string][]string{}}
}

// setView replaces what is known of a session's windows with view. A block
// seen before keeps its records and directory; a new one is returned so the
// caller can ask for its status.
func (w *world) setView(view rex.View) (added []*block) {
	s := w.sessions[view.SessionID]
	if s == nil {
		s = &session{id: view.SessionID, blocks: map[string]*block{}}
		w.sessions[view.SessionID] = s
	}
	s.activeWindow = view.ActiveWindowID
	s.revision = view.Revision
	blocks := map[string]*block{}
	for _, win := range view.Windows {
		for _, b := range win.Blocks {
			known := s.blocks[b.ID]
			if known == nil {
				known = &block{id: b.ID}
				added = append(added, known)
			}
			known.label = b.Label
			known.windowID = win.ID
			blocks[b.ID] = known
		}
	}
	s.blocks = blocks
	return added
}

func (w *world) block(sessionID, blockID string) *block {
	if s := w.sessions[sessionID]; s != nil {
		return s.blocks[blockID]
	}
	return nil
}

func (w *world) removeBlock(sessionID, blockID string) {
	if s := w.sessions[sessionID]; s != nil {
		delete(s.blocks, blockID)
	}
}

func (w *world) removeWindow(sessionID, windowID string) {
	s := w.sessions[sessionID]
	if s == nil {
		return
	}
	maps.DeleteFunc(s.blocks, func(_ string, b *block) bool { return b.windowID == windowID })
	if s.activeWindow == windowID {
		s.activeWindow = ""
	}
}

func (w *world) setActiveWindow(sessionID, windowID string) {
	if s := w.sessions[sessionID]; s != nil {
		s.activeWindow = windowID
	}
}

func (w *world) setApps(apps []rex.App) {
	w.apps = map[string][]string{}
	for _, app := range apps {
		w.apps[app.ClientID] = app.SessionIDs
	}
}

func (w *world) isApp(clientID string) bool {
	_, ok := w.apps[clientID]
	return ok
}

// shown reports whether a Rex app is showing the window. Rex keeps one
// active window per session, not per app, so this is the best it can say:
// an app attached to a session it isn't displaying still counts.
func (w *world) shown(sessionID, windowID string) bool {
	s := w.sessions[sessionID]
	if s == nil || windowID == "" || s.activeWindow != windowID {
		return false
	}
	for _, sessionIDs := range w.apps {
		if slices.Contains(sessionIDs, sessionID) {
			return true
		}
	}
	return false
}

// located is a block and the session it is in.
type located struct {
	sessionID string
	*block
}

// blocks returns every block in a fixed order, so the same picture always
// yields the same agents.
func (w *world) blocks() []located {
	var all []located
	for _, s := range w.sessions {
		for _, b := range s.blocks {
			all = append(all, located{sessionID: s.id, block: b})
		}
	}
	slices.SortFunc(all, func(a, b located) int {
		return cmp.Or(cmp.Compare(a.sessionID, b.sessionID), cmp.Compare(a.id, b.id))
	})
	return all
}

// setRecord replaces the block's record with the same id, as Rex does with
// each report.
func (b *block) setRecord(record rex.StatusRecord) {
	i := slices.IndexFunc(b.records, func(r rex.StatusRecord) bool { return r.ID == record.ID })
	if i < 0 {
		b.records = append(b.records, record)
		return
	}
	b.records[i] = record
}

func (b *block) removeRecords(ids []string) {
	b.records = slices.DeleteFunc(b.records, func(r rex.StatusRecord) bool { return slices.Contains(ids, r.ID) })
}
