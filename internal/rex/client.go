package rex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"time"
)

// Client talks to the Rex server by running the rex CLI, which ships with
// the server and so keeps up with its API revisions.
type Client struct {
	bin string
}

// New returns a Client that runs the rex binary at bin.
func New(bin string) *Client {
	return &Client{bin: bin}
}

// Server identifies the running server.
func (c *Client) Server(ctx context.Context) (Server, error) {
	var out struct {
		PID         int       `json:"pid"`
		InstanceID  string    `json:"instance_id"`
		HostID      string    `json:"host_id"`
		StartedAt   time.Time `json:"started_at"`
		APIRevision int       `json:"api_revision"`
	}
	if err := c.call(ctx, "", "server.status", nil, &out); err != nil {
		return Server{}, err
	}
	return Server{
		HostID:      out.HostID,
		InstanceID:  out.InstanceID,
		PID:         out.PID,
		StartedAt:   out.StartedAt,
		APIRevision: out.APIRevision,
	}, nil
}

// Sessions lists the server's sessions in Rex's order.
func (c *Client) Sessions(ctx context.Context) ([]Session, error) {
	var out struct {
		Sessions []struct {
			ID    string `json:"session_id"`
			Label string `json:"label"`
		} `json:"sessions"`
	}
	if err := c.call(ctx, "", "session.list", nil, &out); err != nil {
		return nil, err
	}
	sessions := make([]Session, 0, len(out.Sessions))
	for _, s := range out.Sessions {
		sessions = append(sessions, Session(s))
	}
	return sessions, nil
}

// View returns a session's windows and their blocks.
func (c *Client) View(ctx context.Context, sessionID string) (View, error) {
	var out struct {
		SessionID      string `json:"session_id"`
		Label          string `json:"label"`
		ActiveWindowID string `json:"active_window_id"`
		Revision       int    `json:"revision"`
		Windows        []struct {
			ID             string `json:"window_id"`
			Label          string `json:"label"`
			FocusedBlockID string `json:"focused_block_id"`
			Layers         []struct {
				Blocks []struct {
					ID    string `json:"block_id"`
					Label string `json:"label"`
					Owner string `json:"owner"`
				} `json:"blocks"`
			} `json:"layers"`
		} `json:"windows"`
	}
	if err := c.call(ctx, sessionID, "session.view", nil, &out); err != nil {
		return View{}, err
	}
	view := View{
		SessionID:      out.SessionID,
		Label:          out.Label,
		ActiveWindowID: out.ActiveWindowID,
		Revision:       out.Revision,
	}
	for _, w := range out.Windows {
		window := Window{ID: w.ID, Label: w.Label, FocusedBlockID: w.FocusedBlockID}
		for _, layer := range w.Layers {
			for _, b := range layer.Blocks {
				window.Blocks = append(window.Blocks, Block(b))
			}
		}
		view.Windows = append(view.Windows, window)
	}
	return view, nil
}

// Status returns the program status records a block holds, root first.
func (c *Client) Status(ctx context.Context, sessionID, blockID string) ([]StatusRecord, error) {
	var out struct {
		Records []StatusRecord `json:"records"`
	}
	if err := c.blockMethod(ctx, sessionID, blockID, "program_status", &out); err != nil {
		return nil, err
	}
	return out.Records, nil
}

// Process reports whether a block's program is running, and where.
func (c *Client) Process(ctx context.Context, sessionID, blockID string) (Process, error) {
	type process struct {
		Cwd string `json:"cwd"`
	}
	var out struct {
		LastExit *struct {
			ExitCode int `json:"exit_code"`
		} `json:"last_exit"`
		Child      *process `json:"child"`
		Foreground *process `json:"foreground"`
	}
	if err := c.blockMethod(ctx, sessionID, blockID, "process", &out); err != nil {
		return Process{}, err
	}
	var p Process
	if out.LastExit != nil {
		p.ExitCode = &out.LastExit.ExitCode
	}
	if out.Child != nil {
		p.Alive = true
		p.Dir = out.Child.Cwd
	}
	// The child can be login(1), running as root, whose cwd Rex can't read.
	if out.Foreground != nil && out.Foreground.Cwd != "" {
		p.Dir = out.Foreground.Cwd
	}
	return p, nil
}

