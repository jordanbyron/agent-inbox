package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jordanbyron/agent-inbox/internal/luasnippet"
)

func newLuaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "lua",
		Short: "Print Rex actions for init.lua that run this binary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("finding this binary: %w", err)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), luasnippet.For(exe))
			return err
		},
	}
}
