package watcher

import (
	"cmp"
	"maps"
	"slices"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

// world is the watcher's picture of the Rex server: its sessions, their
// blocks and status records, and which tab each app is showing. Only the
// watcher's own loop touches it.
type world struct {
	sessions map[string]*session
	// apps holds the session ids each connected Rex app is attached to.
	apps map[string][]string
	// showing is the tab each app last displayed. An app with none yet
	// shows nothing as far as the watcher knows.
	showing map[string]tab
	// heard holds the apps that have displayed a tab since this picture
	// was scanned. Until every app has, what they show is a guess.
	heard map[string]bool
}

type tab struct {
	sessionID string
	windowID  string
}

type session struct {
	id       string
	revision int
	blocks   map[string]*block
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
	return &world{sessions: map[string]*session{}, apps: map[string][]string{}, showing: map[string]tab{}, heard: map[string]bool{}}
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

func (w *world) alone(sessionID, windowID string) bool {
	s := w.sessions[sessionID]
	if s == nil {
		return false
	}
	n := 0
	for _, b := range s.blocks {
		if b.windowID == windowID {
			n++
		}
	}
	return n == 1
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
	maps.DeleteFunc(w.showing, func(_ string, t tab) bool { return t == tab{sessionID, windowID} })
}

func (w *world) removeSession(sessionID string) {
	delete(w.sessions, sessionID)
	maps.DeleteFunc(w.showing, func(_ string, t tab) bool { return t.sessionID == sessionID })
}

func (w *world) setApps(apps []rex.App) {
	w.apps = map[string][]string{}
	for _, app := range apps {
		w.apps[app.ClientID] = app.SessionIDs
	}
	maps.DeleteFunc(w.showing, func(clientID string, _ tab) bool { return !w.isApp(clientID) })
	maps.DeleteFunc(w.heard, func(clientID string, _ bool) bool { return !w.isApp(clientID) })
}

// keepShowing carries over from before a scan what each app was showing,
// where that tab is still there.
func (w *world) keepShowing(before *world) {
	for clientID, t := range before.showing {
		if w.isApp(clientID) && w.hasWindow(t) {
			w.showing[clientID] = t
		}
	}
}

func (w *world) hasWindow(t tab) bool {
	s := w.sessions[t.sessionID]
	if s == nil {
		return false
	}
	for _, b := range s.blocks {
		if b.windowID == t.windowID {
			return true
		}
	}
	return false
}

// sized records that an app displayed the block's tab, and reports false
// for a block the watcher doesn't know.
func (w *world) sized(sessionID, blockID, clientID string) bool {
	b := w.block(sessionID, blockID)
	if b == nil {
		return false
	}
	w.showing[clientID] = tab{sessionID: sessionID, windowID: b.windowID}
	w.heard[clientID] = true
	return true
}

// knowsWhatAppsShow reports whether every connected app has displayed a
// tab since the last scan. Rex says what an app shows only when it next
// sizes a block, so a watcher that has just started or reconnected can't
// tell whether a tab is on screen.
func (w *world) knowsWhatAppsShow() bool {
	for clientID := range w.apps {
		if !w.heardFrom(clientID) {
			return false
		}
	}
	return true
}

func (w *world) heardFrom(clientID string) bool {
	return w.heard[clientID]
}

func (w *world) isApp(clientID string) bool {
	_, ok := w.apps[clientID]
	return ok
}

// shown reports whether a Rex app is showing the window. A session's
// active window can't say: Rex.app attaches to every session, and its tab
// changes don't move the server's active window.
func (w *world) shown(sessionID, windowID string) bool {
	for _, t := range w.showing {
		if t.sessionID == sessionID && t.windowID == windowID {
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
