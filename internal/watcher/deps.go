package watcher

import (
	"context"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

// Rex is what the watcher uses of the Rex server. *rex.Client and
// *rextest.Fake both have it.
type Rex interface {
	Server(ctx context.Context) (rex.Server, error)
	Sessions(ctx context.Context) ([]rex.Session, error)
	View(ctx context.Context, sessionID string) (rex.View, error)
	Status(ctx context.Context, sessionID, blockID string) ([]rex.StatusRecord, error)
	Process(ctx context.Context, sessionID, blockID string) (rex.Process, error)
	Apps(ctx context.Context) ([]rex.App, error)
	CloseBlock(ctx context.Context, sessionID, blockID string) error
	CloseWindow(ctx context.Context, sessionID, windowID string) error
	SetWindowLabel(ctx context.Context, sessionID, windowID, label string) error
	Capture(ctx context.Context, sessionID, blockID string) (string, error)
	Events(ctx context.Context) (<-chan rex.Event, <-chan error)
}

// Clock is where the watcher gets the time, so a test can move it by hand.
type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) Ticker
}

// Ticker delivers the time on C every so often until it is stopped.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// SystemClock is the real clock.
type SystemClock struct{}

// Now returns the current time.
func (SystemClock) Now() time.Time {
	return time.Now()
}

// NewTicker returns a time.Ticker that fires every d.
func (SystemClock) NewTicker(d time.Duration) Ticker {
	return systemTicker{time.NewTicker(d)}
}

type systemTicker struct {
	t *time.Ticker
}

func (s systemTicker) C() <-chan time.Time { return s.t.C }
func (s systemTicker) Stop()               { s.t.Stop() }
