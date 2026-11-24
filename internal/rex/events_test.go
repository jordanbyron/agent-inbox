package rex_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

func TestEventsDecodesWhatRexPrints(t *testing.T) {
	client, calls := fakeRex(t, reply{match: "events --all --json", file: "events.jsonl"})

	events, errs := client.Events(t.Context())
	var got []rex.Event
	for event := range events {
		got = append(got, event)
	}
	require.ErrorIs(t, <-errs, rex.ErrStreamEnded)
	require.Equal(t, []string{"events --all --json"}, calls())

	const (
		session = "session:01a20b37-aed8-73d8-b215-20b7f9590d3d"
		zsh     = "block:01a20b3b-0301-7dbb-845d-56db8872cc23"
		zshWin  = "window:01a20b3b-0318-78cd-b949-47051542b614"
		exited  = "block:01a20b3b-2b97-760e-9498-60b94c3948d3"
		agent   = "block:01a20b3e-5241-75e7-b413-68522863a41c"
		other   = "session:01a20b3e-a2e5-7530-95f5-3a2db6a9092d"
		cleared = "block:01a20b3a-8425-7d23-ae32-728b6ca45f51"
	)
	for i := range got {
		got[i].Record.UpdatedAt = time.Time{}
	}
	forty := 40
	require.Equal(t, []rex.Event{
		{Type: rex.EventBlockCreated, SessionID: session, BlockID: zsh},
		{Type: rex.EventWindowCreated, SessionID: session, WindowID: zshWin, Label: "zsh"},
		{Type: rex.EventViewChanged, SessionID: session, Revision: 16},
		{Type: rex.EventProgramExited, SessionID: session, BlockID: exited, ExitCode: 3},
		{Type: rex.EventBlockClosed, SessionID: session, BlockID: exited},
		{Type: rex.EventWindowClosed, SessionID: session, WindowID: "window:01a20b3b-2baf-7849-886b-a3bc4fdd3945"},
		{Type: rex.EventSessionLabelChanged, SessionID: session, Label: "agent-inbox-test-a2"},
		{Type: rex.EventActiveWindowChanged, SessionID: session, WindowID: "window:01a20b3e-5257-7975-ab27-6e9bf86256f9"},
		{
			Type: rex.EventStatusChanged, SessionID: session, BlockID: agent, Reason: "report",
			Record: rex.StatusRecord{
				State: "blocked", Kind: "permission", App: "claude-code", EffectiveApp: "claude-code", Msg: "approve Bash: ls",
			},
		},
		{
			Type: rex.EventStatusChanged, SessionID: session, BlockID: agent, Reason: "report",
			Record: rex.StatusRecord{
				ID: "task1", State: "working", EffectiveApp: "claude-code", Title: "Run tests", Progress: &forty,
			},
		},
		{Type: rex.EventSessionCreated, SessionID: other, Label: "agent-inbox-test-d"},
		{Type: rex.EventSessionDestroyed, SessionID: other},
		{Type: rex.EventStatusRemoved, SessionID: session, BlockID: cleared, RecordIDs: []string{"a/b"}, Reason: "clear"},
		{Type: rex.EventStatusRemoved, SessionID: session, BlockID: cleared, RecordIDs: []string{""}, Reason: "child_exited"},
		{Type: rex.EventWindowLabelChanged, SessionID: session, WindowID: "window:01a20b38-bf42-7b37-8118-6f6d149d54c7", Label: "probe-win"},
	}, got)
}

func TestEventsEnd(t *testing.T) {
	t.Run("rex events failing is the stream ending, with Rex's reason", func(t *testing.T) {
		client, _ := fakeRex(t, reply{match: "events", stderr: "Error: connection reset", exit: 1})

		events, errs := client.Events(t.Context())
		_, open := <-events
		require.False(t, open)
		err := <-errs
		require.ErrorIs(t, err, rex.ErrStreamEnded)
		require.ErrorContains(t, err, "connection reset")
	})

	t.Run("canceling ends an idle stream with the context's error", func(t *testing.T) {
		client, _ := fakeRex(t, reply{match: "events", exec: "sleep 30"})
		ctx, cancel := context.WithCancel(t.Context())

		events, errs := client.Events(ctx)
		select {
		case <-events:
			t.Fatal("an idle stream delivered something")
		case <-time.After(100 * time.Millisecond):
		}
		cancel()
		require.ErrorIs(t, <-errs, context.Canceled)
		_, open := <-events
		require.False(t, open)
	})

	t.Run("a line that isn't JSON ends the stream", func(t *testing.T) {
		client, _ := fakeRex(t, reply{match: "events", file: "capture.txt", exec: "sleep 30"})

		events, errs := client.Events(t.Context())
		for range events {
		}
		require.ErrorContains(t, <-errs, "decoding a rex event")
	})
}
