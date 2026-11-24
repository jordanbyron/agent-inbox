package watcher_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
	"github.com/jordanbyron/agent-inbox/internal/watcher"
)

func TestReconnectsWhenStreamEndsAndServerStays(t *testing.T) {
	h := newHarness(t)
	work := h.rex.AddSession("work")
	_, block := h.tab(work, "agent-inbox:a1")
	h.edit(func(st *state.State) { st.Records["a1"] = launched(work, "/r/app") })
	h.start()
	h.status(work, block, working)
	h.waitRow("a1", hasStatus(triage.StatusWorking))

	h.rex.GoAway()
	h.waitLog("reconnecting")
	// Lost with the stream, and found again by the scan after reconnecting.
	h.status(work, block, blocked)
	h.clock.Advance(time.Second)
	h.waitSection("a1", needsYou)

	h.status(work, block, idle)
	h.waitRow("a1", hasStatus(triage.StatusIdle))
}

func TestExitsWhenServerGoes(t *testing.T) {
	h := newHarness(t)
	h.rex.AddSession("work")
	h.start()

	h.rex.Shutdown()
	select {
	case err := <-h.done:
		require.NoError(t, err)
		h.done <- nil
	case <-time.After(waitFor):
		require.FailNow(t, "the watcher kept running without its server")
	}
	require.Contains(t, h.log.String(), "the Rex server is gone")
}

func TestHeartbeatRewritesUnchangedSnapshot(t *testing.T) {
	h := newHarness(t)
	work := h.rex.AddSession("work")
	_, block := h.tab(work, "agent-inbox:a1")
	h.edit(func(st *state.State) { st.Records["a1"] = launched(work, "/r/app") })
	require.NoError(t, h.rex.SetStatus(work, block, idle))
	h.start()
	first := h.waitSnapshot(func(s snapshot.Snapshot) bool { return len(s.Rows()) == 1 })
	require.Equal(t, "v-test", first.WatcherVersion)
	statePath := filepath.Join(h.dir, "state.json")
	before, err := os.ReadFile(statePath)
	require.NoError(t, err)
	written := fileInfo(t, statePath).ModTime()

	h.clock.Advance(watcher.HeartbeatEvery)
	require.Eventually(t, func() bool {
		_, at, ok := h.readSnapshot()
		return ok && at.Equal(t0.Add(watcher.HeartbeatEvery))
	}, waitFor, time.Millisecond)
	again, _, _ := h.readSnapshot()
	require.Equal(t, first, again)

	after, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.True(t, bytes.Equal(before, after))
	require.Equal(t, written, fileInfo(t, statePath).ModTime(), "nothing changed, so state.json was not written")
}
