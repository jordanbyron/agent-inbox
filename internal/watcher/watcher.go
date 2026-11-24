// Package watcher is the one process per Rex server that observes Rex,
// applies the triage rules, finds pull requests and writes the snapshot
// every view reads.
package watcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/reap"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

// HeartbeatEvery is how often the snapshot is rewritten when nothing changed.
const HeartbeatEvery = 5 * time.Second

// FreshFor is how old a snapshot can be and still count as current. A view
// that finds it older starts the watcher again, as list.StaleAfter says.
const FreshFor = 15 * time.Second

const (
	// tickEvery is how often the clock alone can change something: a
	// snooze waking, a heartbeat, a refresh coming due.
	tickEvery = time.Second
	// branchScanEvery is how often a launched agent's worktree is checked
	// for new branches between turns.
	branchScanEvery = 2 * time.Minute
	reapEvery       = 10 * time.Minute
)

// Options is what Run needs from outside.
type Options struct {
	Rex   Rex
	GH    pulls.Runner
	Git   worktree.Git
	Clock Clock
	// StateDir is the Rex server's state directory, from state.Dir.
	StateDir string
	Config   config.Config
	// Version is this binary's, written into the lock and the snapshot.
	Version string
	// Log is where the watcher says what it did; nil discards it.
	Log *slog.Logger
	// Notifier sends what Config.Notify asks for; nil sends nothing.
	Notifier Notifier
	// Deleter deletes what Config.Reap reaps; nil reaps nothing.
	Deleter reap.Deleter
}

// Run takes the watcher lock and watches Rex until ctx ends or the Rex
// server goes away, and then returns nil. It returns nil at once when a
// watcher of the same version holds the lock, and asks one of another
// version to exit and takes over from it.
func Run(ctx context.Context, opts Options) error {
	if opts.Log == nil {
		opts.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	store := state.New(opts.StateDir)
	lock, err := takeLock(ctx, store, opts.Version, opts.Log)
	if errors.Is(err, errSameVersion) {
		opts.Log.Info("another watcher of this version is running", "version", opts.Version)
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	opts.Log.Info("watching", "version", opts.Version, "dir", opts.StateDir)
	w, err := newWatcher(opts, store)
	if err != nil {
		return fmt.Errorf("starting the watcher: %w", err)
	}
	return w.run(ctx)
}
