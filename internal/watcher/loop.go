package watcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/notify"
	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/reap"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

// watcher is one run of the watcher. The goroutine in run owns every field;
// the finder's goroutine shares nothing with it and talks only over the
// jobs and findings channels.
type watcher struct {
	rex     Rex
	store   *state.Store
	dir     string
	clock   Clock
	cfg     config.Config
	version string
	log     *slog.Logger
	finder  *finder

	world  *world
	st     *state.State
	agents []agent
	rec    recorder
	// statuses are the live agents' statuses at the last observation, to
	// tell when one finishes a turn.
	statuses map[string]triage.Status
	// found waits to be merged into the state at the next observation.
	found []finding
	// queue waits for the finder, which takes one job at a time.
	queue      []job
	refreshing bool
	lastScan   time.Time
	// unclosable are blocks Rex refused to close, not asked again.
	unclosable map[string]bool

	lastBody []byte
	wroteAt  time.Time

	notifier Notifier
	// moments are the families at the last observation, nil before the first.
	moments map[string]moment
	notes   chan notify.Notification

	reaper   *reap.Reaper
	lastReap time.Time
}

func newWatcher(opts Options, store *state.Store) (*watcher, error) {
	st, err := store.Read()
	if err != nil {
		return nil, err
	}
	w := &watcher{
		rex:     opts.Rex,
		store:   store,
		dir:     opts.StateDir,
		clock:   opts.Clock,
		cfg:     opts.Config,
		version: opts.Version,
		log:     opts.Log,
		finder: &finder{
			rex:       opts.Rex,
			gh:        opts.GH,
			git:       opts.Git,
			refresher: pulls.NewRefresher(opts.GH),
			log:       opts.Log,
		},
		world:      newWorld(),
		st:         st,
		statuses:   map[string]triage.Status{},
		unclosable: map[string]bool{},
		notes:      make(chan notify.Notification, pendingNotifications),
	}
	if opts.Config.Notify {
		w.notifier = opts.Notifier
	}
	if opts.Config.Reap && opts.Deleter != nil {
		w.reaper = &reap.Reaper{Deleter: opts.Deleter}
	}
	return w, nil
}

// run is the loop. It returns once ctx ends or the Rex server goes away,
// after the goroutines it started have stopped.
func (w *watcher) run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	jobs := make(chan job)
	findings := make(chan finding)
	var wg sync.WaitGroup
	wg.Go(func() { w.finder.work(ctx, jobs, findings) })
	if w.notifier != nil {
		wg.Go(func() { w.sendNotifications(ctx, w.notifier) })
	}

	events, ended := w.rex.Events(ctx)
	defer func() {
		cancel()
		wg.Wait()
		drain(events)
	}()

	if err := w.scan(ctx); err != nil {
		return err
	}
	var fsEvents <-chan fsnotify.Event
	var fsErrors <-chan error
	if fs := w.watchState(); fs != nil {
		defer func() { _ = fs.Close() }()
		fsEvents, fsErrors = fs.Events, fs.Errors
	}
	ticker := w.clock.NewTicker(tickEvery)
	defer ticker.Stop()

	w.settle(ctx)
	for {
		var send chan<- job
		var next job
		if len(w.queue) > 0 {
			send, next = jobs, w.queue[0]
		}
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-events:
			if !ok {
				events = nil
				if !w.serverUp(ctx, <-ended) {
					return nil
				}
				continue
			}
			if !w.handle(ctx, event) {
				continue
			}
		case event, ok := <-fsEvents:
			if !ok {
				fsEvents = nil
			}
			if !ok || !isStateFile(event) {
				continue
			}
		case err, ok := <-fsErrors:
			if !ok {
				fsErrors = nil
			} else {
				w.log.Warn("watching the state directory", "err", err)
			}
			continue
		case now := <-ticker.C():
			if events == nil {
				events, ended = w.reconnect(ctx)
			}
			w.onTick(now)
		case found := <-findings:
			w.found = append(w.found, found)
			if found.key == "" {
				w.refreshing = false
			}
		case send <- next:
			w.queue = w.queue[1:]
			if _, ok := next.(refreshJob); ok {
				w.refreshing = true
			}
			continue
		}
		w.settle(ctx)
	}
}

