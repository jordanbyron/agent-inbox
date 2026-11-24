package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Store reads and writes the files in one server's state directory.
type Store struct {
	dir string
}

// New returns a Store for the files in dir, which must already exist.
func New(dir string) *Store {
	return &Store{dir: dir}
}

// Read returns state.json as last written, taking no lock. A missing file
// is an empty state.
func (s *Store) Read() (*State, error) {
	path := s.path("state.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return emptyState(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var file stateFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if file.Version != Version {
		return nil, fmt.Errorf("reading %s: it is format version %d and this agent-inbox reads %d", path, file.Version, Version)
	}
	st := file.State
	st.makeMaps()
	return &st, nil
}

// Update applies fn to the latest state and writes the result, holding
// state.lock throughout so no other writer's edit is lost. If fn returns an
// error, nothing is written and Update returns that error.
func (s *Store) Update(fn func(*State) error) error {
	// The lock is a file of its own because state.json is replaced on every
	// write, and a lock on a file that has been renamed over guards nothing.
	lock, err := os.OpenFile(s.path("state.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("opening the state lock: %w", err)
	}
	// Closing drops the flock. Its error is dropped too, since by then the
	// write has succeeded or failed on its own.
	defer func() { _ = lock.Close() }()

	if err := flock(lock, unix.LOCK_EX); err != nil {
		return fmt.Errorf("locking %s: %w", lock.Name(), err)
	}
	st, err := s.Read()
	if err != nil {
		return err
	}
	if err := fn(st); err != nil {
		return err
	}
	st.makeMaps()
	return writeJSON(s.path("state.json"), stateFile{Version: Version, State: *st})
}

func (s *Store) path(name string) string {
	return filepath.Join(s.dir, name)
}

// writeJSON replaces path with v so that a reader sees the old file or the
// new one, never part of either.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	data = append(data, '\n')

	// The temp file is in the same directory because rename only replaces
	// a file atomically within one filesystem.
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := fill(tmp, data); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// fill writes data to f and syncs it before closing, so a crash after the
// rename can't leave an empty file in its place.
func fill(f *os.File, data []byte) error {
	_, err := f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func flock(f *os.File, how int) error {
	for {
		err := unix.Flock(int(f.Fd()), how)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}
