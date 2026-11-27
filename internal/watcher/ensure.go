package watcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
)

// startWait is how long EnsureRunning waits for a watcher it started to
// write its first snapshot.
const startWait = 5 * time.Second

// Launch says where EnsureRunning looks for the watcher and how it starts
// one.
type Launch struct {
	StateDir string
	// Version is this binary's; a watcher of another version is replaced.
	Version string
	// Command runs the watcher in the foreground: this binary and "watch".
	Command []string
}

// EnsureRunning makes sure a watcher of this version is writing the
// snapshot. If the snapshot is fresh, of this version, and its watcher is
// still alive, it does nothing; otherwise it starts the watcher, detached
// from the caller, and waits briefly for a fresh snapshot from it.
func EnsureRunning(ctx context.Context, l Launch) error {
	store := state.New(l.StateDir)
	if running(store, l.Version) {
		return nil
	}
	if err := start(l); err != nil {
		return err
	}
	deadline := time.Now().Add(startWait)
	for !running(store, l.Version) {
		if time.Now().After(deadline) {
			return fmt.Errorf("the watcher did not start; see %s", LogPath(l.StateDir))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(lockPoll):
		}
	}
	return nil
}

// running reports whether the snapshot is fresh and of this version, and
// the watcher in the lock is still alive.
func running(store *state.Store, version string) bool {
	var snap snapshot.Snapshot
	writtenAt, err := store.ReadSnapshot(&snap)
	fresh := err == nil && snap.WatcherVersion == version && time.Since(writtenAt) < FreshFor
	return fresh && holderAlive(store)
}

// holderAlive reports whether the watcher written into the lock is still
// running. A watcher that just exited leaves a snapshot that looks fresh
// for a while yet.
func holderAlive(store *state.Store) bool {
	held, err := store.WatcherHolder()
	if err != nil || held.PID <= 0 {
		return false
	}
	// One this process started and that has exited is a zombie until it is
	// reaped, and a zombie still answers kill(pid, 0).
	var status unix.WaitStatus
	if reaped, err := unix.Wait4(held.PID, &status, unix.WNOHANG, nil); err == nil && reaped == held.PID {
		return false
	}
	err = unix.Kill(held.PID, 0)
	return err == nil || errors.Is(err, unix.EPERM)
}

// start runs the watcher in a session of its own, so it outlives the
// command that started it and no terminal's signals reach it.
func start(l Launch) error {
	if len(l.Command) == 0 {
		return errors.New("starting the watcher: no command")
	}
	logFile, err := os.OpenFile(LogPath(l.StateDir), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("starting the watcher: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	cmd := exec.Command(l.Command[0], l.Command[1:]...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the watcher: %w", err)
	}
	// Nothing waits for it: it is the watcher's own business when it exits.
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("starting the watcher: %w", err)
	}
	return nil
}