// drain waits for the event stream's goroutine to finish. A nil stream is
// one that already ended.
func drain(events <-chan rex.Event) {
	if events == nil {
		return
	}
	for range events {
	}
}

// serverUp reports whether the server is still there after its event
// stream ended. If it is, the next tick reconnects.
func (w *watcher) serverUp(ctx context.Context, reason error) bool {
	if ctx.Err() != nil {
		return false
	}
	if _, err := w.rex.Server(ctx); err != nil {
		w.log.Info("the Rex server is gone", "reason", reason, "err", err)
		return false
	}
	w.log.Warn("the Rex event stream ended; reconnecting", "reason", reason)
	return true
}

// reconnect follows the events again, and scans everything, since what
// happened while the stream was down is lost.
func (w *watcher) reconnect(ctx context.Context) (<-chan rex.Event, <-chan error) {
	events, ended := w.rex.Events(ctx)
	if err := w.scan(ctx); err != nil {
		w.log.Warn("scanning Rex after reconnecting", "err", err)
	}
	return events, ended
}

// onTick queues the work the clock brings due. The observation that
// follows every tick is what wakes snoozes and writes the heartbeat.
func (w *watcher) onTick(now time.Time) {
	if now.Sub(w.lastScan) >= branchScanEvery {
		w.lastScan = now
		for _, a := range w.agents {
			if a.live() && a.launched && a.record.Worktree != "" {
				w.enqueue(branchJob{key: a.key, worktree: a.record.Worktree})
			}
		}
	}
	if prs := unresolved(w.st); !w.refreshing && len(prs) > 0 {
		w.enqueue(refreshJob{prs: prs, now: now})
	}
}

// unresolved is every remembered pull request gh may yet have news of.
func unresolved(st *state.State) []state.PullRequest {
	var out []state.PullRequest
	seen := map[string]bool{}
	for _, key := range sortedKeys(st.PullRequests) {
		for _, pr := range st.PullRequests[key] {
			if pr.ResolvedAt.IsZero() && !seen[pr.URL] {
				seen[pr.URL] = true
				out = append(out, pr)
			}
		}
	}
	return out
}

func (w *watcher) enqueue(j job) {
	for _, queued := range w.queue {
		if queued.id() == j.id() {
			return
		}
	}
	w.queue = append(w.queue, j)
}

// settle brings the state, the tabs and the snapshot up to date with what
// the watcher has just learned. It reads the clock once, so the state and
// the snapshot it writes agree about whether a snooze has run out.
func (w *watcher) settle(ctx context.Context) {
	now := w.clock.Now()
	w.fillDirs(ctx)
	w.record(now)
	if w.autoSettle(ctx, now) {
		w.record(now)
	}
	if w.reapDue(ctx, now) {
		w.record(now)
	}
	if w.notifier != nil {
		w.notifyChanges(now)
	}
	w.publish(now)
}

// record writes one observation of every agent in a single update.
func (w *watcher) record(now time.Time) {
	found := w.found
	var agents []agent
	var obs observation
	err := w.update(func(st *state.State) {
		agents = collectAgents(w.world, st, w.cfg.Recipes)
		obs = w.rec.observe(st, agents, found, now)
	})
	if err != nil {
		w.log.Error("recording what Rex shows", "err", err)
		return
	}
	w.found = nil
	w.rec.commit(obs)
	w.agents = agents
	w.queueTurnEnds()
}

var errUnchanged = errors.New("nothing changed")

// update applies change to the latest state, and writes it only if change
// changed something.
func (w *watcher) update(change func(*state.State)) error {
	err := w.store.Update(func(st *state.State) error {
		before, err := json.Marshal(st)
		if err != nil {
			return err
		}
		change(st)
		w.st = st
		after, err := json.Marshal(st)
		if err != nil {
			return err
		}
		if bytes.Equal(before, after) {
			return errUnchanged
		}
		return nil
	})
	if errors.Is(err, errUnchanged) {
		return nil
	}
	return err
}