// Apps lists the connected Rex apps.
func (c *Client) Apps(ctx context.Context) ([]App, error) {
	var out struct {
		Clients []struct {
			ID   string `json:"client_id"`
			Info struct {
				Label string `json:"label"`
			} `json:"info"`
			ConnectionState string   `json:"connection_state"`
			SessionIDs      []string `json:"session_ids"`
		} `json:"clients"`
	}
	args := map[string]any{"kinds": []string{"app"}}
	if err := c.call(ctx, "", "client.list", args, &out); err != nil {
		return nil, err
	}
	var apps []App
	for _, cl := range out.Clients {
		// A lost app may still reconnect, but it can't be shown anything now.
		if cl.ConnectionState == "lost" {
			continue
		}
		apps = append(apps, App{ClientID: cl.ID, Label: cl.Info.Label, SessionIDs: cl.SessionIDs})
	}
	return apps, nil
}

// Start runs a command in a new window and returns where it went. A new
// session it made is destroyed again if the window can't be made.
func (c *Client) Start(ctx context.Context, opts StartOptions) (Started, error) {
	sessionID := opts.SessionID
	if sessionID == "" {
		var err error
		if sessionID, err = c.createSession(ctx, opts.SessionLabel); err != nil {
			return Started{}, err
		}
	}
	started, err := c.newWindow(ctx, sessionID, opts)
	if err != nil && opts.SessionID == "" {
		if destroyErr := c.call(ctx, sessionID, "session.destroy", nil, nil); destroyErr != nil {
			err = errors.Join(err, fmt.Errorf("destroying the session it made: %w", destroyErr))
		}
	}
	return started, err
}

// Blocks made by session.create keep this connection as owner after it
// exits, so the app may never size them; session.new_window's are let go.
func (c *Client) createSession(ctx context.Context, label string) (string, error) {
	args := map[string]any{}
	if label != "" {
		args["label"] = label
	}
	var out struct {
		SessionID string `json:"session_id"`
	}
	if err := c.call(ctx, "", "session.create", args, &out); err != nil {
		return "", err
	}
	return out.SessionID, nil
}

func (c *Client) newWindow(ctx context.Context, sessionID string, opts StartOptions) (Started, error) {
	options := map[string]any{}
	if len(opts.Command) > 0 {
		options["command"] = opts.Command
	}
	if opts.Dir != "" {
		options["cwd"] = opts.Dir
	}
	block := map[string]any{"flavor": "com.superlogical.terminal.shell", "options": options}
	if opts.BlockLabel != "" {
		block["label"] = opts.BlockLabel
	}
	args := map[string]any{"focus": opts.Focus, "layout": map[string]any{"block": block}}
	if opts.WindowLabel != "" {
		args["window_label"] = opts.WindowLabel
	}
	var out struct {
		WindowID string   `json:"window_id"`
		BlockIDs []string `json:"block_ids"`
	}
	if err := c.call(ctx, sessionID, "session.new_window", args, &out); err != nil {
		return Started{}, err
	}
	if len(out.BlockIDs) != 1 {
		return Started{}, fmt.Errorf("rex session.new_window: made %d blocks, want 1", len(out.BlockIDs))
	}
	return Started{SessionID: sessionID, WindowID: out.WindowID, BlockID: out.BlockIDs[0]}, nil
}

