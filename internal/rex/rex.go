// Package rex is the one place that knows how to talk to Rex, the terminal
// multiplexer the agents run in. Other packages declare the small interface
// they need and take a *Client, or a rextest.Fake in their tests.
package rex

import "time"

// Server identifies the running Rex server.
type Server struct {
	HostID      string
	InstanceID  string
	PID         int
	StartedAt   time.Time
	APIRevision int
}

// StateID names the server's state directory. The host id survives a
// server restart; Rex leaves it out only when it has no state directory of
// its own, and then the instance id is all there is.
func (s Server) StateID() string {
	if s.HostID != "" {
		return s.HostID
	}
	return s.InstanceID
}

// Session is one Rex session.
type Session struct {
	ID    string
	Label string
}

// View is a session's windows (the app's tabs) and which one is active.
type View struct {
	SessionID      string
	Label          string
	ActiveWindowID string
	// Revision goes up with every change; compare it only within one session.
	Revision int
	Windows  []Window
}

// Window is one tab of a session.
type Window struct {
	ID             string
	Label          string
	FocusedBlockID string
	// Blocks holds the tiled blocks in layout order, then each floating
	// layer's, back to front.
	Blocks []Block
}

// Block is one terminal in a window.
type Block struct {
	ID    string
	Label string
	Owner string
}

// StatusRecord is one program status record a block holds, as reported
// with OSC 7501. Rex decodes Title and Msg from their base64.
type StatusRecord struct {
	// ID is empty for the root record and slash-separated for children.
	ID           string    `json:"id"`
	State        string    `json:"state"`
	Kind         string    `json:"kind"`
	App          string    `json:"app"`
	EffectiveApp string    `json:"effective_app"`
	Title        string    `json:"title"`
	Msg          string    `json:"msg"`
	Progress     *int      `json:"progress"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Process is what runs in a block.
type Process struct {
	Alive bool
	// ExitCode is -1 when a signal ended the program.
	ExitCode *int
	// Dir is the foreground process's working directory, or the program's
	// when nothing else is in the foreground.
	Dir string
}

// App is a connected Rex app, as opposed to a CLI or other client.
type App struct {
	ClientID   string
	Label      string
	SessionIDs []string
}

// ShownWindow is the window an app shows for one session it is attached to.
type ShownWindow struct {
	ClientID  string
	SessionID string
	WindowID  string
}

// StartOptions says where and how Client.Start runs a command.
type StartOptions struct {
	// SessionID is the session to open a window in. Empty makes a new
	// session labeled SessionLabel, or one Rex names when that is empty too.
	SessionID    string
	SessionLabel string
	Command      []string
	Dir          string
	BlockLabel   string
	WindowLabel  string
	// Focus makes the new window the session's active window on the server.
	// It does not move an app; Client.Show does that.
	Focus bool
}

// Started is where Client.Start put the command.
type Started struct {
	SessionID string
	WindowID  string
	BlockID   string
}
