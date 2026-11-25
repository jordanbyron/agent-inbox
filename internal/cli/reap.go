package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/reap"
)

func newReapCommand(open inboxOpener) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "reap",
		Short: "Delete the families settled for 14 days, as reap = true does",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			inbox, err := open(cmd.Context())
			if err != nil {
				return err
			}
			st, err := inbox.Store.Read()
			if err != nil {
				return err
			}
			now := inbox.Now()
			if dryRun {
				due := reap.Due(st, now)
				for _, fam := range due {
					say(cmd, "due: %s", describeFamily(fam, now))
				}
				if len(due) == 0 {
					say(cmd, "nothing to reap")
				}
				return nil
			}
			results := (&reap.Reaper{Deleter: inbox}).Sweep(cmd.Context(), st, now)
			kept := 0
			for _, r := range results {
				if r.Err != nil {
					kept++
					say(cmd, "kept %s: %v", describeFamily(r.Family, now), r.Err)
					continue
				}
				say(cmd, "deleted %s", describeFamily(r.Family, now))
			}
			if len(results) == 0 {
				say(cmd, "nothing to reap")
			}
			if kept > 0 {
				return fmt.Errorf("kept %d of %d families", kept, len(results))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the families due, and delete nothing (delete may still refuse one)")
	return cmd
}

func describeFamily(fam reap.Family, now time.Time) string {
	s := fmt.Sprintf("%s (%s), settled %dd ago", fam.Name, fam.Key, int(now.Sub(fam.LastSettledAt).Hours()/24))
	if fam.Members > 1 {
		s += fmt.Sprintf(", with %d more of its family", fam.Members-1)
	}
	return s
}

// reapDeleter builds the Inbox the watcher uses to delete reaped families.
// It calls newInbox directly rather than openInbox so the watcher does not
// call ensureWatcher and recursively spawn itself.
func reapDeleter(srv server, cfg config.Config) (reap.Deleter, error) {
	if !cfg.Reap {
		return nil, nil
	}
	return newInbox(srv)
}
