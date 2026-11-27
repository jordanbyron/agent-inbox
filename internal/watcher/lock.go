package watcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sys/unix"

	"github.com/jordanbyron/agent-inbox/internal/state"
)

// takeoverWait is how long a watcher waits for one of another version to
// let go of the lock after asking it to exit.
const takeoverWait = 5 * time.Second

const lockPoll = 50 * time.Millisecond

var errSameVersion = errors.New("a watcher of this version holds the lock")

// takeLock takes the watcher lock. A watcher of the same version holding it
// is errSameVersion; one of another version is sent SIGTERM, and the lock
// taken once it lets go.
func takeLock(ctx context.Context, store *state.Store, version string, log *slog.Logger) (*state.WatcherLock, error) {
	deadline := time.Now().Add(takeoverWait)
	asked := 0
	for {
		lock, err := store.TakeWatcherLock(version)
		held, ok := errors.AsType[*state.HeldError](err)
		if !ok {
			return lock, err
		}
		switch {
		case held.PID == 0:
			// It is still writing itself into the lock; ask again shortly.
		case held.Version == version:
			return nil, errSameVersion
		case held.PID != asked:
			log.Info("asking the watcher of another version to exit", "pid", held.PID, "version", held.Version)
			if err := unix.Kill(held.PID, unix.SIGTERM); err != nil && !errors.Is(err, unix.ESRCH) {
				return nil, fmt.Errorf("asking watcher pid %d to exit: %w", held.PID, err)
			}
			asked = held.PID
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("taking the watcher lock: %w", held)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockPoll):
		}
	}
}
