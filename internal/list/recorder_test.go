package list_test

import (
	"errors"
	"os/exec"
	"time"
)

var errNotWired = errors.New("pin is not wired yet")

type recorder struct {
	calls []string
	err   error
	// gate, when set, holds every call until it is closed.
	gate chan struct{}
	// form is what New returns for the list to run.
	form *exec.Cmd
}

func (r *recorder) record(call string) error {
	if r.gate != nil {
		<-r.gate
	}
	r.calls = append(r.calls, call)
	return r.err
}

func (r *recorder) Open(key string) error { return r.record("open " + key) }

func (r *recorder) Snooze(key string, until time.Time) error {
	return r.record("snooze " + key + " until " + until.Format(time.RFC3339))
}

func (r *recorder) SnoozeUntilWoken(key string) error {
	return r.record("snooze " + key + " until woken")
}
func (r *recorder) Wake(key string) error            { return r.record("wake " + key) }
func (r *recorder) Settle(key string) error          { return r.record("settle " + key) }
func (r *recorder) TogglePin(key string) error       { return r.record("pin " + key) }
func (r *recorder) Alias(key, name string) error     { return r.record("alias " + key + " " + name) }
func (r *recorder) Link(key, url string) error       { return r.record("link " + key + " " + url) }
func (r *recorder) Delete(key string) error          { return r.record("delete " + key) }
func (r *recorder) New() (*exec.Cmd, error)          { return r.form, r.record("new") }
func (r *recorder) OpenPullRequest(url string) error { return r.record("open " + url) }
func (r *recorder) CopyPullRequest(url string) error { return r.record("copy " + url) }
