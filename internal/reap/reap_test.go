package reap_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/reap"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/rex/rextest"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
	"github.com/jordanbyron/agent-inbox/internal/verbs"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

var now = time.Date(2026, 12, 20, 12, 0, 0, 0, time.UTC)

func settledFor(d time.Duration) triage.Entry {
	return triage.Entry{SettledAt: now.Add(-d)}
}

func TestDue(t *testing.T) {
	old := settledFor(reap.After + time.Hour)
	recent := settledFor(reap.After - time.Hour)
	cases := []struct {
		name    string
		records map[string]state.Record
		entries map[string]triage.Entry
		want    []reap.Family
	}{
		{
			name:    "settled for 14 days",
			records: map[string]state.Record{"a1": {Name: "fix-login"}},
			entries: map[string]triage.Entry{"a1": settledFor(reap.After)},
			want:    []reap.Family{{Key: "a1", Name: "fix-login", LastSettledAt: now.Add(-reap.After), Members: 1}},
		},
		{
			name:    "settled for less",
			records: map[string]state.Record{"a1": {Name: "fix-login"}},
			entries: map[string]triage.Entry{"a1": recent},
		},
		{
			name:    "never settled",
			records: map[string]state.Record{"a1": {Name: "fix-login"}},
		},
		{
			name:    "pinned",
			records: map[string]state.Record{"a1": {Name: "fix-login"}},
			entries: map[string]triage.Entry{"a1": {SettledAt: old.SettledAt, PinnedAt: now.Add(-reap.After * 2)}},
		},
		{
			name:    "snoozed",
			records: map[string]state.Record{"a1": {Name: "fix-login"}},
			entries: map[string]triage.Entry{"a1": {SettledAt: old.SettledAt, UntilWoken: true}},
		},
		{
			name:    "a found agent has no record",
			entries: map[string]triage.Entry{"block:1": old},
		},
		{
			name:    "a family with a child still running",
			records: map[string]state.Record{"a1": {Name: "root"}, "a2": {Name: "kid", Parent: "a1"}},
			entries: map[string]triage.Entry{"a1": old},
		},
		{
			name:    "a family whose child settled lately",
			records: map[string]state.Record{"a1": {Name: "root"}, "a2": {Name: "kid", Parent: "a1"}},
			entries: map[string]triage.Entry{"a1": old, "a2": recent},
		},
		{
			name:    "a whole family, named by its alias, oldest first",
			records: map[string]state.Record{"a1": {Name: "root"}, "a2": {Name: "kid", Parent: "a1"}, "b1": {Name: "older"}},
			entries: map[string]triage.Entry{
				"a1": {SettledAt: old.SettledAt, Alias: "login"},
				"a2": old,
				"b1": settledFor(reap.After * 2),
			},
			want: []reap.Family{
				{Key: "b1", Name: "older", LastSettledAt: now.Add(-reap.After * 2), Members: 1},
				{Key: "a1", Name: "login", LastSettledAt: old.SettledAt, Members: 2},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := &state.State{Records: c.records, Entries: c.entries}
			if st.Records == nil {
				st.Records = map[string]state.Record{}
			}
			if st.Entries == nil {
				st.Entries = map[string]triage.Entry{}
			}

			require.Equal(t, c.want, reap.Due(st, now))
		})
	}
}

