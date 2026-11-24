package rex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

// EventType is Rex's own name for an event.
type EventType string

// The events Events delivers. Rex sends more, which are dropped.
const (
	EventStatusChanged       EventType = "program_status_changed"
	EventStatusRemoved       EventType = "program_status_removed"
	EventProgramExited       EventType = "child_exited"
	EventBlockCreated        EventType = "block_created"
	EventBlockClosed         EventType = "block_closed"
	EventWindowCreated       EventType = "window_created"
	EventWindowClosed        EventType = "window_closed"
	EventWindowLabelChanged  EventType = "window_label_changed"
	EventActiveWindowChanged EventType = "active_window_changed"
	EventViewChanged         EventType = "session_view_changed"
	EventSessionCreated      EventType = "session_created"
	EventSessionDestroyed    EventType = "session_destroyed"
	EventSessionLabelChanged EventType = "session_label_changed"
	EventClientJoined        EventType = "client_joined"
	EventClientLeft          EventType = "client_left"
	EventClientConnected     EventType = "client_connected"
	EventClientDisconnected  EventType = "client_disconnected"
	// EventBlockSized is Rex's size_changed. An app sizes every block it
	// displays, each time it displays it, so this is how a watcher learns
	// which tab an app shows: Rex has no call that says.
	EventBlockSized EventType = "size_changed"
)

// Event is one thing that happened on the server. Each type fills the ids
// it is about; Label goes with the created and label-changed types, Record
// with EventStatusChanged, RecordIDs with EventStatusRemoved, Reason with
// both status types, ExitCode with EventProgramExited, Revision with
// EventViewChanged, and ClientID with the four client types and with
// EventBlockSized, where it is the block's owner, the app that sized it. ClientKind
// goes with EventClientJoined: "app" for a Rex app, "cli" for a rex command.
type Event struct {
	Type       EventType
	SessionID  string
	WindowID   string
	BlockID    string
	Label      string
	Record     StatusRecord
	RecordIDs  []string
	Reason     string
	ExitCode   int
	Revision   int
	ClientID   string
	ClientKind string
}

// A clipboard or selection event can carry a large copy.
const maxEventBytes = 16 << 20

// Events follows every session on the server over one `rex events`
// process. The events channel closes when the stream ends, right after the
// reason arrives on the error channel: ctx's error if it was canceled, or
// an error matching ErrStreamEnded if the server went away.
func (c *Client) Events(ctx context.Context) (<-chan Event, <-chan error) {
	events := make(chan Event)
	errs := make(chan error, 1)
	go func() {
		errs <- c.stream(ctx, events)
		close(errs)
		close(events)
	}()
	return events, errs
}

func (c *Client) stream(ctx context.Context, events chan<- Event) error {
	cmd := exec.CommandContext(ctx, c.bin, noAutostart, "events", "--all", "--json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("starting rex events: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting rex events: %w", err)
	}
	readErr := deliver(ctx, stdout, events)
	if readErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if readErr != nil {
		return readErr
	}
	if _, exited := errors.AsType[*exec.ExitError](waitErr); exited {
		return fmt.Errorf("%w: %w", ErrStreamEnded, parseError("events", stderr.String()))
	}
	return ErrStreamEnded
}

func deliver(ctx context.Context, stdout io.Reader, events chan<- Event) error {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), maxEventBytes)
	for scanner.Scan() {
		event, ok, err := decodeEvent(scanner.Bytes())
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		select {
		case events <- event:
		case <-ctx.Done():
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading rex events: %w", err)
	}
	return nil
}

type wireEvent struct {
	Type    EventType       `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type wireFields struct {
	Session struct {
		ID    string `json:"session_id"`
		Label string `json:"label"`
	} `json:"session"`
	Client struct {
		ID   string `json:"client_id"`
		Info struct {
			Kind string `json:"kind"`
		} `json:"info"`
	} `json:"client"`
	ClientID  string          `json:"client_id"`
	SessionID string          `json:"session_id"`
	WindowID  string          `json:"window_id"`
	BlockID   string          `json:"block_id"`
	Label     string          `json:"label"`
	Revision  int             `json:"revision"`
	Event     wireEvent       `json:"event"`
	Name      EventType       `json:"name"`
	Payload   json.RawMessage `json:"payload"`
	Record    StatusRecord    `json:"record"`
	IDs       []string        `json:"ids"`
	Reason    string          `json:"reason"`
	ExitCode  int             `json:"exit_code"`
	Owner     string          `json:"owner"`
}

// decodeEvent reads one line of `rex events --json`. Session events come
// wrapped in session_event, and a block's own events, program status among
// them, in a further block_event.
func decodeEvent(line []byte) (Event, bool, error) {
	var outer wireEvent
	if err := json.Unmarshal(line, &outer); err != nil {
		return Event{}, false, fmt.Errorf("decoding a rex event: %w", err)
	}
	fields, err := decodeFields(outer)
	if err != nil {
		return Event{}, false, err
	}
	switch outer.Type {
	case EventSessionCreated:
		return Event{Type: outer.Type, SessionID: fields.Session.ID, Label: fields.Session.Label}, true, nil
	case EventSessionDestroyed:
		return Event{Type: outer.Type, SessionID: fields.SessionID}, true, nil
	case EventClientJoined:
		return Event{Type: outer.Type, ClientID: fields.Client.ID, ClientKind: fields.Client.Info.Kind}, true, nil
	case EventClientLeft:
		return Event{Type: outer.Type, ClientID: fields.ClientID}, true, nil
	case "session_event":
		return decodeSessionEvent(fields.SessionID, fields.Event)
	}
	return Event{}, false, nil
}

func decodeSessionEvent(sessionID string, inner wireEvent) (Event, bool, error) {
	fields, err := decodeFields(inner)
	if err != nil {
		return Event{}, false, err
	}
	event := Event{
		Type:      inner.Type,
		SessionID: sessionID,
		WindowID:  fields.WindowID,
		BlockID:   fields.BlockID,
		Label:     fields.Label,
		Revision:  fields.Revision,
	}
	switch inner.Type {
	case EventClientConnected, EventClientDisconnected:
		event.ClientID = fields.ClientID
		return event, true, nil
	case EventBlockCreated, EventBlockClosed, EventWindowCreated, EventWindowClosed,
		EventWindowLabelChanged, EventActiveWindowChanged, EventViewChanged, EventSessionLabelChanged:
		return event, true, nil
	case "block_event":
		return decodeBlockEvent(sessionID, fields)
	}
	return Event{}, false, nil
}

func decodeBlockEvent(sessionID string, block wireFields) (Event, bool, error) {
	switch block.Name {
	case EventStatusChanged, EventStatusRemoved, EventProgramExited, EventBlockSized:
	default:
		return Event{}, false, nil
	}
	fields, err := decodeFields(wireEvent{Type: block.Name, Payload: block.Payload})
	if err != nil {
		return Event{}, false, err
	}
	return Event{
		Type:      block.Name,
		SessionID: sessionID,
		BlockID:   block.BlockID,
		Record:    fields.Record,
		RecordIDs: fields.IDs,
		Reason:    fields.Reason,
		ExitCode:  fields.ExitCode,
		ClientID:  fields.Owner,
	}, true, nil
}

func decodeFields(event wireEvent) (wireFields, error) {
	var fields wireFields
	if len(event.Payload) == 0 {
		return fields, nil
	}
	if err := json.Unmarshal(event.Payload, &fields); err != nil {
		return fields, fmt.Errorf("decoding a rex %s event: %w", event.Type, err)
	}
	return fields, nil
}
