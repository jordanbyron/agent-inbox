package verbs_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/recipe"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/rex/rextest"
	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
	"github.com/jordanbyron/agent-inbox/internal/verbs"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

var began = time.Date(2026, 11, 23, 9, 0, 0, 0, time.Local)

// world is an inbox over a fake Rex server with one session, "work", a
// temp state directory and a clock stopped an hour after began.
type world struct {
	inbox   *verbs.Inbox
	rex     *rextest.Fake
	store   *state.Store
	session string
	env     map[string]string
	snap    snapshot.Snapshot
	finder  *finder
	system  *system
}

type finder struct {
	id    string
	asked []recipe.SessionQuery
}

func (f *finder) FindSessionID(_ context.Context, q recipe.SessionQuery) (string, bool, error) {
	f.asked = append(f.asked, q)
	return f.id, f.id != "", nil
}

type system struct {
	installed []string
	ran       [][]string
	stdin     []string
}

func (s *system) Run(_ context.Context, stdin, name string, args ...string) error {
	s.ran = append(s.ran, append([]string{name}, args...))
	s.stdin = append(s.stdin, stdin)
	return nil
}

func (s *system) Has(name string) bool {
	return slices.Contains(s.installed, name)
}

func newWorld(t *testing.T) *world {
	t.Helper()
	isolateGit(t)
	fake := rextest.New(rex.Server{HostID: "host"})
	w := &world{
		rex:     fake,
		store:   state.New(t.TempDir()),
		session: fake.AddSession("work"),
		env:     map[string]string{},
		finder:  &finder{},
		system:  &system{},
	}
	now := began.Add(time.Hour)
	cfg := config.Default()
	cfg.Recipes = recipe.Set{
		{
			Name:   "fake",
			App:    "fake",
			Start:  []string{"fake", "--session-id", "{session_id}", "--name", "{name}", "--model", "{model}", "--", "{prompt}"},
			Resume: []string{"fake", "--resume", "{session_id}"},
		},
		{
			Name:         "late",
			App:          "late",
			Start:        []string{"late", "--", "{prompt}"},
			Resume:       []string{"late", "resume", "{session_id}"},
			ResumeLatest: []string{"late", "resume", "--last"},
			Sessions:     w.finder,
		},
	}
	w.inbox = &verbs.Inbox{
		Store:    w.store,
		Rex:      fake,
		Config:   cfg,
		Git:      worktree.ExecGit{},
		System:   w.system,
		Now:      func() time.Time { return now },
		Snapshot: func() (snapshot.Snapshot, error) { return w.snap, nil },
		Getenv:   func(key string) string { return w.env[key] },
		Home:     "/home/test",
	}
	return w
}

// launch records a launched agent and, with running, gives it a tab of its
// own in the work session. It returns the block id, or "".
func (w *world) launch(t *testing.T, id string, rec state.Record, running bool) string {
	t.Helper()
	if rec.Harness == "" {
		rec.Harness = "fake"
	}
	if rec.Name == "" {
		rec.Name = id
	}
	if rec.Dir == "" {
		rec.Dir = t.TempDir()
	}
	rec.RexSession = w.session
	rec.Began = began
	require.NoError(t, w.store.Update(func(st *state.State) error {
		st.Records[id] = rec
		return nil
	}))
	if !running {
		return ""
	}
	return w.tab(t, rec.Name, verbs.BlockLabel(id))
}

func (w *world) tab(t *testing.T, name, label string) string {
	t.Helper()
	window, err := w.rex.AddWindow(w.session, name)
	require.NoError(t, err)
	block, err := w.rex.AddBlock(w.session, window, label)
	require.NoError(t, err)
	return block
}

// find makes a block running a Claude that agent-inbox didn't start, listed
// in the snapshot as the watcher would list it.
func (w *world) find(t *testing.T) string {
	t.Helper()
	block := w.tab(t, "claude", "")
	w.snap = snapshot.New("test", map[triage.Section][]snapshot.Row{
		triage.SectionActive: {{Key: block, Label: "claude", RexSession: w.session, RexBlock: block}},
	}, snapshot.SortDefault)
	return block
}

func (w *world) state(t *testing.T) *state.State {
	t.Helper()
	st, err := w.store.Read()
	require.NoError(t, err)
	return st
}

func (w *world) entry(t *testing.T, key string) triage.Entry {
	t.Helper()
	return w.state(t).Entries[key]
}

