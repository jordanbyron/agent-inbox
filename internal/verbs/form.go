package verbs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

// formBounds centers the form's layer over most of the window.
var formBounds = [4]float64{0.1, 0.1, 0.8, 0.8}

// OpenForm shows the new-agent form, started in the current directory. Run
// from a Rex pane, it opens in a floating layer over the pane's tab and
// returns nil. Anywhere else it returns the command for the caller to run
// in its own terminal.
func (in *Inbox) OpenForm(ctx context.Context) (*exec.Cmd, error) {
	if len(in.Form) == 0 {
		return nil, errors.New("opening the form: no command for it")
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("opening the form: %w", err)
	}
	pane, inRex, err := rex.FindPane(ctx, in.Rex, in.Getenv)
	if err != nil {
		return nil, fmt.Errorf("opening the form: %w", err)
	}
	if !inRex {
		cmd := exec.CommandContext(ctx, in.Form[0], in.Form[1:]...)
		cmd.Dir = dir
		return cmd, nil
	}
	windowID, err := in.windowOf(ctx, pane)
	if err != nil {
		return nil, err
	}
	_, err = in.Rex.OpenLayer(ctx, rex.LayerOptions{
		SessionID: pane.SessionID,
		WindowID:  windowID,
		Command:   in.Form,
		Dir:       dir,
		Bounds:    formBounds,
	})
	if err != nil {
		return nil, fmt.Errorf("opening the form in Rex: %w", err)
	}
	return nil, nil
}

// The layer has to name the pane's own window: the session's active window
// is the server's idea, which an app switching tabs doesn't change.
func (in *Inbox) windowOf(ctx context.Context, pane rex.Pane) (string, error) {
	view, err := in.Rex.View(ctx, pane.SessionID)
	if err != nil {
		return "", fmt.Errorf("reading the Rex session this runs in: %w", err)
	}
	for _, w := range view.Windows {
		for _, b := range w.Blocks {
			if b.ID == pane.BlockID {
				return w.ID, nil
			}
		}
	}
	return "", fmt.Errorf("opening the form: block %s is in no window of its session", pane.BlockID)
}
