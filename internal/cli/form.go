package cli

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/jordanbyron/agent-inbox/internal/form"
	"github.com/jordanbyron/agent-inbox/internal/images"
	"github.com/jordanbyron/agent-inbox/internal/recipe"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/verbs"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

func newFormCommand(open inboxOpener) *cobra.Command {
	return &cobra.Command{
		Use:    "form",
		Short:  "Show the new-agent form and print what was chosen as JSON",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			inbox, err := open(cmd.Context())
			if err != nil {
				return err
			}
			req := verbs.StartRequest{Worktree: inbox.Config.Worktree.Default}
			pane, ok, err := rex.FindPane(cmd.Context(), inbox.Rex, inbox.Getenv)
			if err != nil {
				return err
			}
			if ok {
				req.Session = pane.SessionID
			}
			// The JSON goes to stdout, so it can be piped while the form shows.
			opts, err := formOptions(cmd.Context(), inbox, req, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			result, ok, err := form.Run(cmd.Context(), opts)
			if err != nil || !ok {
				return err
			}
			out := json.NewEncoder(cmd.OutOrStdout())
			out.SetIndent("", "  ")
			if err := out.Encode(result); err != nil {
				return fmt.Errorf("printing the result: %w", err)
			}
			return nil
		},
	}
}

// formOptions starts the form from what the request already says, so flags
// given to new pre-fill it. With no Dir it starts in the directory last
// chosen in the form, or the current one when that is gone.
func formOptions(ctx context.Context, inbox *verbs.Inbox, req verbs.StartRequest, output io.Writer) (form.Options, error) {
	listed, err := inbox.Rex.Sessions(ctx)
	if err != nil {
		return form.Options{}, fmt.Errorf("listing Rex sessions: %w", err)
	}
	var sessions []form.Session
	current := ""
	for _, s := range listed {
		sessions = append(sessions, form.Session{ID: s.ID, Label: s.Label})
		if req.Session != "" && (s.ID == req.Session || s.Label == req.Session) {
			current = s.ID
		}
	}
	dir, err := filepath.Abs(cmp.Or(req.Dir, lastDir(inbox), "."))
	if err != nil {
		return form.Options{}, fmt.Errorf("reading the directory: %w", err)
	}
	return form.Options{
		Recipes:         preferred(inbox.Config.Recipes, req),
		Sessions:        sessions,
		CurrentSession:  current,
		Dir:             dir,
		WorktreeDefault: req.Worktree,
		InRepo: func(dir string) bool {
			return worktree.InRepo(ctx, inbox.Git, dir)
		},
		Home:       inbox.Home,
		Now:        time.Now,
		Clipboard:  images.Pasteboard{},
		ImageDir:   filepath.Join(inbox.Store.Dir(), "images"),
		Output:     output,
		RecentDirs: recentDirs(ctx, inbox),
	}, nil
}

func lastDir(inbox *verbs.Inbox) string {
	st, err := inbox.Store.Read()
	if err != nil || st.LastDir == "" {
		return ""
	}
	if info, err := os.Stat(st.LastDir); err != nil || !info.IsDir() {
		return ""
	}
	return st.LastDir
}

// rememberDir keeps the directory chosen in the form for the next one.
func rememberDir(inbox *verbs.Inbox, dir string) error {
	err := inbox.Store.Update(func(st *state.State) error {
		st.LastDir = dir
		return nil
	})
	if err != nil {
		return fmt.Errorf("remembering the directory: %w", err)
	}
	return nil
}

// recentDirs lists where agents were started, most recent first. An agent
// in a worktree counts as its repository, since that is what was chosen.
func recentDirs(ctx context.Context, inbox *verbs.Inbox) []string {
	st, err := inbox.Store.Read()
	if err != nil {
		return nil
	}
	records := make([]state.Record, 0, len(st.Records))
	for _, r := range st.Records {
		records = append(records, r)
	}
	slices.SortFunc(records, func(a, b state.Record) int { return b.Began.Compare(a.Began) })
	var dirs []string
	for _, r := range records {
		dir := r.Dir
		if r.Worktree != "" {
			common, err := inbox.Git.Run(ctx, r.Worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
			if err != nil {
				continue
			}
			dir = filepath.Dir(strings.TrimSpace(common))
		}
		if dir != "" && !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// preferred puts the harness the request names first, and its model,
// effort and permissions first among their choices, since the form starts
// on whatever comes first.
func preferred(recipes recipe.Set, req verbs.StartRequest) recipe.Set {
	out := slices.Clone(recipes)
	if i := slices.IndexFunc(out, func(r recipe.Recipe) bool { return r.Name == req.Harness }); i > 0 {
		chosen := out[i]
		out = slices.Insert(slices.Delete(out, i, i+1), 0, chosen)
	}
	if len(out) == 0 {
		return out
	}
	choices := &out[0].Choices
	choices.Model = first(choices.Model, req.Model)
	choices.Effort = first(choices.Effort, req.Effort)
	choices.Permissions = first(choices.Permissions, req.Permissions)
	return out
}

func first(values []string, value string) []string {
	i := slices.Index(values, value)
	if i <= 0 {
		return values
	}
	return slices.Insert(slices.Delete(slices.Clone(values), i, i+1), 0, value)
}

// fromForm is what to start: the form's choices, with the name and --show
// from the flags when the form left them unset.
func fromForm(result form.Result, flags verbs.StartRequest) verbs.StartRequest {
	req := verbs.StartRequest{
		Prompt:      result.Prompt,
		Harness:     result.Harness,
		Name:        result.Name,
		Dir:         result.Dir,
		Session:     result.Session,
		Model:       result.Model,
		Effort:      result.Effort,
		Permissions: result.Permissions,
		Worktree:    result.Worktree,
		Show:        result.ShowTab || flags.Show,
	}
	if req.Name == "" {
		req.Name = flags.Name
	}
	return req
}

func terminalIn(cmd *cobra.Command) (*os.File, bool) {
	in, ok := cmd.InOrStdin().(*os.File)
	return in, ok && term.IsTerminal(in.Fd())
}

// holdOnError keeps a failed start on screen until Enter. The form may be
// in a Rex layer, which closes the moment this program exits.
func holdOnError(cmd *cobra.Command, in *os.File, err error) {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "agent-inbox: %v\npress Enter to close", err)
	_, _ = bufio.NewReader(in).ReadString('\n')
}
