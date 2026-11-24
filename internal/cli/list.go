package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/jordanbyron/agent-inbox/internal/list"
	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
)

func newListCommand() *cobra.Command {
	var fixture string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show the inbox, for now from a fixture snapshot",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if fixture == "" {
				return errors.New("list needs --fixture until the watcher writes a snapshot")
			}
			m, err := fixtureList(fixture)
			if err != nil {
				return err
			}
			// The program detects the terminal's color profile and NO_COLOR,
			// and brings the list's colors down to match.
			program := tea.NewProgram(m, tea.WithContext(cmd.Context()),
				tea.WithInput(cmd.InOrStdin()), tea.WithOutput(cmd.OutOrStdout()))
			if _, err := program.Run(); err != nil {
				return fmt.Errorf("running the list: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fixture, "fixture", "", "draw this snapshot file, with every action stubbed out")
	return cmd
}

// fixtureList draws the fixture as if it were just written, with the clock
// starting from when it was, so its ages read as the fixture means them.
func fixtureList(path string) (list.Model, error) {
	var snap snapshot.Snapshot
	writtenAt, err := state.ReadSnapshotFile(path, &snap)
	if err != nil {
		return list.Model{}, err
	}
	started := time.Now()
	now := func() time.Time { return writtenAt.Add(time.Since(started)) }
	home, err := os.UserHomeDir()
	if err != nil {
		return list.Model{}, fmt.Errorf("finding the home directory: %w", err)
	}
	return list.New(list.Config{
		Load:    func() (snapshot.Snapshot, time.Time, error) { return snap, now(), nil },
		Actions: list.NotWired(),
		Now:     now,
		Home:    home,
	}), nil
}
