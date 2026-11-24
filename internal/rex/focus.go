package rex

import (
	"context"
	"errors"
)

// FocusHolder is the part of *Client that HoldFocus uses.
type FocusHolder interface {
	View(ctx context.Context, sessionID string) (View, error)
	Events(ctx context.Context) (<-chan Event, <-chan error)
	FocusBlock(ctx context.Context, sessionID, blockID string) error
}

// HoldFocus keeps target focused when Rex.app returns focus to from as a
// palette closes. It watches until ctx expires; if focus moves back to from
// while target is still open and not minimized, it focuses target once and
// returns.
func HoldFocus(ctx context.Context, r FocusHolder, sessionID, from, target string) error {
	if sessionID == "" || from == "" || target == "" || from == target {
		return nil
	}
	watchCtx, cancel := context.WithCancel(ctx)
	events, errs := r.Events(watchCtx)
	defer func() {
		cancel()
		for range events {
		}
		for range errs {
		}
	}()

	if done, err := checkFocus(watchCtx, r, sessionID, from, target); done || err != nil {
		return ignoreCancel(ctx, err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-events:
			if !ok {
				return ignoreCancel(ctx, <-errs)
			}
			if event.SessionID != sessionID {
				continue
			}
			switch event.Type {
			case EventSessionDestroyed:
				return nil
			case EventBlockClosed:
				if event.BlockID == target {
					return nil
				}
			case EventViewChanged:
				if done, err := checkFocus(watchCtx, r, sessionID, from, target); done || err != nil {
					return ignoreCancel(ctx, err)
				}
			}
		}
	}
}

func checkFocus(ctx context.Context, r FocusHolder, sessionID, from, target string) (bool, error) {
	view, err := r.View(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	for _, w := range view.Windows {
		for _, b := range w.Blocks {
			if b.ID != target {
				continue
			}
			if b.Minimized {
				return true, nil
			}
			switch w.FocusedBlockID {
			case target:
				return false, nil
			case from:
				err := r.FocusBlock(ctx, sessionID, target)
				if errors.Is(err, ErrNotFound) {
					return true, nil
				}
				return true, err
			default:
				return true, nil
			}
		}
	}
	return true, nil
}

func ignoreCancel(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}