// queueTurnEnds looks for pull requests for each agent that has just
// finished a turn, or was first seen between turns.
func (w *watcher) queueTurnEnds() {
	statuses := map[string]triage.Status{}
	for _, a := range w.agents {
		if !a.live() {
			continue
		}
		now := a.status.status
		statuses[a.key] = now
		prev, had := w.statuses[a.key]
		between := now != triage.StatusWorking && now != triage.StatusStarting
		if between && (!had || prev == triage.StatusWorking || prev == triage.StatusStarting) {
			w.enqueue(findJob(a))
		}
	}
	w.statuses = statuses
}

// findJob looks in a launched agent's worktree, which knows its branches
// exactly, and on any other agent's screen.
func findJob(a agent) job {
	if a.launched && a.record.Worktree != "" {
		return branchJob{key: a.key, worktree: a.record.Worktree}
	}
	return captureJob{key: a.key, sessionID: a.sessionID, blockID: a.block.id}
}

// autoSettle closes the blocks of every family whose work has landed, and
// reports whether it closed any. The observation after it settles them.
func (w *watcher) autoSettle(ctx context.Context, now time.Time) bool {
	closed := false
	for _, c := range toAutoSettle(w.agents, w.st, now) {
		if w.unclosable[c.blockID] {
			continue
		}
		alone := w.world.alone(c.sessionID, c.windowID)
		if err := rex.CloseAgent(ctx, w.rex, c.sessionID, c.windowID, c.blockID, alone); err != nil {
			w.log.Error("closing a tab", "agent", c.key, "window", c.windowID, "block", c.blockID, "err", err)
			w.unclosable[c.blockID] = true
			continue
		}
		w.log.Info("closed a tab", "agent", c.key, "session", c.sessionID, "window", c.windowID,
			"block", c.blockID, "whole_tab", alone, "reason", c.reason)
		closed = true
		if alone {
			w.world.removeWindow(c.sessionID, c.windowID)
		} else {
			w.world.removeBlock(c.sessionID, c.blockID)
		}
	}
	return closed
}

// publish writes the snapshot when it changed or the heartbeat is due.
func (w *watcher) publish(now time.Time) {
	snap := snapshot.New(w.version, rows(w.agents, w.st, now, w.cfg.FoldWorking), snapshot.Sort(w.cfg.Sort))
	body, err := json.Marshal(snap)
	if err != nil {
		w.log.Error("encoding the snapshot", "err", err)
		return
	}
	if bytes.Equal(body, w.lastBody) && now.Sub(w.wroteAt) < HeartbeatEvery {
		return
	}
	if err := w.store.WriteSnapshot(snap, now); err != nil {
		w.log.Error("writing the snapshot", "err", err)
		return
	}
	w.lastBody, w.wroteAt = body, now
}

// watchState watches the state directory, so the loop hears at once when
// another command replaces state.json. It watches the directory rather
// than the file because every writer renames a new file over the old one.
// With no watch, the tick's observation has to do.
func (w *watcher) watchState() *fsnotify.Watcher {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		w.log.Warn("watching the state directory; relying on the tick", "err", err)
		return nil
	}
	if err := fs.Add(w.dir); err != nil {
		_ = fs.Close()
		w.log.Warn("watching the state directory; relying on the tick", "err", err)
		return nil
	}
	return fs
}

func isStateFile(event fsnotify.Event) bool {
	return filepath.Base(event.Name) == "state.json"
}

func (w *watcher) reapDue(ctx context.Context, now time.Time) bool {
	if w.reaper == nil || now.Sub(w.lastReap) < reapEvery {
		return false
	}
	w.lastReap = now
	reaped := false
	for _, r := range w.reaper.Sweep(ctx, w.st, now) {
		days := int(now.Sub(r.LastSettledAt).Hours() / 24)
		if r.Err != nil {
			w.log.Warn("kept a family reap is due to delete; trying again in a day",
				"agent", r.Key, "name", r.Name, "settled_days", days, "err", r.Err)
			continue
		}
		w.log.Info("reaped a family", "agent", r.Key, "name", r.Name, "members", r.Members, "settled_days", days)
		reaped = true
	}
	return reaped
}
