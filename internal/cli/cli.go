// Package cli is the agent-inbox command line: the root command and its subcommands.
package cli

import (
	"runtime/debug"

	"github.com/spf13/cobra"
)

// New returns the agent-inbox root command with every subcommand attached.
func New() *cobra.Command {
	root := &cobra.Command{
		Use:          "agent-inbox",
		Short:        "A triage inbox for coding agents running in Rex",
		Args:         cobra.NoArgs,
		RunE:         showInbox,
		SilenceUsage: true,
	}
	root.AddCommand(newVersionCommand(), newListCommand(), newLsCommand(), newWatchCommand())
	root.AddCommand(newFormCommand())
	root.AddCommand(newReapCommand(openInbox), newLuaCommand())
	root.AddCommand(verbCommands(openInbox)...)
	return root
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println(version())
		},
	}
}

// version is the one the Go toolchain stamped into the binary: a tag for
// `go install module@v1.2.3`, a pseudo-version for a build from a checkout.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return info.Main.Version
}
