package watcher

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

// scan reads the whole server afresh: its apps, and every session's blocks
// and their status records.
func (w *watcher) scan(ctx context.Context) error {
	sessions, err := w.rex.Sessions(ctx)
	if err != nil {
		return fmt.Errorf("listing Rex sessions: %w", err)
	}
	apps, err := w.rex.Apps(ctx)
	if err != nil {
		return fmt.Errorf("listing Rex apps: %w", err)
	}
	fresh := newWorld()
	fresh.setApps(apps)
	for _, s := range sessions {
		if err := w.scanSession(ctx, fresh, s.ID); err != nil {
			return err
		}
	}
	fresh.keepShowing(w.world)
	w.world = fresh
	return nil
}

// scanSession reads one session's view into wd, and the status of each
// block it hasn't seen before. A session or block that went away meanwhile
// is left out; its own events follow.
func (w *watcher) scanSession(ctx context.Context, wd *world, sessionID string) error {
	view, err := w.rex.View(ctx, sessionID)
	if errors.Is(err, rex.ErrNotFound) {
		wd.removeSession(sessionID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading Rex session %s: %w", sessionID, err)
	}
	for _, b := range wd.setView(view) {
		records, err := w.rex.Status(ctx, sessionID, b.id)
		if errors.Is(err, rex.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reading the status of block %s: %w", b.id, err)
		}
		b.records = records
	}
	return nil
}

// rescanAll reads every session again into the world as it stands, and
// reports whether it could.
func (w *watcher) rescanAll(ctx context.Context) bool {
	sessions, err := w.rex.Sessions(ctx)
	if err != nil {
		w.log.Warn("listing Rex sessions", "err", err)
		return false
	}
	for _, s := range sessions {
		if err := w.scanSession(ctx, w.world, s.ID); err != nil {
			w.log.Warn("rescanning a session", "err", err)
			return false
		}
	}
	return true
}

// rescan reads a session again after an event that leaves doubt about it.
func (w *watcher) rescan(ctx context.Context, sessionID string) {
	if err := w.scanSession(ctx, w.world, sessionID); err != nil {
		w.log.Warn("rescanning a session", "err", err)
	}
}

func (w *watcher) refreshApps(ctx context.Context) {
	apps, err := w.rex.Apps(ctx)
	if err != nil {
		w.log.Warn("listing Rex apps", "err", err)
		return
	}
	w.world.setApps(apps)
}

// handle applies one event to the world, and reports whether it may have
// changed anything. Every rex call, the watcher's own included, comes back
// as client events, which mostly change nothing.
func (w *watcher) handle(ctx context.Context, event rex.Event) bool {
	switch event.Type {
	case rex.EventStatusChanged:
		if b := w.world.block(event.SessionID, event.BlockID); b != nil {
			b.setRecord(event.Record)
		} else {
			w.rescan(ctx, event.SessionID)
		}
	case rex.EventStatusRemoved:
		if b := w.world.block(event.SessionID, event.BlockID); b != nil {
			b.removeRecords(event.RecordIDs)
		} else {
			w.rescan(ctx, event.SessionID)
		}
	case rex.EventBlockClosed:
		w.world.removeBlock(event.SessionID, event.BlockID)
	case rex.EventWindowClosed:
		w.world.removeWindow(event.SessionID, event.WindowID)
	case rex.EventBlockSized:
		if !w.world.isApp(event.ClientID) {
			return false
		}
		if w.world.block(event.SessionID, event.BlockID) == nil {
			w.rescan(ctx, event.SessionID)
		}
		first := !w.world.heardFrom(event.ClientID)
		if !w.world.sized(event.SessionID, event.BlockID, event.ClientID) {
			return false
		}
		if first {
			w.log.Info("heard which tab a Rex app shows", "app", event.ClientID)
		}
		return true
	case rex.EventSessionDestroyed:
		w.world.removeSession(event.SessionID)
	case rex.EventSessionCreated, rex.EventBlockCreated, rex.EventWindowCreated, rex.EventWindowLabelChanged:
		w.rescan(ctx, event.SessionID)
	case rex.EventViewChanged:
		// A block's label changing sends only this.
		if s := w.world.sessions[event.SessionID]; s != nil && event.Revision <= s.revision {
			return false
		}
		w.rescan(ctx, event.SessionID)
	case rex.EventClientJoined:
		if event.ClientKind != "app" {
			return false
		}
		w.refreshApps(ctx)
	case rex.EventClientLeft, rex.EventClientConnected, rex.EventClientDisconnected:
		if !w.world.isApp(event.ClientID) {
			return false
		}
		w.refreshApps(ctx)
	default:
		return false
	}
	return true
}

// fillDirs asks Rex where each found agent works, once per block. A
// launched agent's record already says.
func (w *watcher) fillDirs(ctx context.Context) {
	for _, b := range w.world.blocks() {
		if b.dirKnown {
			continue
		}
		id, ok := identify(b.id, b.label, b.records, w.st.Records, w.cfg.Recipes)
		if !ok || id.launched {
			continue
		}
		b.dirKnown = true
		process, err := w.rex.Process(ctx, b.sessionID, b.id)
		if err != nil {
			w.log.Warn("reading where a block works", "block", b.id, "err", err)
			continue
		}
		b.dir = process.Dir
	}
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
