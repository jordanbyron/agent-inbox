package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/cli"
	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/recipe"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/rex/rextest"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/verbs"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

var now = time.Date(2026, 11, 23, 10, 0, 0, 0, time.Local)

type fixture struct {
	inbox   *verbs.Inbox
	rex     *rextest.Fake
	store   *state.Store
	session string
	env     map[string]string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	fake := rextest.New(rex.Server{HostID: "host"})
	f := &fixture{
		rex:     fake,
		store:   state.New(t.TempDir()),
		session: fake.AddSession("work"),
		env:     map[string]string{},
	}
	cfg := config.Default()
	cfg.Recipes = recipe.Set{{
		Name:   "fake",
		App:    "fake",
		Start:  []string{"fake", "--session-id", "{session_id}", "--", "{prompt}"},
		Resume: []string{"fake", "--resume", "{session_id}"},
	}}
	f.inbox = &verbs.Inbox{
		Store:  f.store,
		Rex:    fake,
		Config: cfg,
		Git:    worktree.ExecGit{},
		Now:    func() time.Time { return now },
		Getenv: func(key string) string { return f.env[key] },
	}
	return f
}

func (f *fixture) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := cli.NewWithInbox(f.inbox)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return strings.TrimSpace(out.String()), err
}

func (f *fixture) must(t *testing.T, args ...string) string {
	t.Helper()
	out, err := f.run(t, args...)
	require.NoError(t, err)
	return out
}

func (f *fixture) windows(t *testing.T) []rex.Window {
	t.Helper()
	view, err := f.rex.View(t.Context(), f.session)
	require.NoError(t, err)
	return view.Windows
}

func TestTheVerbsEndToEnd(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()

	out := f.must(t, "new", "--prompt", "fix the login", "--cwd", dir, "--session", "work")

	st, err := f.store.Read()
	require.NoError(t, err)
	require.Len(t, st.Records, 1)
	var id string
	for key := range st.Records {
		id = key
	}
	require.Equal(t, "started fix-the-login ("+id+")", out)
	require.Len(t, f.windows(t), 1)
	require.Equal(t, "fix-the-login", f.windows(t)[0].Label)
	require.Equal(t, verbs.BlockLabel(id), f.windows(t)[0].Blocks[0].Label)

	require.Equal(t, "settled fix-the-login", f.must(t, "settle", id))
	require.Empty(t, f.windows(t))

	require.Equal(t, "restored fix-the-login", f.must(t, "restore", id))
	require.Len(t, f.windows(t), 1)
	require.Equal(t, []string{"fake", "--resume", st.Records[id].SessionID}, f.rex.Started()[1].Command)

	require.Equal(t, "snoozed fix-the-login until Mon Nov 23 11:00", f.must(t, "snooze", id, "1h"))
	require.Equal(t, "woke fix-the-login", f.must(t, "wake", id))
	require.Equal(t, "pinned fix-the-login", f.must(t, "pin", id))
	require.Equal(t, "aliased "+id+" to login", f.must(t, "alias", id, "login"))
	require.Equal(t, "linked login to https://github.com/o/r/pull/4", f.must(t, "link", id, "o/r#4"))
	require.Equal(t, "cleared the link of login", f.must(t, "link", id, ""))

	st, err = f.store.Read()
	require.NoError(t, err)
	e := st.Entries[id]
	require.False(t, e.Snoozed(now))
	require.True(t, e.Pinned())
	require.Equal(t, "login", e.Alias)

	require.Equal(t, "deleted login", f.must(t, "delete", id, "--yes"))
	require.Empty(t, f.windows(t))
	st, err = f.store.Read()
	require.NoError(t, err)
	require.Empty(t, st.Records)
	require.Empty(t, st.Entries)
}

func TestNewInAWorktree(t *testing.T) {
	f := newFixture(t)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	git := worktree.ExecGit{}
	_, err = git.Run(t.Context(), repo, "init", "--quiet", "--initial-branch=main")
	require.NoError(t, err)
	_, err = git.Run(t.Context(), repo, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "--quiet", "--allow-empty", "-m", "first")
	require.NoError(t, err)

	f.must(t, "new", "--prompt", "hello", "--cwd", repo, "--session", "work")
	f.must(t, "new", "--prompt", "hello again", "--cwd", repo, "--session", "work", "--worktree=false")

	started := f.rex.Started()
	require.Equal(t, filepath.Join(repo, ".worktrees", "hello"), started[0].Dir)
	require.Equal(t, repo, started[1].Dir)
}

func TestTheVerbsFindTheAgentByBlock(t *testing.T) {
	f := newFixture(t)
	f.must(t, "new", "--prompt", "hi", "--cwd", t.TempDir(), "--session", "work")
	block := f.windows(t)[0].Blocks[0].ID

	require.Equal(t, "pinned hi", f.must(t, "pin", "--block", block))

	f.env["REX_SESSION"], f.env["REX_BLOCK"] = f.session, block
	require.Equal(t, "snoozed hi until woken", f.must(t, "snooze", "until-woken"))
	require.Equal(t, "aliased "+strings.TrimPrefix(f.windows(t)[0].Blocks[0].Label, verbs.LabelPrefix)+" to me", f.must(t, "alias", "me"))
}

func TestNewStartsInThePanesSession(t *testing.T) {
	f := newFixture(t)
	f.env["REX_SESSION"], f.env["REX_BLOCK"] = f.session, "block:elsewhere"

	f.must(t, "new", "--prompt", "hi", "--cwd", t.TempDir())

	require.Equal(t, f.session, f.rex.Started()[0].SessionID)
}

func TestTheVerbsFail(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "new without a prompt", args: []string{"new"}, want: "new needs --prompt"},
		{name: "a block that is not an agent", args: []string{"settle", "--block", "block:nope"}, want: "block block:nope is not an agent"},
		{name: "an unknown agent", args: []string{"wake", "nope"}, want: `no agent "nope"`},
		{name: "no agent outside Rex", args: []string{"pin"}, want: "no agent given"},
		{name: "a snooze it can't read", args: []string{"snooze", "nope", "someday"}, want: `can't read "someday"`},
		{name: "delete off a terminal", args: []string{"delete", "nope"}, want: "pass --yes"},
		{name: "too many arguments", args: []string{"alias", "a", "b", "c"}, want: "accepts between 1 and 2 arg(s)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			out, err := f.run(t, c.args...)
			require.ErrorContains(t, err, c.want)
			require.Empty(t, out)
		})
	}
}
