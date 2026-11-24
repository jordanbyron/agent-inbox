package rextest

import (
	"context"
	"sync"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

// stream queues without limit, so a test can script many changes before it
// reads any of them without the Fake blocking.
type stream struct {
	mu     sync.Mutex
	queue  []rex.Event
	reason error
	wake   chan struct{}
	events chan rex.Event
	errs   chan error
}

func newStream(ctx context.Context) *stream {
	st := &stream{
		wake:   make(chan struct{}, 1),
		events: make(chan rex.Event),
		errs:   make(chan error, 1),
	}
	go st.pump(ctx)
	return st
}

func (st *stream) push(event rex.Event) {
	st.mu.Lock()
	st.queue = append(st.queue, event)
	st.mu.Unlock()
	st.signal()
}

func (st *stream) end(reason error) {
	st.mu.Lock()
	st.reason = reason
	st.mu.Unlock()
	st.signal()
}

func (st *stream) signal() {
	select {
	case st.wake <- struct{}{}:
	default:
	}
}

func (st *stream) pump(ctx context.Context) {
	finish := func(err error) {
		st.errs <- err
		close(st.errs)
		close(st.events)
	}
	for {
		st.mu.Lock()
		queue, reason := st.queue, st.reason
		st.queue = nil
		st.mu.Unlock()
		for _, event := range queue {
			select {
			case st.events <- event:
			case <-ctx.Done():
				finish(ctx.Err())
				return
			}
		}
		if len(queue) > 0 {
			continue
		}
		if reason != nil {
			finish(reason)
			return
		}
		select {
		case <-st.wake:
		case <-ctx.Done():
			finish(ctx.Err())
			return
		}
	}
}
