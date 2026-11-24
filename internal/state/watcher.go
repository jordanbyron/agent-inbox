package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// WatcherLock is watcher.lock, held by the running watcher until it closes
// the lock or exits.
type WatcherLock struct {
	file *os.File
}

// HeldError is the refusal to take the watcher lock, naming the watcher
// that holds it.
type HeldError struct {
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

func (e *HeldError) Error() string {
	if e.PID == 0 {
		return "the watcher lock is held by a watcher still starting"
	}
	return fmt.Sprintf("the watcher lock is held by pid %d, version %s", e.PID, e.Version)
}

// TakeWatcherLock takes watcher.lock without waiting and writes this
// process's pid and the given version into it. If another process holds the
// lock, the error is a *HeldError.
func (s *Store) TakeWatcherLock(version string) (*WatcherLock, error) {
	// Not truncated on open: until the flock is ours, what is in the file
	// belongs to whoever holds it.
	file, err := os.OpenFile(s.path("watcher.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening the watcher lock: %w", err)
	}
	err = flock(file, unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		held := readHolder(file)
		_ = file.Close()
		return nil, held
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("locking %s: %w", file.Name(), err)
	}
	if err := writeHolder(file, HeldError{PID: os.Getpid(), Version: version}); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("writing %s: %w", file.Name(), err)
	}
	return &WatcherLock{file: file}, nil
}

// Close releases the watcher lock.
func (l *WatcherLock) Close() error {
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("releasing the watcher lock: %w", err)
	}
	return nil
}

func writeHolder(file *os.File, holder HeldError) error {
	data, err := json.Marshal(holder)
	if err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	_, err = file.WriteAt(data, 0)
	return err
}

// readHolder returns a HeldError with no pid when the holder has not
// written itself in yet, since it takes the lock before it writes.
func readHolder(file *os.File) *HeldError {
	var held HeldError
	data, err := io.ReadAll(file)
	if err != nil || json.Unmarshal(data, &held) != nil {
		return &HeldError{}
	}
	return &held
}
