package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"github.com/jordanbyron/agent-inbox/internal/list"
	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/verbs"
)

const modalUsage = "close once an agent is shown, or on Esc, as in a Rex floating layer"

func newListCommand() *cobra.Command {
	var fixture string
	var modal bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show the inbox",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if fixture != "" {
				m, err := fixtureList(fixture, modal)
				if err != nil {
					return err
				}
				return runList(cmd, m, "")
			}
			return showInbox(cmd, modal)
		},
	}
	cmd.Flags().StringVar(&fixture, "fixture", "", "draw this snapshot file, with every action stubbed out")
	cmd.Flags().BoolVar(&modal, "modal", false, modalUsage)
	return cmd
}

// showInbox makes sure the watcher is running and shows the list it writes.
func showInbox(cmd *cobra.Command, modal bool) error {
	srv, err := findServer(cmd.Context())
	if err != nil {
		return err
	}
	if err := ensureWatcher(cmd.Context(), srv); err != nil {
		return err
	}
	inbox, err := newInbox(srv)
	if err != nil {
		return err
	}
	restart := func() error { return ensureWatcher(cmd.Context(), srv) }
	return runList(cmd, liveList(cmd.Context(), inbox, restart, modal), srv.dir)
}

// runList shows the list until it quits. With a state directory, a new
// snapshot there reloads it at once rather than on the next tick.
func runList(cmd *cobra.Command, m list.Model, stateDir string) error {
	// The program detects the terminal's color profile and NO_COLOR,
	// and brings the list's colors down to match.
	program := tea.NewProgram(m, tea.WithContext(cmd.Context()),
		tea.WithInput(cmd.InOrStdin()), tea.WithOutput(cmd.OutOrStdout()))
	if stateDir != "" {
		stop := reloadOnSnapshot(stateDir, program)
		defer stop()
	}
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("running the list: %w", err)
	}
	return nil
}

// reloadOnSnapshot tells the program each time the watcher replaces
// snapshot.json. It watches the directory, since the watcher renames a new
// file over the old one. Without a watch the list's own tick still reloads.
func reloadOnSnapshot(dir string, program *tea.Program) (stop func()) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return func() {}
	}
	if err := fsw.Add(dir); err != nil {
		_ = fsw.Close()
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for event := range fsw.Events {
			if filepath.Base(event.Name) == "snapshot.json" {
				program.Send(list.ReloadMsg{})
			}
		}
	}()
	return func() {
		_ = fsw.Close()
		<-done
	}
}

// liveList draws the watcher's snapshot, with each key running a verb.
func liveList(ctx context.Context, inbox *verbs.Inbox, restart func() error, modal bool) list.Model {
	return list.New(list.Config{
		Load: func() (snapshot.Snapshot, time.Time, error) {
			var snap snapshot.Snapshot
			writtenAt, err := inbox.Store.ReadSnapshot(&snap)
			return snap, writtenAt, err
		},
		Actions: inbox.ListActions(ctx),
		Now:     time.Now,
		Home:    inbox.Home,
		Restart: restart,
		Modal:   modal,
	})
}

// fixtureList draws the fixture as if it were just written, with the clock
// starting from when it was, so its ages read as the fixture means them.
func fixtureList(path string, modal bool) (list.Model, error) {
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
		Modal:   modal,
	}), nil
}