func inboxOverRealGit(t *testing.T) *verbs.Inbox {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	return &verbs.Inbox{
		Store:  state.New(t.TempDir()),
		Rex:    rextest.New(rex.Server{HostID: "host"}),
		Config: config.Default(),
		Git:    worktree.ExecGit{},
		Now:    func() time.Time { return now },
		Getenv: func(string) string { return "" },
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	_, err := worktree.ExecGit{}.Run(t.Context(), dir, args...)
	require.NoError(t, err)
}

func settledAgentInWorktree(t *testing.T, in *verbs.Inbox, repo, id string) string {
	t.Helper()
	path := filepath.Join(repo, ".worktrees", id)
	git(t, repo, "worktree", "add", "--quiet", "-b", id, path)
	require.NoError(t, in.Store.Update(func(st *state.State) error {
		st.Records[id] = state.Record{Harness: "claude", Name: id, Dir: path, Worktree: path, Branch: id}
		st.Entries[id] = settledFor(reap.After + time.Hour)
		return nil
	}))
	return path
}

func TestSweepDeletesWhatIsDueAndKeepsWhatRefuses(t *testing.T) {
	cases := []struct {
		name string
		work func(t *testing.T, path string)
		want string
	}{
		{
			name: "uncommitted changes",
			work: func(t *testing.T, path string) {
				require.NoError(t, os.WriteFile(filepath.Join(path, "new.txt"), []byte("x"), 0o600))
			},
			want: "has uncommitted changes",
		},
		{
			name: "an unpushed commit",
			work: func(t *testing.T, path string) { git(t, path, "commit", "--quiet", "--allow-empty", "-m", "mine") },
			want: "has commits that are on no other branch",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := inboxOverRealGit(t)
			repo, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			git(t, repo, "init", "--quiet", "--initial-branch=main")
			git(t, repo, "commit", "--quiet", "--allow-empty", "-m", "first")
			clean := settledAgentInWorktree(t, in, repo, "clean")
			busy := settledAgentInWorktree(t, in, repo, "busy")
			c.work(t, busy)
			reaper := &reap.Reaper{Deleter: in}
			st, err := in.Store.Read()
			require.NoError(t, err)

			results := reaper.Sweep(t.Context(), st, now)

			require.Len(t, results, 2)
			require.Equal(t, "busy", results[0].Key)
			require.ErrorContains(t, results[0].Err, "worktree "+busy+" "+c.want)
			require.Equal(t, "clean", results[1].Key)
			require.NoError(t, results[1].Err)
			require.NoDirExists(t, clean)
			require.DirExists(t, busy)
			st, err = in.Store.Read()
			require.NoError(t, err)
			require.Contains(t, st.Records, "busy")
			require.NotContains(t, st.Records, "clean")

			require.Empty(t, reaper.Sweep(t.Context(), st, now.Add(reap.RetryAfter-time.Minute)), "it backs off")
			again := reaper.Sweep(t.Context(), st, now.Add(reap.RetryAfter))
			require.Len(t, again, 1)
			require.Error(t, again[0].Err)
		})
	}
}

// The sweep picks families from a state it read earlier, so a family
// changed since must survive it.
func TestSweepKeepsAFamilyThatChangedSinceItLooked(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, in *verbs.Inbox)
	}{
		{
			name: "restored",
			change: func(t *testing.T, in *verbs.Inbox) {
				startTab(t, in, "a1")
				require.NoError(t, in.Store.Update(func(st *state.State) error {
					e := st.Entries["a1"]
					e.Wake(triage.Agent{}, now)
					st.Entries["a1"] = e
					return nil
				}))
			},
		},
		{
			name:   "restoring, with its tab open before its state is written",
			change: func(t *testing.T, in *verbs.Inbox) { startTab(t, in, "a1") },
		},
		{
			name: "pinned",
			change: func(t *testing.T, in *verbs.Inbox) {
				_, _, err := in.TogglePin(t.Context(), verbs.Target{Key: "a1"})
				require.NoError(t, err)
			},
		},
		{
			name: "snoozed",
			change: func(t *testing.T, in *verbs.Inbox) {
				_, err := in.Snooze(t.Context(), verbs.Target{Key: "a1"}, verbs.Snooze{UntilWoken: true})
				require.NoError(t, err)
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := inboxOverRealGit(t)
			repo, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			git(t, repo, "init", "--quiet", "--initial-branch=main")
			git(t, repo, "commit", "--quiet", "--allow-empty", "-m", "first")
			path := settledAgentInWorktree(t, in, repo, "a1")
			st, err := in.Store.Read()
			require.NoError(t, err)
			c.change(t, in)

			results := (&reap.Reaper{Deleter: in}).Sweep(t.Context(), st, now)

			require.Len(t, results, 1)
			require.Error(t, results[0].Err)
			require.DirExists(t, path)
			fake, ok := in.Rex.(*rextest.Fake)
			require.True(t, ok)
			require.Empty(t, fake.ClosedWindows())
			require.Empty(t, fake.ClosedBlocks())
			after, err := in.Store.Read()
			require.NoError(t, err)
			require.Contains(t, after.Records, "a1")
		})
	}
}

// startTab gives agent id a tab of its own, as a restore does before it
// writes the state.
func startTab(t *testing.T, in *verbs.Inbox, id string) {
	t.Helper()
	_, err := in.Rex.Start(t.Context(), rex.StartOptions{BlockLabel: verbs.BlockLabel(id), WindowLabel: id})
	require.NoError(t, err)
}
