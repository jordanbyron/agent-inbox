// Package rextest holds an in-memory Rex server for other packages' tests.
package rextest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

// RemoteControlRefusal is the text Rex.app answers with while Remote Control
// is off.
const RemoteControlRefusal = "Rex.app is not accepting actions. Turn on Remote Control in its Settings under Rex Server."

// ErrDown is what every call returns after Shutdown.
var ErrDown = errors.New("rextest: the server is not running")

// Fake has the same methods as rex.Client, backed by sessions, windows and
// blocks a test adds. Changes made through either kind of method come out
// of every stream Events returned, as the real server's events would.
type Fake struct {
	mu            sync.Mutex
	server        rex.Server
	sessions      []*session
	apps          []rex.App
	remoteOff     bool
	down          bool
	captures      map[string]string
	lastID        int
	streams       []*stream
	started       []rex.StartOptions
	closedBlocks  []string
	closedWindows []string
	shown         []string
}

type session struct {
	id       string
	label    string
	active   string
	revision int
	windows  []*window
}

type window struct {
	id     string
	label  string
	blocks []*block
}

type block struct {
	id      string
	label   string
	dir     string
	records []rex.StatusRecord
}

// New returns an empty Fake whose server has the given identity.
func New(server rex.Server) *Fake {
	return &Fake{server: server, captures: map[string]string{}}
}

// AddSession adds a session and returns its id.
func (f *Fake) AddSession(label string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addSession(label).id
}

// AddWindow adds a window to a session and returns its id. A session's first
// window becomes its active one.
func (f *Fake) AddWindow(sessionID, label string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.session(sessionID)
	if err != nil {
		return "", err
	}
	return f.addWindow(s, label).id, nil
}

// AddBlock adds a block to a window and returns its id.
func (f *Fake) AddBlock(sessionID, windowID, label string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.session(sessionID)
	if err != nil {
		return "", err
	}
	w, err := s.window(windowID)
	if err != nil {
		return "", err
	}
	return f.addBlock(s, w, label, "").id, nil
}

// SetStatus replaces the block's record with the same id, as an OSC 7501
// report would.
func (f *Fake) SetStatus(sessionID, blockID string, record rex.StatusRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, _, b, err := f.block(sessionID, blockID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(b.records, func(r rex.StatusRecord) bool { return r.ID == record.ID })
	if i < 0 {
		b.records = append(b.records, record)
	} else {
		b.records[i] = record
	}
	f.emit(rex.Event{Type: rex.EventStatusChanged, SessionID: sessionID, BlockID: blockID, Record: record, Reason: "report"})
	return nil
}

// RemoveStatus removes the block's records with the given ids, the root
// being "", for a reason such as "child_exited".
func (f *Fake) RemoveStatus(sessionID, blockID, reason string, ids ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, _, b, err := f.block(sessionID, blockID)
	if err != nil {
		return err
	}
	b.records = slices.DeleteFunc(b.records, func(r rex.StatusRecord) bool { return slices.Contains(ids, r.ID) })
	f.emit(rex.Event{Type: rex.EventStatusRemoved, SessionID: sessionID, BlockID: blockID, RecordIDs: ids, Reason: reason})
	return nil
}

// ExitBlock ends a block's program, which closes the block as it would in
// Rex, without counting as a CloseBlock request.
func (f *Fake) ExitBlock(sessionID, blockID string, exitCode int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, w, b, err := f.block(sessionID, blockID)
	if err != nil {
		return err
	}
	f.emit(rex.Event{Type: rex.EventProgramExited, SessionID: sessionID, BlockID: blockID, ExitCode: exitCode})
	f.removeBlock(s, w, b)
	return nil
}

// SetActiveWindow makes a window its session's active one, as a click on
// its tab would.
func (f *Fake) SetActiveWindow(sessionID, windowID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.session(sessionID)
	if err != nil {
		return err
	}
	if _, err := s.window(windowID); err != nil {
		return err
	}
	f.activate(s, windowID)
	return nil
}

// RemoveSession destroys a session and closes its blocks.
func (f *Fake) RemoveSession(sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.session(sessionID)
	if err != nil {
		return err
	}
	for _, w := range s.windows {
		for _, b := range w.blocks {
			f.emit(rex.Event{Type: rex.EventBlockClosed, SessionID: s.id, BlockID: b.id})
		}
	}
	f.sessions = slices.DeleteFunc(f.sessions, func(other *session) bool { return other == s })
	f.emit(rex.Event{Type: rex.EventSessionDestroyed, SessionID: s.id})
	return nil
}

// AddApp connects a Rex app attached to the given sessions and returns its
// client id.
func (f *Fake) AddApp(label string, sessionIDs ...string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	app := rex.App{ClientID: f.newID("client"), Label: label, SessionIDs: sessionIDs}
	f.apps = append(f.apps, app)
	f.emit(rex.Event{Type: rex.EventClientJoined, ClientID: app.ClientID, ClientKind: "app"})
	for _, sessionID := range sessionIDs {
		f.emit(rex.Event{Type: rex.EventClientConnected, SessionID: sessionID, ClientID: app.ClientID})
	}
	return app.ClientID
}

// RemoveApp disconnects an app AddApp connected.
func (f *Fake) RemoveApp(clientID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.apps, func(app rex.App) bool { return app.ClientID == clientID })
	if i < 0 {
		return
	}
	app := f.apps[i]
	f.apps = slices.Delete(f.apps, i, i+1)
	for _, sessionID := range app.SessionIDs {
		f.emit(rex.Event{Type: rex.EventClientDisconnected, SessionID: sessionID, ClientID: clientID})
	}
	f.emit(rex.Event{Type: rex.EventClientLeft, ClientID: clientID})
}

