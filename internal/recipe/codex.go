package recipe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CodexSessions finds a Codex session in the rollout files under
// ~/.codex/sessions/YYYY/MM/DD.
type CodexSessions struct{}

type rolloutMeta struct {
	Payload struct {
		ID        string    `json:"id"`
		Cwd       string    `json:"cwd"`
		Timestamp time.Time `json:"timestamp"`
	} `json:"payload"`
}

// FindSessionID returns the newest rollout in q.Dir created at or after
// q.Began.
func (CodexSessions) FindSessionID(_ context.Context, q SessionQuery) (string, bool, error) {
	root := filepath.Join(q.Home, ".codex", "sessions")
	days, err := filepath.Glob(filepath.Join(root, "*", "*", "*"))
	if err != nil {
		return "", false, fmt.Errorf("listing Codex sessions: %w", err)
	}
	var newest rolloutMeta
	for _, day := range days {
		if !mayHoldSince(root, day, q.Began) {
			continue
		}
		rollouts, err := filepath.Glob(filepath.Join(day, "rollout-*.jsonl"))
		if err != nil {
			return "", false, fmt.Errorf("listing Codex sessions: %w", err)
		}
		for _, path := range rollouts {
			meta, ok := readRolloutMeta(path)
			created := meta.Payload.Timestamp
			if !ok || created.Before(q.Began) || !sameDir(meta.Payload.Cwd, q.Dir) {
				continue
			}
			if created.After(newest.Payload.Timestamp) {
				newest = meta
			}
		}
	}
	return newest.Payload.ID, newest.Payload.ID != "", nil
}

// mayHoldSince reports whether a day directory can hold a rollout from
// began on. Codex names the directories for the local date, so a day of
// slack covers a time zone change.
func mayHoldSince(root, day string, began time.Time) bool {
	rel, err := filepath.Rel(root, day)
	if err != nil {
		return false
	}
	date, err := time.ParseInLocation("2006/01/02", filepath.ToSlash(rel), time.Local)
	if err != nil {
		return false
	}
	return !date.AddDate(0, 0, 2).Before(began)
}

// readRolloutMeta reads a rollout's first line. A file that cannot be read
// is skipped rather than failing the lookup, since Codex may be writing it.
func readRolloutMeta(path string) (rolloutMeta, bool) {
	f, err := os.Open(path)
	if err != nil {
		return rolloutMeta{}, false
	}
	defer func() { _ = f.Close() }()
	var meta rolloutMeta
	if err := json.NewDecoder(f).Decode(&meta); err != nil {
		return rolloutMeta{}, false
	}
	return meta, meta.Payload.ID != ""
}
