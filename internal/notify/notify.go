// Package notify sends desktop notifications through whichever program the
// machine has for it.
package notify

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Notification is one desktop notification.
type Notification struct {
	Title string
	Body  string
	// Group names what the notification is about, so a later one in the
	// same group replaces it where the program allows.
	Group string
}

// ErrNoProgram is the error when the machine has no program to notify with.
var ErrNoProgram = errors.New("no notification program: install terminal-notifier (macOS) or notify-send (Linux)")

const timeout = 10 * time.Second

// Desktop notifies through the program it picked when it was made.
type Desktop struct {
	Program string
	build   func(Notification) []string
}

// NewDesktop picks the program for goos, given has, which reports whether a
// program is on PATH. On macOS it prefers terminal-notifier, whose -group
// replaces an earlier notification about the same agent, and falls back to
// osascript.
func NewDesktop(goos string, has func(string) bool) (Desktop, error) {
	switch {
	case goos == "darwin" && has("terminal-notifier"):
		return Desktop{Program: "terminal-notifier", build: terminalNotifier}, nil
	case goos == "darwin" && has("osascript"):
		return Desktop{Program: "osascript", build: osascript}, nil
	case goos != "darwin" && has("notify-send"):
		return Desktop{Program: "notify-send", build: notifySend}, nil
	}
	return Desktop{}, ErrNoProgram
}

// NewSystemDesktop is NewDesktop for this machine.
func NewSystemDesktop() (Desktop, error) {
	return NewDesktop(runtime.GOOS, func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	})
}

// Command is the argv that sends n.
func (d Desktop) Command(n Notification) []string {
	return append([]string{d.Program}, d.build(n)...)
}

// Notify sends n. Its error carries the program's exit and what it printed.
func (d Desktop) Notify(ctx context.Context, n Notification) error {
	if d.build == nil {
		return ErrNoProgram
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	argv := d.Command(n)
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("notifying through %s: %w: %s", d.Program, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func terminalNotifier(n Notification) []string {
	argv := []string{"-title", n.Title, "-message", n.Body}
	if n.Group != "" {
		argv = append(argv, "-group", n.Group)
	}
	return argv
}

// The text reaches AppleScript as the run handler's arguments, so nothing
// in it is ever read as script.
func osascript(n Notification) []string {
	return []string{
		"-e", "on run argv",
		"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
		"-e", "end run",
		n.Title, n.Body,
	}
}

func notifySend(n Notification) []string {
	return []string{"--app-name=agent-inbox", "--", n.Title, n.Body}
}