func (w *world) labels(t *testing.T) []string {
	t.Helper()
	view, err := w.rex.View(t.Context(), w.session)
	require.NoError(t, err)
	var labels []string
	for _, window := range view.Windows {
		for _, b := range window.Blocks {
			labels = append(labels, b.Label)
		}
	}
	return labels
}

func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", tempDir(t))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

// tempDir is t.TempDir with symlinks resolved, as git reports paths.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo := tempDir(t)
	git(t, repo, "init", "--quiet", "--initial-branch=main")
	git(t, repo, "commit", "--quiet", "--allow-empty", "-m", "first")
	return repo
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := worktree.ExecGit{}.Run(t.Context(), dir, args...)
	require.NoError(t, err)
	return out
}

func TestResolvingTheTarget(t *testing.T) {
	cases := []struct {
		name   string
		target func(w *world, block string) verbs.Target
		env    bool
	}{
		{name: "by key", target: func(*world, string) verbs.Target { return verbs.Target{Key: "a1"} }},
		{name: "by block", target: func(_ *world, block string) verbs.Target { return verbs.Target{Block: block} }},
		{name: "from the environment", target: func(*world, string) verbs.Target { return verbs.Target{} }, env: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			block := w.launch(t, "a1", state.Record{Name: "fix-login"}, true)
			if c.env {
				w.env["REX_SESSION"], w.env["REX_BLOCK"] = w.session, block
			}

			a, err := w.inbox.Alias(t.Context(), c.target(w, block), "login")

			require.NoError(t, err)
			require.Equal(t, verbs.Agent{Key: "a1", Name: "login", Launched: true}, a)
			require.Equal(t, "login", w.entry(t, "a1").Alias)
		})
	}
}

func TestATargetedChildActsOnItsRoot(t *testing.T) {
	w := newWorld(t)
	w.launch(t, "root", state.Record{}, true)
	w.launch(t, "kid", state.Record{Parent: "root"}, true)
	grandchild := w.launch(t, "grandkid", state.Record{Parent: "kid"}, true)

	_, err := w.inbox.Snooze(t.Context(), verbs.Target{Block: grandchild}, verbs.Snooze{UntilWoken: true})

	require.NoError(t, err)
	st := w.state(t)
	require.True(t, st.Entries["root"].UntilWoken)
	require.NotContains(t, st.Entries, "kid")
	require.NotContains(t, st.Entries, "grandkid")
}

func TestAFoundAgentIsTargetedByItsBlock(t *testing.T) {
	w := newWorld(t)
	block := w.find(t)

	a, _, err := w.inbox.TogglePin(t.Context(), verbs.Target{Block: block})

	require.NoError(t, err)
	require.Equal(t, verbs.Agent{Key: block, Name: "claude"}, a)
	require.True(t, w.entry(t, block).Pinned())
}

func TestATargetThatIsNotAnAgent(t *testing.T) {
	cases := []struct {
		name   string
		target func(w *world, t *testing.T) verbs.Target
		env    bool
		want   string
	}{
		{
			name:   "a plain block",
			target: func(w *world, t *testing.T) verbs.Target { return verbs.Target{Block: w.tab(t, "zsh", "shell")} },
			want:   "is not an agent",
		},
		{
			name: "a label for a deleted agent",
			target: func(w *world, t *testing.T) verbs.Target {
				return verbs.Target{Block: w.tab(t, "old", verbs.BlockLabel("gone"))}
			},
			want: "is not an agent",
		},
		{
			name: "the pane it runs from",
			target: func(w *world, t *testing.T) verbs.Target {
				w.env["REX_SESSION"], w.env["REX_BLOCK"] = w.session, w.tab(t, "zsh", "")
				return verbs.Target{}
			},
			want: "is not an agent",
		},
		{
			name:   "an unknown key",
			target: func(*world, *testing.T) verbs.Target { return verbs.Target{Key: "nope"} },
			want:   `no agent "nope"`,
		},
		{
			name:   "nothing, outside Rex",
			target: func(*world, *testing.T) verbs.Target { return verbs.Target{} },
			want:   "no agent given",
		},
		{
			name:   "both a key and a block",
			target: func(*world, *testing.T) verbs.Target { return verbs.Target{Key: "a1", Block: "block:1"} },
			want:   "not both",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)

			_, _, err := w.inbox.TogglePin(t.Context(), c.target(w, t))

			require.ErrorContains(t, err, c.want)
			require.Empty(t, w.state(t).Entries)
		})
	}
}
