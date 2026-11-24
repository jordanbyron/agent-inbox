package cli

import (
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
		Short: "Show the inbox",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if fixture != "" {
				m, err := fixtureList(fixture)
				if err != nil {
					return err
				}
				return runList(cmd, m)
			}
			return showInbox(cmd, nil)
		},
	}
	cmd.Flags().StringVar(&fixture, "fixture", "", "draw this snapshot file, with every action stubbed out")
	return cmd
}

// showInbox makes sure the watcher is running and shows the list it writes.
func showInbox(cmd *cobra.Command, _ []string) error {
	srv, err := findServer(cmd.Context())
	if err != nil {
		return err
	}
	if err := ensureWatcher(cmd.Context(), srv); err != nil {
		return err
	}
	m, err := liveList(state.New(srv.dir))
	if err != nil {
		return err
	}
	return runList(cmd, m)
}

func runList(cmd *cobra.Command, m list.Model) error {
	// The program detects the terminal's color profile and NO_COLOR,
	// and brings the list's colors down to match.
	program := tea.NewProgram(m, tea.WithContext(cmd.Context()),
		tea.WithInput(cmd.InOrStdin()), tea.WithOutput(cmd.OutOrStdout()))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("running the list: %w", err)
	}
	return nil
}

// liveList draws the watcher's snapshot. Its actions come with the verbs.
func liveList(store *state.Store) (list.Model, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return list.Model{}, fmt.Errorf("finding the home directory: %w", err)
	}
	return list.New(list.Config{
		Load: func() (snapshot.Snapshot, time.Time, error) {
			var snap snapshot.Snapshot
			writtenAt, err := store.ReadSnapshot(&snap)
			return snap, writtenAt, err
		},
		Actions: list.NotWired(),
		Now:     time.Now,
		Home:    home,
	}), nil
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