// SetRemoteControl turns the apps' Remote Control setting on or off. While
// it is off, Show fails with an error matching rex.ErrRemoteControlOff.
func (f *Fake) SetRemoteControl(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.remoteOff = !on
}

// SetCapture sets what Capture returns for a block.
func (f *Fake) SetCapture(blockID, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.captures[blockID] = text
}

// GoAway ends every open stream with rex.ErrStreamEnded, as the server
// going away would.
func (f *Fake) GoAway() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, st := range f.streams {
		st.end(rex.ErrStreamEnded)
	}
	f.streams = nil
}

// Shutdown stops the server: every open stream ends as GoAway ends it, and
// every call after fails as one to a server that is not running would.
func (f *Fake) Shutdown() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = true
	for _, st := range f.streams {
		st.end(rex.ErrStreamEnded)
	}
	f.streams = nil
}

// Started returns the options of every Start call, in order.
func (f *Fake) Started() []rex.StartOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.started)
}

// ClosedBlocks returns the blocks CloseBlock was asked to close, in order.
func (f *Fake) ClosedBlocks() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.closedBlocks)
}

// ClosedWindows returns the windows CloseWindow was asked to close, in order.
func (f *Fake) ClosedWindows() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.closedWindows)
}

// Shown returns the blocks Show was asked to show, in order.
func (f *Fake) Shown() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.shown)
}

// Server returns the identity New was given.
func (f *Fake) Server(ctx context.Context) (rex.Server, error) {
	if err := f.reachable(ctx); err != nil {
		return rex.Server{}, err
	}
	return f.server, nil
}

