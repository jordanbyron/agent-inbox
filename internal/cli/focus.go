package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

const holdFocusTimeout = time.Second

func newHoldFocusCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "hold-focus SESSION FROM TARGET",
		Short:  "Keep a newly opened Rex block focused after a palette closes",
		Hidden: true,
		Args:   cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			bin, err := findRexBinary()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), holdFocusTimeout)
			defer cancel()
			return rex.HoldFocus(ctx, rex.New(bin), args[0], args[1], args[2])
		},
	}
}
