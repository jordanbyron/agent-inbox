package rex_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/rex/rextest"
)

// watchingFake signals once HoldFocus has subscribed to events and read the
// initial view, so tests can trigger changes after that without sleeping.
type watchingFake struct {
	*rextest.Fake
	once  sync.Once
	ready chan struct{}
}

func newWatchingFake(f *rextest.Fake) *watchingFake {
	return &watchingFake{Fake: f, ready: make(chan struct{})}
}

func (w *watchingFake) View(ctx context.Context, sessionID string) (rex.View, error) {
	view, err := w.Fake.View(ctx, sessionID)
	w.once.Do(func() { close(w.ready) })
	return view, err
}

func TestHoldFocus(t *testing.T) {
	t.Run("refocuses target when focus bounces back to from", func(t *testing.T) {
		fake := rextest.New(rex.Server{})
		session := fake.AddSession("work")
		window, err := fake.AddWindow(session, "tab")
		require.NoError(t, err)
		from, err := fake.AddBlock(session, window, "shell")
		require.NoError(t, err)
		target, err := fake.AddBlock(session, window, "agent-inbox-modal")
		require.NoError(t, err)
		require.NoError(t, fake.SetFocusedBlock(session, target))

		wf := newWatchingFake(fake)
		done := make(chan error, 1)
		go func() {
			done <- rex.HoldFocus(t.Context(), wf, session, from, target)
		}()
		<-wf.ready

		require.NoError(t, fake.SetFocusedBlock(session, from))
		require.NoError(t, <-done)
		require.Equal(t, []string{target}, fake.FocusedBlocks())

		view, err := fake.View(t.Context(), session)
		require.NoError(t, err)
		require.Equal(t, target, view.Windows[0].FocusedBlockID)
	})

	t.Run("refocuses immediately when the bounce happened before watching started", func(t *testing.T) {
		fake := rextest.New(rex.Server{})
		session := fake.AddSession("work")
		window, err := fake.AddWindow(session, "tab")
		require.NoError(t, err)
		from, err := fake.AddBlock(session, window, "shell")
		require.NoError(t, err)
		target, err := fake.AddBlock(session, window, "agent-inbox-modal")
		require.NoError(t, err)
		require.NoError(t, fake.SetFocusedBlock(session, from))

		require.NoError(t, rex.HoldFocus(t.Context(), fake, session, from, target))
		require.Equal(t, []string{target}, fake.FocusedBlocks())
	})

	t.Run("does nothing when focus stays on target until timeout", func(t *testing.T) {
		fake := rextest.New(rex.Server{})
		session := fake.AddSession("work")
		window, err := fake.AddWindow(session, "tab")
		require.NoError(t, err)
		from, err := fake.AddBlock(session, window, "shell")
		require.NoError(t, err)
		target, err := fake.AddBlock(session, window, "agent-inbox-modal")
		require.NoError(t, err)
		require.NoError(t, fake.SetFocusedBlock(session, target))

		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		require.NoError(t, rex.HoldFocus(ctx, fake, session, from, target))
		require.Empty(t, fake.FocusedBlocks())
	})

	t.Run("stops without refocusing when target closes", func(t *testing.T) {
		fake := rextest.New(rex.Server{})
		session := fake.AddSession("work")
		window, err := fake.AddWindow(session, "tab")
		require.NoError(t, err)
		from, err := fake.AddBlock(session, window, "shell")
		require.NoError(t, err)
		target, err := fake.AddBlock(session, window, "agent-inbox-modal")
		require.NoError(t, err)
		require.NoError(t, fake.SetFocusedBlock(session, target))

		wf := newWatchingFake(fake)
		done := make(chan error, 1)
		go func() {
			done <- rex.HoldFocus(t.Context(), wf, session, from, target)
		}()
		<-wf.ready

		require.NoError(t, fake.CloseBlock(t.Context(), session, target))
		require.NoError(t, <-done)
		require.Empty(t, fake.FocusedBlocks())
	})

	t.Run("stops without refocusing when target is minimized", func(t *testing.T) {
		fake := rextest.New(rex.Server{})
		session := fake.AddSession("work")
		window, err := fake.AddWindow(session, "tab")
		require.NoError(t, err)
		from, err := fake.AddBlock(session, window, "shell")
		require.NoError(t, err)
		target, err := fake.AddBlock(session, window, "agent-inbox-modal")
		require.NoError(t, err)
		require.NoError(t, fake.SetMinimized(session, target, true))
		require.NoError(t, fake.SetFocusedBlock(session, from))

		require.NoError(t, rex.HoldFocus(t.Context(), fake, session, from, target))
		require.Empty(t, fake.FocusedBlocks())
	})

	t.Run("stops without refocusing when focus moves to a third block", func(t *testing.T) {
		fake := rextest.New(rex.Server{})
		session := fake.AddSession("work")
		window, err := fake.AddWindow(session, "tab")
		require.NoError(t, err)
		from, err := fake.AddBlock(session, window, "shell")
		require.NoError(t, err)
		target, err := fake.AddBlock(session, window, "agent-inbox-modal")
		require.NoError(t, err)
		other, err := fake.AddBlock(session, window, "other")
		require.NoError(t, err)
		require.NoError(t, fake.SetFocusedBlock(session, target))

		wf := newWatchingFake(fake)
		done := make(chan error, 1)
		go func() {
			done <- rex.HoldFocus(t.Context(), wf, session, from, target)
		}()
		<-wf.ready

		require.NoError(t, fake.SetFocusedBlock(session, other))
		require.NoError(t, <-done)
		require.Empty(t, fake.FocusedBlocks())
	})

	t.Run("stops when the session is destroyed", func(t *testing.T) {
		fake := rextest.New(rex.Server{})
		session := fake.AddSession("work")
		window, err := fake.AddWindow(session, "tab")
		require.NoError(t, err)
		from, err := fake.AddBlock(session, window, "shell")
		require.NoError(t, err)
		target, err := fake.AddBlock(session, window, "agent-inbox-modal")
		require.NoError(t, err)
		require.NoError(t, fake.SetFocusedBlock(session, target))

		wf := newWatchingFake(fake)
		done := make(chan error, 1)
		go func() {
			done <- rex.HoldFocus(t.Context(), wf, session, from, target)
		}()
		<-wf.ready

		require.NoError(t, fake.RemoveSession(session))
		require.NoError(t, <-done)
		require.Empty(t, fake.FocusedBlocks())
	})

	t.Run("returns immediately when from and target are the same", func(t *testing.T) {
		fake := rextest.New(rex.Server{})
		fake.Shutdown()
		require.NoError(t, rex.HoldFocus(t.Context(), fake, "session:1", "block:1", "block:1"))
	})
}
