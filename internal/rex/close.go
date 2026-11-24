package rex

import "context"

// Closer closes blocks and windows. *Client has it.
type Closer interface {
	CloseBlock(ctx context.Context, sessionID, blockID string) error
	CloseWindow(ctx context.Context, sessionID, windowID string) error
}

// CloseAgent closes an agent's block. One alone in its window takes the
// window with it; one sharing a window with other panes closes by itself,
// so they stay.
func CloseAgent(ctx context.Context, c Closer, sessionID, windowID, blockID string, alone bool) error {
	if alone {
		return c.CloseWindow(ctx, sessionID, windowID)
	}
	return c.CloseBlock(ctx, sessionID, blockID)
}
