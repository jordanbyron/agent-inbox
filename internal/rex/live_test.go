package rex_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

// TestLive runs against the real Rex server, in a session of its own that
// no app window shows, and kills that session when it's done.
func TestLive(t *testing.T) {
	if os.Getenv("AGENT_INBOX_LIVE_REX") != "1" {
		t.Skip("set AGENT_INBOX_LIVE_REX=1 to run against the Rex server")
	}
	bin, err := rex.FindBinary(os.Getenv)
	require.NoError(t, err)
	client := rex.New(bin)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	server, err := client.Server(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, server.StateID())

	events, errs := client.Events(ctx)
	label := fmt.Sprintf("agent-inbox-test-live-%d", os.Getpid())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	// The pause gives `rex events` time to connect before the report.
	script := `sleep 2; printf '\033]7501;state=blocked:kind=permission:app=agent-inbox-live:msg=bGl2ZSB0ZXN0\033\\'; ` +
		`echo live-marker; exec sleep 600`
	started, err := client.Start(ctx, rex.StartOptions{
		SessionLabel: label,
		Command:      []string{"sh", "-c", script},
		Dir:          dir,
		BlockLabel:   "agent-inbox:live",
		WindowLabel:  "live",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		out, err := exec.Command(bin, "kill", label).CombinedOutput()
		if err != nil {
			t.Logf("rex kill %s: %v: %s", label, err, out)
		}
	})

	waitFor := func(match func(rex.Event) bool) rex.Event {
		t.Helper()
		for {
			select {
			case event, ok := <-events:
				if !ok {
					require.FailNow(t, "the stream ended", "%v", <-errs)
				}
				if event.SessionID == started.SessionID && match(event) {
					return event
				}
			case <-ctx.Done():
				require.FailNow(t, "timed out waiting for an event")
			}
		}
	}

	changed := waitFor(func(e rex.Event) bool {
		return e.Type == rex.EventStatusChanged && e.BlockID == started.BlockID
	})
	want := rex.StatusRecord{State: "blocked", Kind: "permission", App: "agent-inbox-live", EffectiveApp: "agent-inbox-live", Msg: "live test"}
	changed.Record.UpdatedAt = time.Time{}
	require.Equal(t, want, changed.Record)

	records, err := client.Status(ctx, started.SessionID, started.BlockID)
	require.NoError(t, err)
	require.Len(t, records, 1)
	records[0].UpdatedAt = time.Time{}
	require.Equal(t, want, records[0])

	view, err := client.View(ctx, started.SessionID)
	require.NoError(t, err)
	require.Equal(t, started.WindowID, view.ActiveWindowID)
	require.Equal(t, []rex.Window{{
		ID:             started.WindowID,
		Label:          "live",
		FocusedBlockID: started.BlockID,
		Blocks:         []rex.Block{{ID: started.BlockID, Label: "agent-inbox:live"}},
	}}, view.Windows)

	process, err := client.Process(ctx, started.SessionID, started.BlockID)
	require.NoError(t, err)
	require.True(t, process.Alive)
	require.Equal(t, dir, process.Dir)

	text, err := client.Capture(ctx, started.SessionID, started.BlockID)
	require.NoError(t, err)
	require.Contains(t, text, "live-marker")

	require.NoError(t, client.CloseBlock(ctx, started.SessionID, started.BlockID))
	waitFor(func(e rex.Event) bool { return e.Type == rex.EventBlockClosed && e.BlockID == started.BlockID })
	waitFor(func(e rex.Event) bool { return e.Type == rex.EventWindowClosed && e.WindowID == started.WindowID })

	_, err = client.Status(ctx, started.SessionID, started.BlockID)
	require.ErrorIs(t, err, rex.ErrNotFound)

	cancel()
	for range events {
	}
	require.ErrorIs(t, <-errs, context.Canceled)
}
