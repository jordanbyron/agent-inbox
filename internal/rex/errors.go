package rex

import (
	"errors"
	"regexp"
	"strings"
)

// ErrRemoteControlOff matches the error Show returns when a Rex app refuses
// to change tab because Remote Control is off in its settings. The error's
// own text is Rex's, which names the setting.
var ErrRemoteControlOff = errors.New("rex: remote control is off")

// ErrNotFound matches an error for a session, window or block Rex doesn't have.
var ErrNotFound = errors.New("rex: not found")

// ErrStreamEnded is the reason an event stream ended on its own, which
// happens when the Rex server goes away.
var ErrStreamEnded = errors.New("rex: event stream ended")

// Error is a failed rex command, with the API's error code when it gave one.
type Error struct {
	Command string
	Code    string
	Message string
}

func (e *Error) Error() string {
	return "rex " + e.Command + ": " + e.Message
}

// Is lets errors.Is match the sentinels above against an *Error.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrRemoteControlOff:
		return strings.Contains(e.Message, "Remote Control")
	case ErrNotFound:
		return e.Code == "not_found" || e.Code == "session_not_found" ||
			(e.Code == "" && strings.HasSuffix(e.Message, "not found"))
	}
	return false
}

// The CLI prints `Error: stream response "control-3" failed: MESSAGE
// (status=404 code=not_found)`, or just `Error: MESSAGE` for its own checks.
var stderrPattern = regexp.MustCompile(`^Error: (?:stream response "[^"]*" failed: )?(.*?)(?: \(status=\d+ code=([a-z_]+)\))?$`)

func parseError(command, stderr string) *Error {
	line := strings.TrimSpace(stderr)
	if i := strings.LastIndex(line, "\nError: "); i >= 0 {
		line = line[i+1:]
	}
	m := stderrPattern.FindStringSubmatch(line)
	if m == nil {
		return &Error{Command: command, Message: line}
	}
	return &Error{Command: command, Code: m[2], Message: m[1]}
}
