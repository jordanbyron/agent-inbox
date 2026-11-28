package watcher_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/snapshot"
)

// fakeReleases answers with tag, or err, and counts the asks.
type fakeReleases struct {
	mu    sync.Mutex
	tag   string
	err   error
	asked int
}

func (r *fakeReleases) Latest(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked++
	return r.tag, r.err
}

func (r *fakeReleases) set(tag string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tag, r.err = tag, err
}

func (r *fakeReleases) asks() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.asked
}

func TestTheSnapshotCarriesTheLatestRelease(t *testing.T) {
	h := newHarness(t)
	releases := &fakeReleases{tag: "v0.3.0"}
	h.releases = releases
	h.start()

	t.Run("it asks at the start", func(t *testing.T) {
		h.waitSnapshot(func(s snapshot.Snapshot) bool { return s.Latest == "v0.3.0" })
		require.Equal(t, 1, releases.asks())
	})

	t.Run("and not again within the hour", func(t *testing.T) {
		releases.set("v0.4.0", nil)
		h.clock.Advance(30 * time.Minute)
		h.heartbeat()
		require.Equal(t, 1, releases.asks())
	})

	t.Run("a failed ask keeps the last release", func(t *testing.T) {
		releases.set("", errors.New("403 Forbidden"))
		h.clock.Advance(31 * time.Minute)
		h.waitLog("asking for the latest release")
		h.heartbeat()
		snap, _, _ := h.readSnapshot()
		require.Equal(t, "v0.3.0", snap.Latest)
	})

	t.Run("the next hour brings the new one", func(*testing.T) {
		releases.set("v0.4.0", nil)
		h.clock.Advance(time.Hour)
		h.waitSnapshot(func(s snapshot.Snapshot) bool { return s.Latest == "v0.4.0" })
	})
}
