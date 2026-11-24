package rextest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/rex/rextest"
)

// rexAPI is every method of rex.Client, so the build fails when the fake
// falls behind it.
type rexAPI interface {
	Server(ctx context.Context) (rex.Server, error)
	Sessions(ctx context.Context) ([]rex.Session, error)
	View(ctx context.Context, sessionID string) (rex.View, error)
	Status(ctx context.Context, sessionID, blockID string) ([]rex.StatusRecord, error)
	Process(ctx context.Context, sessionID, blockID string) (rex.Process, error)
	Apps(ctx context.Context) ([]rex.App, error)
	ShownWindows(ctx context.Context) ([]rex.ShownWindow, error)
	Start(ctx context.Context, opts rex.StartOptions) (rex.Started, error)
	CloseBlock(ctx context.Context, sessionID, blockID string) error
	CloseWindow(ctx context.Context, sessionID, windowID string) error
	Show(ctx context.Context, sessionID, blockID string) error
	Capture(ctx context.Context, sessionID, blockID string) (string, error)
	Events(ctx context.Context) (<-chan rex.Event, <-chan error)
}

var (
	_ rexAPI = (*rex.Client)(nil)
	_ rexAPI = (*rextest.Fake)(nil)
)

func next(t *testing.T, events <-chan rex.Event) rex.Event {
	t.Helper()
	select {
	case event, ok := <-events:
		require.True(t, ok, "the stream ended")
		return event
	case <-time.After(time.Second):
		require.FailNow(t, "no event arrived")
		return rex.Event{}
	}
}

func TestFakeScriptsAView(t *testing.T) {
	fake := rextest.New(rex.Server{HostID: "host"})
	session := fake.AddSession("work")
	first, err := fake.AddWindow(session, "editor")
	require.NoError(t, err)
	second, err := fake.AddWindow(session, "agent")
	require.NoError(t, err)
	block, err := fake.AddBlock(session, second, "agent-inbox:a1")
	require.NoError(t, err)

	view, err := fake.View(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, first, view.ActiveWindowID)
	require.Len(t, view.Windows, 2)
	require.Equal(t, "agent", view.Windows[1].Label)
	require.Equal(t, []rex.Block{{ID: block, Label: "agent-inbox:a1"}}, view.Windows[1].Blocks)

	server, err := fake.Server(t.Context())
	require.NoError(t, err)
	require.Equal(t, "host", server.StateID())

	_, err = fake.View(t.Context(), "session:missing")
	require.ErrorIs(t, err, rex.ErrNotFound)
}

func TestFakeEvents(t *testing.T) {
	fake := rextest.New(rex.Server{})
	session := fake.AddSession("work")
	window, err := fake.AddWindow(session, "agent")
	require.NoError(t, err)
	other, err := fake.AddWindow(session, "editor")
	require.NoError(t, err)
	block, err := fake.AddBlock(session, window, "agent-inbox:a1")
	require.NoError(t, err)
	events, errs := fake.Events(t.Context())

	blocked := rex.StatusRecord{State: "blocked", Kind: "permission", App: "claude-code"}
	require.NoError(t, fake.SetStatus(session, block, blocked))
	require.NoError(t, fake.SetActiveWindow(session, other))
	require.NoError(t, fake.RemoveStatus(session, block, "child_exited", ""))
	require.NoError(t, fake.ExitBlock(session, block, 0))

	require.Equal(t, rex.Event{Type: rex.EventStatusChanged, SessionID: session, BlockID: block, Record: blocked, Reason: "report"}, next(t, events))
	require.Equal(t, rex.Event{Type: rex.EventActiveWindowChanged, SessionID: session, WindowID: other}, next(t, events))
	require.Equal(t, rex.EventViewChanged, next(t, events).Type)
	require.Equal(t, rex.Event{Type: rex.EventStatusRemoved, SessionID: session, BlockID: block, RecordIDs: []string{""}, Reason: "child_exited"}, next(t, events))
	require.Equal(t, rex.Event{Type: rex.EventProgramExited, SessionID: session, BlockID: block}, next(t, events))
	require.Equal(t, rex.Event{Type: rex.EventBlockClosed, SessionID: session, BlockID: block}, next(t, events))
	require.Equal(t, rex.Event{Type: rex.EventWindowClosed, SessionID: session, WindowID: window}, next(t, events))

	records, err := fake.Status(t.Context(), session, block)
	require.ErrorIs(t, err, rex.ErrNotFound)
	require.Empty(t, records)

	fake.GoAway()
	for range events {
	}
	require.ErrorIs(t, <-errs, rex.ErrStreamEnded)
}

