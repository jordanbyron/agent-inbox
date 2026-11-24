package cli_test

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/cli"
	"github.com/jordanbyron/agent-inbox/internal/form"
	"github.com/jordanbyron/agent-inbox/internal/recipe"
	"github.com/jordanbyron/agent-inbox/internal/verbs"
)

func TestFlagsPrefillTheForm(t *testing.T) {
	f := newFixture(t)
	f.inbox.Config.Recipes = recipe.Builtins()
	other := f.rex.AddSession("other")
	dir := t.TempDir()

	opts, err := cli.FormOptions(t.Context(), f.inbox, verbs.StartRequest{
		Harness: "codex", Model: "gpt-5.6-terra", Permissions: "danger-full-access",
		Dir: dir, Session: "other", Worktree: false,
	}, io.Discard)

	require.NoError(t, err)
	require.Equal(t, []form.Session{{ID: f.session, Label: "work"}, {ID: other, Label: "other"}}, opts.Sessions)
	require.Equal(t, other, opts.CurrentSession, "a session given by label starts chosen")
	require.Equal(t, dir, opts.Dir)
	require.False(t, opts.WorktreeDefault)
	require.Equal(t, filepath.Join(f.store.Dir(), "images"), opts.ImageDir)
	codex := opts.Recipes[0]
	require.Equal(t, "codex", codex.Name, "the harness given comes first, where the form starts")
	require.Equal(t, "gpt-5.6-terra", codex.Choices.Model[0])
	require.Equal(t, "default", codex.Choices.Effort[0])
	require.Equal(t, "danger-full-access", codex.Choices.Permissions[0])
	require.Equal(t, "default", recipe.Builtins()[1].Choices.Model[0], "the built-ins are left as they were")
	require.Len(t, opts.Recipes, len(recipe.Builtins()))
}

func TestTheFormStartsInThePanesSession(t *testing.T) {
	f := newFixture(t)

	opts, err := cli.FormOptions(t.Context(), f.inbox, verbs.StartRequest{Dir: ".", Session: f.session}, io.Discard)

	require.NoError(t, err)
	require.Equal(t, f.session, opts.CurrentSession)
	require.Equal(t, "fake", opts.Recipes[0].Name)
}

func TestTheFormsChoicesAreWhatStarts(t *testing.T) {
	result := form.Result{
		Prompt: "fix it", Harness: "codex", Dir: "/r/app", Session: "session:1",
		Model: "gpt-6-luna", Effort: "high", Permissions: "danger-full-access", Worktree: true,
	}

	req := cli.FromForm(result, verbs.StartRequest{Name: "from-flag", Show: true, Harness: "claude"})

	require.Equal(t, verbs.StartRequest{
		Prompt: "fix it", Harness: "codex", Name: "from-flag", Dir: "/r/app", Session: "session:1",
		Model: "gpt-6-luna", Effort: "high", Permissions: "danger-full-access", Worktree: true, Show: true,
	}, req)

	result.Name, result.ShowTab = "typed", true
	req = cli.FromForm(result, verbs.StartRequest{})
	require.Equal(t, "typed", req.Name)
	require.True(t, req.Show)
}

func TestTheFormKeepsTheListsConfigAndState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/scratch/config")
	t.Setenv("XDG_STATE_HOME", "/scratch/state")

	require.Equal(t, []string{"env", "XDG_CONFIG_HOME=/scratch/config", "XDG_STATE_HOME=/scratch/state", "/bin/agent-inbox", "new"},
		cli.FormCommand("/bin/agent-inbox"))
}