// Sessions lists the sessions in the order they were added.
func (f *Fake) Sessions(ctx context.Context) ([]rex.Session, error) {
	if err := f.reachable(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	sessions := make([]rex.Session, 0, len(f.sessions))
	for _, s := range f.sessions {
		sessions = append(sessions, rex.Session{ID: s.id, Label: s.label})
	}
	return sessions, nil
}

// View returns a session's windows and blocks.
func (f *Fake) View(ctx context.Context, sessionID string) (rex.View, error) {
	if err := f.reachable(ctx); err != nil {
		return rex.View{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.session(sessionID)
	if err != nil {
		return rex.View{}, err
	}
	view := rex.View{SessionID: s.id, Label: s.label, ActiveWindowID: s.active, Revision: s.revision}
	for _, w := range s.windows {
		window := rex.Window{ID: w.id, Label: w.label}
		for _, b := range w.blocks {
			window.Blocks = append(window.Blocks, rex.Block{ID: b.id, Label: b.label})
		}
		if len(window.Blocks) > 0 {
			window.FocusedBlockID = window.Blocks[0].ID
		}
		view.Windows = append(view.Windows, window)
	}
	return view, nil
}

// Status returns a block's records in the order they were first set.
func (f *Fake) Status(ctx context.Context, sessionID, blockID string) ([]rex.StatusRecord, error) {
	if err := f.reachable(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	_, _, b, err := f.block(sessionID, blockID)
	if err != nil {
		return nil, err
	}
	return slices.Clone(b.records), nil
}

// Process reports every block as alive, since a block closes when its
// program exits; Dir is the one Start was given, if any.
func (f *Fake) Process(ctx context.Context, sessionID, blockID string) (rex.Process, error) {
	if err := f.reachable(ctx); err != nil {
		return rex.Process{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	_, _, b, err := f.block(sessionID, blockID)
	if err != nil {
		return rex.Process{}, err
	}
	return rex.Process{Alive: true, Dir: b.dir}, nil
}

// Apps lists the apps AddApp connected.
func (f *Fake) Apps(ctx context.Context) ([]rex.App, error) {
	if err := f.reachable(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.apps), nil
}

// ShownWindows returns each app's session's active window.
func (f *Fake) ShownWindows(ctx context.Context) ([]rex.ShownWindow, error) {
	if err := f.reachable(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var shown []rex.ShownWindow
	for _, app := range f.apps {
		for _, sessionID := range app.SessionIDs {
			s, err := f.session(sessionID)
			if err != nil || s.active == "" {
				continue
			}
			shown = append(shown, rex.ShownWindow{ClientID: app.ClientID, SessionID: sessionID, WindowID: s.active})
		}
	}
	return shown, nil
}

// Start records opts and adds the window and block they describe.
func (f *Fake) Start(ctx context.Context, opts rex.StartOptions) (rex.Started, error) {
	if err := f.reachable(ctx); err != nil {
		return rex.Started{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var s *session
	if opts.SessionID == "" {
		s = f.addSession(opts.SessionLabel)
	} else {
		var err error
		if s, err = f.session(opts.SessionID); err != nil {
			return rex.Started{}, err
		}
	}
	f.started = append(f.started, opts)
	w := f.addWindow(s, opts.WindowLabel)
	b := f.addBlock(s, w, opts.BlockLabel, opts.Dir)
	if opts.Focus {
		f.activate(s, w.id)
	}
	return rex.Started{SessionID: s.id, WindowID: w.id, BlockID: b.id}, nil
}

// CloseBlock records the request and closes the block.
func (f *Fake) CloseBlock(ctx context.Context, sessionID, blockID string) error {
	if err := f.reachable(ctx); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, w, b, err := f.block(sessionID, blockID)
	if err != nil {
		return err
	}
	f.closedBlocks = append(f.closedBlocks, blockID)
	f.removeBlock(s, w, b)
	return nil
}

// CloseWindow records the request and closes the window and its blocks.
func (f *Fake) CloseWindow(ctx context.Context, sessionID, windowID string) error {
	if err := f.reachable(ctx); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.session(sessionID)
	if err != nil {
		return err
	}
	w, err := s.window(windowID)
	if err != nil {
		return err
	}
	f.closedWindows = append(f.closedWindows, windowID)
	f.removeWindow(s, w)
	return nil
}

// Show records the request and makes the block's window active. With Remote
// Control off and an app attached to the session, it then fails as Rex does.
func (f *Fake) Show(ctx context.Context, sessionID, blockID string) error {
	if err := f.reachable(ctx); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, w, _, err := f.block(sessionID, blockID)
	if err != nil {
		return err
	}
	f.shown = append(f.shown, blockID)
	f.activate(s, w.id)
	for _, app := range f.apps {
		if f.remoteOff && slices.Contains(app.SessionIDs, sessionID) {
			return &rex.Error{Command: "client.perform", Code: "refused", Message: RemoteControlRefusal}
		}
	}
	return nil
}

// Capture returns what SetCapture set for the block.
func (f *Fake) Capture(ctx context.Context, sessionID, blockID string) (string, error) {
	if err := f.reachable(ctx); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, _, _, err := f.block(sessionID, blockID); err != nil {
		return "", err
	}
	return f.captures[blockID], nil
}

// Events returns a stream of every change from now on, which ends when ctx
// is canceled or GoAway or Shutdown is called, as rex.Client's does.
func (f *Fake) Events(ctx context.Context) (<-chan rex.Event, <-chan error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := newStream(ctx)
	if f.down {
		st.end(rex.ErrStreamEnded)
		return st.events, st.errs
	}
	f.streams = append(f.streams, st)
	return st.events, st.errs
}

// reachable is the error a call gets before it reaches the server.
func (f *Fake) reachable(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return ErrDown
	}
	return nil
}

func (f *Fake) emit(event rex.Event) {
	for _, st := range f.streams {
		st.push(event)
	}
}

func (f *Fake) newID(kind string) string {
	f.lastID++
	return fmt.Sprintf("%s:%d", kind, f.lastID)
}

func (f *Fake) addSession(label string) *session {
	s := &session{id: f.newID("session"), label: label}
	if s.label == "" {
		s.label = s.id
	}
	f.sessions = append(f.sessions, s)
	f.emit(rex.Event{Type: rex.EventSessionCreated, SessionID: s.id, Label: s.label})
	return s
}

func (f *Fake) addWindow(s *session, label string) *window {
	w := &window{id: f.newID("window"), label: label}
	if w.label == "" {
		w.label = w.id
	}
	s.windows = append(s.windows, w)
	f.emit(rex.Event{Type: rex.EventWindowCreated, SessionID: s.id, WindowID: w.id, Label: w.label})
	if s.active == "" {
		f.activate(s, w.id)
	}
	f.viewChanged(s)
	return w
}

func (f *Fake) addBlock(s *session, w *window, label, dir string) *block {
	b := &block{id: f.newID("block"), label: label, dir: dir}
	w.blocks = append(w.blocks, b)
	f.emit(rex.Event{Type: rex.EventBlockCreated, SessionID: s.id, BlockID: b.id})
	f.viewChanged(s)
	return b
}

func (f *Fake) removeBlock(s *session, w *window, b *block) {
	w.blocks = slices.DeleteFunc(w.blocks, func(other *block) bool { return other == b })
	f.emit(rex.Event{Type: rex.EventBlockClosed, SessionID: s.id, BlockID: b.id})
	if len(w.blocks) == 0 {
		f.removeWindow(s, w)
		return
	}
	f.viewChanged(s)
}

// removeWindow follows Rex's order for session.close_window: the window's
// close comes before its blocks'.
func (f *Fake) removeWindow(s *session, w *window) {
	i := slices.Index(s.windows, w)
	s.windows = slices.Delete(s.windows, i, i+1)
	f.emit(rex.Event{Type: rex.EventWindowClosed, SessionID: s.id, WindowID: w.id})
	for _, b := range w.blocks {
		f.emit(rex.Event{Type: rex.EventBlockClosed, SessionID: s.id, BlockID: b.id})
	}
	if s.active == w.id {
		next := ""
		if len(s.windows) > 0 {
			next = s.windows[min(i, len(s.windows)-1)].id
		}
		f.activate(s, next)
	}
	f.viewChanged(s)
}

func (f *Fake) activate(s *session, windowID string) {
	if s.active == windowID {
		return
	}
	s.active = windowID
	if windowID != "" {
		f.emit(rex.Event{Type: rex.EventActiveWindowChanged, SessionID: s.id, WindowID: windowID})
	}
	f.viewChanged(s)
}

func (f *Fake) viewChanged(s *session) {
	s.revision++
	f.emit(rex.Event{Type: rex.EventViewChanged, SessionID: s.id, Revision: s.revision})
}

func (f *Fake) session(id string) (*session, error) {
	for _, s := range f.sessions {
		if s.id == id || s.label == id {
			return s, nil
		}
	}
	return nil, notFound("session", id)
}

func (s *session) window(id string) (*window, error) {
	for _, w := range s.windows {
		if w.id == id {
			return w, nil
		}
	}
	return nil, notFound("window", id)
}

func (f *Fake) block(sessionID, blockID string) (*session, *window, *block, error) {
	s, err := f.session(sessionID)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, w := range s.windows {
		for _, b := range w.blocks {
			if b.id == blockID {
				return s, w, b, nil
			}
		}
	}
	return nil, nil, nil, notFound("block", blockID)
}

func notFound(kind, id string) error {
	return &rex.Error{Command: "fake", Code: "not_found", Message: kind + " " + id + " not found"}
}
