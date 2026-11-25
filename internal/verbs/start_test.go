package verbs_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/rex/rextest"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/verbs"
)

type refusingRex struct {
	*rextest.Fake
}

func (refusingRex) Start(context.Context, rex.StartOptions) (rex.Started, error) {
	return rex.Started{}, errors.New("no room")
}

type refusingPark struct {
	*rextest.Fake
}

func (refusingPark) Park(context.Context, rex.ParkOptions) (string, error) {
	return "", errors.New("no room")
}

func TestStartInAWorktree(t *testing.T) {
	w := newWorld(t)
	repo := newRepo(t)

	a, err := w.inbox.Start(t.Context(), verbs.StartRequest{
		Prompt:   "Fix the login page",
		Harness:  "fake",
		Dir:      repo,
		Session:  "work",
		Model:    "opus",
		Worktree: true,
	})

	require.NoError(t, err)
	require.Len(t, a.Key, 6)
	require.Equal(t, "Fix the login page", a.Name)
	path := filepath.Join(repo, ".worktrees", "fix-login-page")
	rec := w.state(t).Records[a.Key]
	require.Equal(t, state.Record{
		Harness:    "fake",
		SessionID:  rec.SessionID,
		Name:       "Fix the login page",
		Dir:        path,
		Worktree:   path,
		Branch:     "fix-login-page",
		RexSession: w.session,
		Began:      began.Add(time.Hour),
	}, rec)
	require.Len(t, rec.SessionID, 36)
	require.Equal(t, "fix-login-page", git(t, path, "branch", "--show-current"))
	require.Equal(t, []rex.StartOptions{{
		SessionID:   w.session,
		Command:     []string{"fake", "--session-id", rec.SessionID, "--name", "Fix the login page", "--model", "opus", "--", "Fix the login page"},
		Dir:         path,
		BlockLabel:  "agent-inbox:" + a.Key,
		WindowLabel: "Fix the login page",
	}}, w.rex.Started())
	require.Empty(t, w.rex.Shown())
	require.Empty(t, w.rex.Parked(), "no parked command, so nothing parked")
}

func TestStartParksTheInboxOnTheLeftOfTheAgent(t *testing.T) {
	w := newWorld(t)
	w.inbox.Parked = []string{"/opt/agent-inbox"}

	a, err := w.inbox.Start(t.Context(), verbs.StartRequest{Prompt: "hi", Dir: t.TempDir(), Session: w.session, Show: true})

	require.NoError(t, err)
	parked := w.rex.Parked()
	require.Len(t, parked, 1)
	require.Equal(t, rex.ParkOptions{
		SessionID:     w.session,
		BlockID:       w.rex.Shown()[0],
		Command:       []string{"/opt/agent-inbox"},
		WidthFraction: verbs.ParkedWidth,
	}, parked[0])
	require.Equal(t, []string{verbs.BlockLabel(a.Key), rex.ParkedLabel}, w.labels(t))
}

func TestStartKeepsTheAgentWhenTheInboxCantPark(t *testing.T) {
	w := newWorld(t)
	w.inbox.Rex = refusingPark{w.rex}
	w.inbox.Parked = []string{"/opt/agent-inbox"}

	a, err := w.inbox.Start(t.Context(), verbs.StartRequest{Prompt: "hi", Dir: t.TempDir(), Session: w.session, Show: true})

	require.ErrorContains(t, err, "parking the inbox beside Hi: no room")
	require.True(t, a.Launched)
	require.Contains(t, w.state(t).Records, a.Key)
	require.Len(t, w.rex.Shown(), 1, "the tab is shown all the same")
}

func TestStartWithoutAWorktree(t *testing.T) {
	cases := []struct {
		name     string
		dir      func(t *testing.T) string
		worktree bool
	}{
		{name: "outside a repository", dir: tempDir, worktree: true},
		{name: "when not asked", dir: newRepo, worktree: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			dir := c.dir(t)

			a, err := w.inbox.Start(t.Context(), verbs.StartRequest{Prompt: "hi", Name: "n", Dir: dir, Worktree: c.worktree})

			require.NoError(t, err)
			rec := w.state(t).Records[a.Key]
			require.Equal(t, dir, rec.Dir)
			require.Empty(t, rec.Worktree)
			require.NoDirExists(t, filepath.Join(dir, ".worktrees"))
		})
	}
}

