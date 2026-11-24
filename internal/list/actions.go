package list

import (
	"fmt"
	"os/exec"
	"time"
)

// Actions is what the list asks for when a key changes something. Each
// takes the key of the row under the cursor; a verb aimed at a child acts
// on its family, which is the implementation's business, not the list's.
type Actions interface {
	Open(key string) error
	Snooze(key string, until time.Time) error
	SnoozeUntilWoken(key string) error
	Wake(key string) error
	Settle(key string) error
	TogglePin(key string) error
	// Alias sets the row's alias, and an empty name clears it.
	Alias(key, name string) error
	// Link links a pull request by hand, and an empty url clears the link.
	Link(key, url string) error
	Delete(key string) error
	// New opens the new-agent form. It returns nil when the form opened
	// somewhere else, such as a Rex layer, and otherwise the command that
	// runs it, which the list runs in its own terminal while it waits.
	New() (*exec.Cmd, error)
	OpenPullRequest(url string) error
	CopyPullRequest(url string) error
}

// NotWired returns Actions with nothing behind them yet: every call says so.
func NotWired() Actions {
	return notWired{}
}

type notWired struct{}

func errNotWired(verb string) error {
	return fmt.Errorf("%s is not wired yet", verb)
}

func (notWired) Open(string) error              { return errNotWired("open") }
func (notWired) Snooze(string, time.Time) error { return errNotWired("snooze") }
func (notWired) SnoozeUntilWoken(string) error  { return errNotWired("snooze") }
func (notWired) Wake(string) error              { return errNotWired("wake") }
func (notWired) Settle(string) error            { return errNotWired("settle") }
func (notWired) TogglePin(string) error         { return errNotWired("pin") }
func (notWired) Alias(string, string) error     { return errNotWired("alias") }
func (notWired) Link(string, string) error      { return errNotWired("link") }
func (notWired) Delete(string) error            { return errNotWired("delete") }
func (notWired) New() (*exec.Cmd, error)        { return nil, errNotWired("new") }
func (notWired) OpenPullRequest(string) error   { return errNotWired("opening a pull request") }
func (notWired) CopyPullRequest(string) error   { return errNotWired("copying a pull request") }
