package recipe

import (
	"context"
	"crypto/rand"
	"fmt"
	"path/filepath"
	"time"
)

// SessionFinder learns the session id a harness chose for itself.
type SessionFinder interface {
	// FindSessionID returns the id of the newest conversation in q.Dir that
	// began at or after q.Began, and false when there is none.
	FindSessionID(ctx context.Context, q SessionQuery) (string, bool, error)
}

// SessionQuery says whose session to look for: the agent's directory and
// when it began, and the home directory that holds the harness's files.
type SessionQuery struct {
	Home  string
	Dir   string
	Began time.Time
}

// NewSessionID returns a random UUID for a harness that takes its session
// id at start.
func NewSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails.
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// sameDir compares directories the way a harness may have recorded them,
// with symlinks resolved (macOS's /tmp is /private/tmp).
func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}