func TestFakeEventsEndWithTheContext(t *testing.T) {
	fake := rextest.New(rex.Server{})
	ctx, cancel := context.WithCancel(t.Context())
	events, errs := fake.Events(ctx)
	fake.AddSession("work")

	cancel()
	require.ErrorIs(t, <-errs, context.Canceled)
	for range events {
	}
}

func TestFakeRecordsRequests(t *testing.T) {
	fake := rextest.New(rex.Server{})
	work := fake.AddSession("work")
	app := fake.AddApp("studio", work)
	opts := rex.StartOptions{
		SessionID:   work,
		Command:     []string{"claude"},
		Dir:         "/tmp/repo",
		BlockLabel:  "agent-inbox:a1",
		WindowLabel: "fix login",
	}

	started, err := fake.Start(t.Context(), opts)
	require.NoError(t, err)
	require.Equal(t, []rex.StartOptions{opts}, fake.Started())
	process, err := fake.Process(t.Context(), work, started.BlockID)
	require.NoError(t, err)
	require.Equal(t, rex.Process{Alive: true, Dir: "/tmp/repo"}, process)

	fresh, err := fake.Start(t.Context(), rex.StartOptions{SessionLabel: "agents", Focus: true})
	require.NoError(t, err)
	require.NotEqual(t, work, fresh.SessionID)

	fake.SetCapture(started.BlockID, "https://github.com/example/repo/pull/12\n")
	text, err := fake.Capture(t.Context(), work, started.BlockID)
	require.NoError(t, err)
	require.Equal(t, "https://github.com/example/repo/pull/12\n", text)

	require.NoError(t, fake.Show(t.Context(), work, started.BlockID))
	shown, err := fake.ShownWindows(t.Context())
	require.NoError(t, err)
	require.Equal(t, []rex.ShownWindow{{ClientID: app, SessionID: work, WindowID: started.WindowID}}, shown)

	fake.SetRemoteControl(false)
	err = fake.Show(t.Context(), work, started.BlockID)
	require.ErrorIs(t, err, rex.ErrRemoteControlOff)
	require.ErrorContains(t, err, "Turn on Remote Control")
	require.Equal(t, []string{started.BlockID, started.BlockID}, fake.Shown())

	require.NoError(t, fake.CloseBlock(t.Context(), work, started.BlockID))
	require.Equal(t, []string{started.BlockID}, fake.ClosedBlocks())
	require.NoError(t, fake.CloseWindow(t.Context(), fresh.SessionID, fresh.WindowID))
	require.Equal(t, []string{fresh.WindowID}, fake.ClosedWindows())

	view, err := fake.View(t.Context(), work)
	require.NoError(t, err)
	require.Empty(t, view.Windows)
	require.Empty(t, view.ActiveWindowID)
}

func TestFakeRemoveSession(t *testing.T) {
	fake := rextest.New(rex.Server{})
	work := fake.AddSession("work")
	window, err := fake.AddWindow(work, "agent")
	require.NoError(t, err)
	block, err := fake.AddBlock(work, window, "")
	require.NoError(t, err)
	events, _ := fake.Events(t.Context())

	require.NoError(t, fake.RemoveSession(work))
	require.Equal(t, rex.Event{Type: rex.EventBlockClosed, SessionID: work, BlockID: block}, next(t, events))
	require.Equal(t, rex.Event{Type: rex.EventSessionDestroyed, SessionID: work}, next(t, events))
	sessions, err := fake.Sessions(t.Context())
	require.NoError(t, err)
	require.Empty(t, sessions)
}
