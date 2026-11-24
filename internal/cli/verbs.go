package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/form"
	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/verbs"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

type inboxOpener func(ctx context.Context) (*verbs.Inbox, error)

func verbCommands(open inboxOpener) []*cobra.Command {
	return []*cobra.Command{
		newNewCommand(open),
		newOpenCommand(open),
		newSnoozeCommand(open),
		newWakeCommand(open),
		newPinCommand(open),
		newSettleCommand(open),
		newRestoreCommand(open),
		newAliasCommand(open),
		newLinkCommand(open),
		newDeleteCommand(open),
	}
}

// openInbox is where every verb gets its Inbox, and makes sure the watcher
// is running, since a verb's change reaches the list only through it.
func openInbox(ctx context.Context) (*verbs.Inbox, error) {
	srv, err := findServer(ctx)
	if err != nil {
		return nil, err
	}
	if err := ensureWatcher(ctx, srv); err != nil {
		return nil, err
	}
	return newInbox(srv)
}

func newInbox(srv server) (*verbs.Inbox, error) {
	path, err := config.Path()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("finding the home directory: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("finding this binary: %w", err)
	}
	store := state.New(srv.dir)
	return &verbs.Inbox{
		Store:  store,
		Rex:    srv.rex,
		Config: cfg,
		Git:    worktree.ExecGit{},
		GH:     pulls.GH{},
		System: verbs.ExecSystem{},
		Now:    time.Now,
		Snapshot: func() (snapshot.Snapshot, error) {
			var snap snapshot.Snapshot
			if _, err := store.ReadSnapshot(&snap); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return snapshot.Snapshot{}, err
			}
			return snap, nil
		},
		Getenv: os.Getenv,
		Home:   home,
		Form:   []string{exe, "new"},
	}, nil
}

func newNewCommand(open inboxOpener) *cobra.Command {
	var req verbs.StartRequest
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Start an agent in a new Rex tab; without --prompt, choose in the form",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, onTerminal := terminalIn(cmd)
			if req.Prompt == "" && !onTerminal {
				return errors.New("new needs --prompt when it is not on a terminal")
			}
			inbox, err := open(cmd.Context())
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("worktree") {
				req.Worktree = inbox.Config.Worktree.Default
			}
			if !cmd.Flags().Changed("session") {
				if pane, ok := rex.PaneFromEnv(inbox.Getenv); ok {
					req.Session = pane.SessionID
				}
			}
			if req.Prompt != "" {
				return start(cmd, inbox, req)
			}
			opts, err := formOptions(cmd.Context(), inbox, req, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			result, ok, err := form.Run(cmd.Context(), opts)
			if err != nil || !ok {
				return err
			}
			if err := start(cmd, inbox, fromForm(result, req)); err != nil {
				holdOnError(cmd, in, err)
				return err
			}
			return nil
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&req.Prompt, "prompt", "", "what to ask the agent")
	flags.StringVar(&req.Harness, "harness", "", "the recipe to start (default: the first, claude)")
	flags.StringVar(&req.Name, "name", "", "the agent's name (default: from the prompt)")
	flags.StringVar(&req.Dir, "cwd", ".", "the directory to start in")
	flags.StringVar(&req.Session, "session", "", "the Rex session's id or label (default: this pane's, else a new one)")
	flags.StringVar(&req.Model, "model", "", "the model, as the recipe lists them")
	flags.StringVar(&req.Effort, "effort", "", "the effort, as the recipe lists them")
	flags.StringVar(&req.Permissions, "permissions", "", "the permission mode, as the recipe lists them")
	flags.BoolVar(&req.Worktree, "worktree", true, "make a worktree when the directory is in a git repository (default from the config)")
	flags.BoolVar(&req.Show, "show", false, "show the new tab")
	return cmd
}

func start(cmd *cobra.Command, inbox *verbs.Inbox, req verbs.StartRequest) error {
	a, err := inbox.Start(cmd.Context(), req)
	if a.Key != "" {
		say(cmd, "started %s (%s)", a.Name, a.Key)
	}
	return err
}

func newOpenCommand(open inboxOpener) *cobra.Command {
	return verbCommand(open, verbSpec{
		use:   "open [AGENT]",
		short: "Show an agent's tab, restoring it first if it is settled",
		run: func(ctx context.Context, inbox *verbs.Inbox, t verbs.Target, _ string) (string, error) {
			a, err := inbox.Open(ctx, t)
			return "showing " + a.Name, err
		},
	})
}

func newSnoozeCommand(open inboxOpener) *cobra.Command {
	return verbCommand(open, verbSpec{
		use:        "snooze [AGENT] DURATION",
		short:      "Snooze an agent: 15m, 1h, tomorrow, until-woken, or a time such as fri 5pm",
		takesValue: true,
		run: func(ctx context.Context, inbox *verbs.Inbox, t verbs.Target, text string) (string, error) {
			s, err := verbs.ParseSnooze(text, inbox.Now())
			if err != nil {
				return "", err
			}
			a, err := inbox.Snooze(ctx, t, s)
			if err != nil {
				return "", err
			}
			if s.UntilWoken {
				return "snoozed " + a.Name + " until woken", nil
			}
			return "snoozed " + a.Name + " until " + s.Until.Format("Mon Jan 2 15:04"), nil
		},
	})
}

