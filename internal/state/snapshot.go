package state

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type snapshotFile struct {
	// WrittenAt is the heartbeat. It sits in the file rather than coming
	// from the mtime so that a reader gets the age and the body from the
	// same read, and a stat can never pair a fresh time with older rows.
	WrittenAt int64           `json:"written_at"`
	Body      json.RawMessage `json:"body"`
}

// WriteSnapshot replaces snapshot.json with body, stamped with now as its
// heartbeat.
func (s *Store) WriteSnapshot(body any, now time.Time) error {
	path := s.path("snapshot.json")
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	return writeJSON(path, snapshotFile{WrittenAt: now.Unix(), Body: data})
}

// ReadSnapshot decodes the last snapshot's body into v and returns when it
// was written. With no snapshot yet, the error wraps fs.ErrNotExist.
func (s *Store) ReadSnapshot(v any) (time.Time, error) {
	return ReadSnapshotFile(s.path("snapshot.json"), v)
}

// ReadSnapshotFile is ReadSnapshot for a file at any path, such as a fixture.
func ReadSnapshotFile(path string, v any) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var file snapshotFile
	if err := json.Unmarshal(data, &file); err != nil {
		return time.Time{}, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(file.Body, v); err != nil {
		return time.Time{}, fmt.Errorf("reading %s: %w", path, err)
	}
	return time.Unix(file.WrittenAt, 0), nil
}
