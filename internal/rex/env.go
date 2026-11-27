package rex

import (
	"context"
	"fmt"
)

// Pane is the Rex block a command was run from.
type Pane struct {
	SessionID string
	BlockID   string
}

// PaneFromEnv reads the pane from the variables Rex sets in a block, and
// reports false outside Rex. SessionID is empty when REX_SESSION is unset,
// which it is in panes from Rex 0.1.0-preview.23 on; FindPane fills it in.
func PaneFromEnv(getenv func(string) string) (Pane, bool) {
	pane := Pane{SessionID: getenv("REX_SESSION"), BlockID: getenv("REX_BLOCK")}
	if pane.BlockID == "" {
		return Pane{}, false
	}
	return pane, true
}

// Viewer is the part of Client FindPane uses.
type Viewer interface {
	View(ctx context.Context, sessionID string) (View, error)
}

// FindPane is PaneFromEnv with the session always known. Rex takes a block
// id wherever it takes a session, so viewing the block names its session.
func FindPane(ctx context.Context, v Viewer, getenv func(string) string) (Pane, bool, error) {
	pane, ok := PaneFromEnv(getenv)
	if !ok || pane.SessionID != "" {
		return pane, ok, nil
	}
	view, err := v.View(ctx, pane.BlockID)
	if err != nil {
		return Pane{}, false, fmt.Errorf("finding the Rex session of %s: %w", pane.BlockID, err)
	}
	pane.SessionID = view.SessionID
	return pane, true, nil
}