func newWakeCommand(open inboxOpener) *cobra.Command {
	return verbCommand(open, verbSpec{
		use:   "wake [AGENT]",
		short: "Wake a snoozed agent, or restore a settled one",
		run: func(ctx context.Context, inbox *verbs.Inbox, t verbs.Target, _ string) (string, error) {
			a, restored, err := inbox.Wake(ctx, t)
			if restored {
				return "restored " + a.Name, err
			}
			return "woke " + a.Name, err
		},
	})
}

func newPinCommand(open inboxOpener) *cobra.Command {
	return verbCommand(open, verbSpec{
		use:   "pin [AGENT]",
		short: "Pin an agent, or unpin it",
		run: func(ctx context.Context, inbox *verbs.Inbox, t verbs.Target, _ string) (string, error) {
			a, pinned, err := inbox.TogglePin(ctx, t)
			if !pinned {
				return "unpinned " + a.Name, err
			}
			return "pinned " + a.Name, err
		},
	})
}

func newSettleCommand(open inboxOpener) *cobra.Command {
	return verbCommand(open, verbSpec{
		use:   "settle [AGENT]",
		short: "Close an agent's tab and settle it",
		run: func(ctx context.Context, inbox *verbs.Inbox, t verbs.Target, _ string) (string, error) {
			a, err := inbox.Settle(ctx, t)
			return "settled " + a.Name, err
		},
	})
}

func newRestoreCommand(open inboxOpener) *cobra.Command {
	return verbCommand(open, verbSpec{
		use:   "restore [AGENT]",
		short: "Resume a settled agent in a new tab",
		run: func(ctx context.Context, inbox *verbs.Inbox, t verbs.Target, _ string) (string, error) {
			a, err := inbox.Restore(ctx, t)
			return "restored " + a.Name, err
		},
	})
}

func newAliasCommand(open inboxOpener) *cobra.Command {
	return verbCommand(open, verbSpec{
		use:        "alias [AGENT] NAME",
		short:      "Name an agent in the inbox; an empty name clears it",
		takesValue: true,
		run: func(ctx context.Context, inbox *verbs.Inbox, t verbs.Target, name string) (string, error) {
			a, err := inbox.Alias(ctx, t, name)
			if strings.TrimSpace(name) == "" {
				return "cleared the alias of " + a.Name, err
			}
			return "aliased " + a.Key + " to " + a.Name, err
		},
	})
}

func newLinkCommand(open inboxOpener) *cobra.Command {
	return verbCommand(open, verbSpec{
		use:        "link [AGENT] PR",
		short:      "Link a pull request by hand: a link, owner/repo#n or #n; empty clears it",
		takesValue: true,
		run: func(ctx context.Context, inbox *verbs.Inbox, t verbs.Target, input string) (string, error) {
			a, url, err := inbox.Link(ctx, t, input)
			if url == "" {
				return "cleared the link of " + a.Name, err
			}
			return "linked " + a.Name + " to " + url, err
		},
	})
}

func newDeleteCommand(open inboxOpener) *cobra.Command {
	var yes bool
	cmd := verbCommand(open, verbSpec{
		use:   "delete [AGENT]",
		short: "Close an agent's family's tabs, remove its worktrees and forget it",
		run: func(ctx context.Context, inbox *verbs.Inbox, t verbs.Target, _ string) (string, error) {
			a, members, err := inbox.Delete(ctx, t)
			if members > 1 {
				return fmt.Sprintf("deleted %s and %d more of its family", a.Name, members-1), err
			}
			return "deleted " + a.Name, err
		},
	})
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "delete without asking")
	cmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
		if yes {
			return nil
		}
		return confirm(cmd, "Delete it, its worktrees and its branches? [y/N] ")
	}
	return cmd
}

// confirm asks on a terminal. Anywhere else there is no one to answer, so
// it refuses rather than guess.
func confirm(cmd *cobra.Command, question string) error {
	in, ok := terminalIn(cmd)
	if !ok {
		return errors.New("not on a terminal: pass --yes to delete without asking")
	}
	_, _ = fmt.Fprint(cmd.ErrOrStderr(), question)
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading the answer: %w", err)
	}
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		return errors.New("not deleted")
	}
	return nil
}

type verbSpec struct {
	use        string
	short      string
	takesValue bool
	run        func(ctx context.Context, inbox *verbs.Inbox, t verbs.Target, value string) (string, error)
}

func verbCommand(open inboxOpener, spec verbSpec) *cobra.Command {
	var block string
	args := cobra.MaximumNArgs(1)
	if spec.takesValue {
		args = cobra.RangeArgs(1, 2)
	}
	cmd := &cobra.Command{
		Use:   spec.use,
		Short: spec.short,
		Args:  args,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := verbs.Target{Block: block}
			var value string
			if spec.takesValue {
				value, args = args[len(args)-1], args[:len(args)-1]
			}
			if len(args) == 1 {
				t.Key = args[0]
			}
			inbox, err := open(cmd.Context())
			if err != nil {
				return err
			}
			done, err := spec.run(cmd.Context(), inbox, t, value)
			if err != nil {
				return err
			}
			say(cmd, "%s", done)
			return nil
		},
	}
	cmd.Flags().StringVar(&block, "block", "", "aim at the agent in this Rex block (default: $REX_BLOCK)")
	return cmd
}

func say(cmd *cobra.Command, format string, args ...any) {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), format+"\n", args...)
}