// OpenLayer runs a command in a new floating layer at the front of a window,
// focused. The layer closes with its block when the command exits.
func (c *Client) OpenLayer(ctx context.Context, opts LayerOptions) (Layer, error) {
	options := map[string]any{"command": opts.Command}
	if opts.Dir != "" {
		options["cwd"] = opts.Dir
	}
	// The block is this call's until the CLI exits, which lets it go, so
	// the app sizes it as soon as the call returns.
	args := map[string]any{
		"window_id": opts.WindowID,
		"bounds":    opts.Bounds,
		"focus":     true,
		"layout":    map[string]any{"block": map[string]any{"flavor": "com.superlogical.terminal.shell", "options": options}},
	}
	var out struct {
		LayerID  string   `json:"layer_id"`
		BlockIDs []string `json:"block_ids"`
	}
	if err := c.call(ctx, opts.SessionID, "session.new_layer", args, &out); err != nil {
		return Layer{}, err
	}
	if len(out.BlockIDs) != 1 {
		return Layer{}, fmt.Errorf("rex session.new_layer: made %d blocks, want 1", len(out.BlockIDs))
	}
	return Layer{ID: out.LayerID, BlockID: out.BlockIDs[0]}, nil
}

// CloseBlock closes a block, ending its program. Rex closes the window too
// when it was the window's last block.
func (c *Client) CloseBlock(ctx context.Context, sessionID, blockID string) error {
	return c.call(ctx, sessionID, "block.close", map[string]string{"block_id": blockID}, nil)
}

// CloseWindow closes a window and every block in it.
func (c *Client) CloseWindow(ctx context.Context, sessionID, windowID string) error {
	return c.call(ctx, sessionID, "session.close_window", map[string]string{"window_id": windowID}, nil)
}

// Show focuses a block on the server and asks every app attached to its
// session to select its window. The server's focus alone doesn't move an
// app. An app with Remote Control off refuses with ErrRemoteControlOff.
func (c *Client) Show(ctx context.Context, sessionID, blockID string) error {
	var described struct {
		Block struct {
			WindowID string `json:"window_id"`
		} `json:"block"`
	}
	if err := c.call(ctx, sessionID, "session.describe_block", map[string]string{"block_id": blockID}, &described); err != nil {
		return err
	}
	windowID := described.Block.WindowID
	if windowID == "" {
		return fmt.Errorf("showing block %s: it is not in a window", blockID)
	}
	if err := c.call(ctx, sessionID, "session.focus_block", map[string]string{"block_id": blockID}, nil); err != nil {
		return err
	}
	apps, err := c.Apps(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, app := range apps {
		if !slices.Contains(app.SessionIDs, sessionID) {
			continue
		}
		args := map[string]any{
			"client_id": app.ClientID,
			"action":    "session.select",
			"args":      map[string]string{"session_id": sessionID, "window_id": windowID},
		}
		if err := c.call(ctx, "", "client.perform", args, nil); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Capture returns a block's screen and scrollback as text, with
// soft-wrapped lines joined so a long link comes back whole.
func (c *Client) Capture(ctx context.Context, sessionID, blockID string) (string, error) {
	out, err := c.run(ctx, "capture", "capture", "-s", sessionID, "-b", blockID, "--trim", "--unwrap")
	return string(out), err
}

func (c *Client) blockMethod(ctx context.Context, sessionID, blockID, method string, out any) error {
	args := map[string]string{"block_id": blockID, "method": method}
	return c.call(ctx, sessionID, "block.method", args, out)
}

// call runs `rex api call`. With a session, the CLI attaches to it first,
// which every session method needs, and fills in session_id.
func (c *Client) call(ctx context.Context, sessionID, method string, args, out any) error {
	argv := []string{"api", "call"}
	if sessionID != "" {
		argv = append(argv, "-s", sessionID)
	}
	argv = append(argv, method)
	if args != nil {
		encoded, err := json.Marshal(args)
		if err != nil {
			return fmt.Errorf("encoding the arguments to %s: %w", method, err)
		}
		argv = append(argv, string(encoded))
	}
	stdout, err := c.run(ctx, method, argv...)
	if err != nil || out == nil {
		return err
	}
	if err := json.Unmarshal(stdout, out); err != nil {
		return fmt.Errorf("decoding the result of %s: %w", method, err)
	}
	return nil
}

func (c *Client) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("rex %s: %w", name, ctxErr)
	}
	if _, exited := errors.AsType[*exec.ExitError](err); exited {
		return nil, parseError(name, stderr.String())
	}
	if err != nil {
		return nil, fmt.Errorf("running rex %s: %w", name, err)
	}
	return stdout.Bytes(), nil
}
