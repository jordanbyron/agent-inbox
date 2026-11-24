package cli

import (
	"context"

	"github.com/spf13/cobra"

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

// FormCommand is the command n runs to show the form.
var FormCommand = formCommand
