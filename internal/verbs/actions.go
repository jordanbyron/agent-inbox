package verbs

import (
	"context"
	"os/exec"
	"time"

	"github.com/jordanbyron/agent-inbox/internal/list"
)

// ListActions returns the list's keys wired to the inbox's verbs, each run
// with ctx.
func (in *Inbox) ListActions(ctx context.Context) list.Actions {
	return listActions{ctx: ctx, inbox: in}
}

// list.Actions takes no context, so the adapter carries the list's own.
type listActions struct {
	ctx   context.Context
	inbox *Inbox
}

func (l listActions) Open(key string) error {
	_, err := l.inbox.Open(l.ctx, Target{Key: key})
	return err
}

func (l listActions) Snooze(key string, until time.Time) error {
	_, err := l.inbox.Snooze(l.ctx, Target{Key: key}, Snooze{Until: until})
	return err
}

func (l listActions) SnoozeUntilWoken(key string) error {
	_, err := l.inbox.Snooze(l.ctx, Target{Key: key}, Snooze{UntilWoken: true})
	return err
}

func (l listActions) Wake(key string) error {
	_, _, err := l.inbox.Wake(l.ctx, Target{Key: key})
	return err
}

func (l listActions) Settle(key string) error {
	_, err := l.inbox.Settle(l.ctx, Target{Key: key})
	return err
}

func (l listActions) TogglePin(key string) error {
	_, _, err := l.inbox.TogglePin(l.ctx, Target{Key: key})
	return err
}

func (l listActions) Alias(key, name string) error {
	_, err := l.inbox.Alias(l.ctx, Target{Key: key}, name)
	return err
}

func (l listActions) Link(key, url string) error {
	_, _, err := l.inbox.Link(l.ctx, Target{Key: key}, url)
	return err
}

func (l listActions) Delete(key string) error {
	_, _, err := l.inbox.Delete(l.ctx, Target{Key: key})
	return err
}

func (l listActions) New() (*exec.Cmd, error) {
	return l.inbox.OpenForm(l.ctx)
}

func (l listActions) OpenPullRequest(url string) error {
	return l.inbox.OpenPullRequest(l.ctx, url)
}

func (l listActions) CopyPullRequest(url string) error {
	return l.inbox.CopyPullRequest(l.ctx, url)
}