func TestStartInANewSessionAndShowIt(t *testing.T) {
	w := newWorld(t)

	a, err := w.inbox.Start(t.Context(), verbs.StartRequest{Prompt: "hi", Harness: "late", Dir: t.TempDir(), Show: true})

	require.NoError(t, err)
	rec := w.state(t).Records[a.Key]
	require.NotEqual(t, w.session, rec.RexSession)
	require.Empty(t, rec.SessionID, "late takes no session id at start")
	require.Equal(t, []string{"late", "--", "hi"}, w.rex.Started()[0].Command)
	require.Len(t, w.rex.Shown(), 1)
}

func TestStartRecordsTheAgentItRunsInAsTheParent(t *testing.T) {
	w := newWorld(t)
	block := w.launch(t, "mom", state.Record{}, true)
	w.env["REX_SESSION"], w.env["REX_BLOCK"] = w.session, block

	a, err := w.inbox.Start(t.Context(), verbs.StartRequest{Prompt: "help", Dir: t.TempDir(), Session: w.session})

	require.NoError(t, err)
	require.Equal(t, "mom", w.state(t).Records[a.Key].Parent)
}

func TestStartUndoesWhatItDidOnFailure(t *testing.T) {
	cases := []struct {
		name string
		repo func(t *testing.T) string
		rex  func(w *world) verbs.Rex
		want string
	}{
		{
			name: "the worktree can't be made",
			repo: func(t *testing.T) string {
				repo := tempDir(t)
				git(t, repo, "init", "--quiet")
				return repo
			},
			rex:  func(w *world) verbs.Rex { return w.rex },
			want: "has no commit to branch from",
		},
		{
			name: "Rex can't start it",
			repo: newRepo,
			rex:  func(w *world) verbs.Rex { return refusingRex{w.rex} },
			want: "no room",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			w.inbox.Rex = c.rex(w)
			repo := c.repo(t)

			_, err := w.inbox.Start(t.Context(), verbs.StartRequest{Prompt: "fix it", Dir: repo, Session: w.session, Worktree: true})

			require.ErrorContains(t, err, c.want)
			require.Empty(t, w.state(t).Records)
			require.NoDirExists(t, filepath.Join(repo, ".worktrees", "fix-it"))
			require.Empty(t, git(t, repo, "branch", "--list", "fix-it"))
			require.Empty(t, w.labels(t))
		})
	}
}

func TestStartRefuses(t *testing.T) {
	cases := []struct {
		name string
		req  verbs.StartRequest
		want string
	}{
		{name: "an unknown harness", req: verbs.StartRequest{Harness: "emacs", Dir: "."}, want: `no harness "emacs"`},
		{name: "an unknown session", req: verbs.StartRequest{Session: "elsewhere", Dir: "."}, want: `no Rex session "elsewhere"`},
		{name: "a missing directory", req: verbs.StartRequest{Dir: "/no/such/dir"}, want: "is not a directory"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			_, err := w.inbox.Start(t.Context(), c.req)
			require.ErrorContains(t, err, c.want)
			require.Empty(t, w.state(t).Records)
		})
	}
}

func TestStartLeavesAHarnessThatTitlesToNameItself(t *testing.T) {
	cases := []struct {
		name    string
		typed   string
		command []string
		label   string
	}{
		{
			name:    "no name typed",
			command: []string{"titled", "--", "Fix the login page"},
			label:   "Fix the login page",
		},
		{
			name:    "a name typed",
			typed:   "billing retries",
			command: []string{"titled", "--name", "billing retries", "--", "Fix the login page"},
			label:   "billing retries",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			a, err := w.inbox.Start(t.Context(), verbs.StartRequest{
				Prompt: "Fix the login page", Harness: "titled", Name: c.typed, Dir: t.TempDir(), Session: "work",
			})
			require.NoError(t, err)
			require.Equal(t, c.label, a.Name)
			require.Equal(t, c.command, w.rex.Started()[0].Command)
			require.Equal(t, c.label, w.rex.Started()[0].WindowLabel)
		})
	}
}
