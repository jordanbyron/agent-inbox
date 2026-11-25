package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/reap"
	"github.com/jordanbyron/agent-inbox/internal/verbs"
)

// NewWithInbox is New with the verbs run against inbox instead of the real
// Rex server and state.
func NewWithInbox(inbox *verbs.Inbox) *cobra.Command {
	root := &cobra.Command{Use: "agent-inbox", SilenceUsage: true}
	root.AddCommand(verbCommands(func(context.Context) (*verbs.Inbox, error) { return inbox, nil })...)
	return root
}

// FormOptions is how new opens the form for a request its flags made.
var FormOptions = formOptions

// FromForm is how new turns the form's result back into a request.
var FromForm = fromForm

// InRexEnv is how a pane Rex starts runs this binary: the form, the parked inbox.
var InRexEnv = inRexEnv

// ReapDeleter builds the watcher's reap deleter for a state directory without
// calling findServer or ensureWatcher.
func ReapDeleter(stateDir string, cfg config.Config) (reap.Deleter, error) {
	return reapDeleter(server{dir: stateDir}, cfg)
}
