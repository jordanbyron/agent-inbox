package watcher_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
)

const claudeSession = "0e5f3c2a-7d41-4c1b-9a8e-2f6b1d0c9e73"

func (h *harness) claudeTitles(titles ...string) {
	h.t.Helper()
	dir := filepath.Join(h.home, ".claude", "projects", "-work")
	require.NoError(h.t, os.MkdirAll(dir, 0o755))
	body := ""
	for _, title := range titles {
		body += `{"type":"ai-title","aiTitle":"` + title + `","sessionId":"` + claudeSession + `"}` + "\n"
	}
	require.NoError(h.t, os.WriteFile(filepath.Join(dir, claudeSession+".jsonl"), []byte(body), 0o600))
}

func (h *harness) windowLabel(session, window string) string {
	h.t.Helper()
	view, err := h.rex.View(h.t.Context(), session)
	require.NoError(h.t, err)
	for _, w := range view.Windows {
		if w.ID == window {
			return w.Label
		}
	}
	return ""
}

func labeled(label string) func(snapshot.Row) bool {
	return func(r snapshot.Row) bool { return r.Label == label }
}

func TestALaunchedAgentTakesClaudesTitle(t *testing.T) {
	h := newHarness(t)
	work := h.rex.AddSession("work")
	tab, err := h.rex.AddWindow(work, "fix-login")
	require.NoError(t, err)
	_, err = h.rex.AddBlock(work, tab, "agent-inbox:a1")
	require.NoError(t, err)
	h.edit(func(st *state.State) {
		rec := launched(work, t.TempDir())
		rec.SessionID = claudeSession
		st.Records["a1"] = rec
	})
	h.start()
	h.waitRow("a1", labeled("fix-login"))

	h.claudeTitles("Clone the repo", "Fix the login redirect loop")
	h.clock.Advance(30 * time.Second)
	h.waitRow("a1", labeled("Fix the login redirect loop"))
	require.Equal(t, "Fix the login redirect loop", h.state().Records["a1"].Title)
	require.Eventually(t, func() bool { return h.windowLabel(work, tab) == "Fix the login redirect loop" }, waitFor, time.Millisecond)

	// A tab renamed by hand keeps its name; the row still follows Claude.
	require.NoError(t, h.rex.SetWindowLabel(t.Context(), work, tab, "mine"))
	h.claudeTitles("Fix the Safari login redirect")
	h.clock.Advance(30 * time.Second)
	h.waitRow("a1", labeled("Fix the Safari login redirect"))
	require.Equal(t, "mine", h.windowLabel(work, tab))
}
