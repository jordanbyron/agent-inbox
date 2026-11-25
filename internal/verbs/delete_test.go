package verbs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
	"github.com/jordanbyron/agent-inbox/internal/verbs"
)

func startFamily(t *testing.T, w *world) (repo, rootID, kidID string) {
	t.Helper()
	repo = newRepo(t)
	start := func(prompt string) string {
		a, err := w.inbox.Start(t.Context(), verbs.StartRequest{Prompt: prompt, Dir: repo, Session: w.session, Worktree: true})
		require.NoError(t, err)
		return a.Key
	}
	rootID = start("the root")
	view, err := w.rex.View(t.Context(), w.session)
	require.NoError(t, err)
	w.env["REX_SESSION"], w.env["REX_BLOCK"] = w.session, view.Windows[0].Blocks[0].ID
	kidID = start("the kid")
	delete(w.env, "REX_BLOCK")
	require.NoError(t, w.store.Update(func(st *state.State) error {
		st.Entries[rootID] = triage.Entry{Alias: "family"}
		st.PullRequests[kidID] = []state.PullRequest{{URL: "https://github.com/o/r/pull/1"}}
		return nil
	}))
	return repo, rootID, kidID
}

func TestDeleteRemovesTheWholeFamily(t *testing.T) {
	w := newWorld(t)
	repo, _, kid := startFamily(t, w)
	w.tab(t, "zsh", "shell")

	a, members, err := w.inbox.Delete(t.Context(), verbs.Target{Key: kid})

	require.NoError(t, err)
	require.Equal(t, "family", a.Name)
	require.Equal(t, 2, members)
	require.Equal(t, []string{"shell"}, w.labels(t))
	st := w.state(t)
	require.Empty(t, st.Records)
	require.Empty(t, st.Entries)
	require.Empty(t, st.PullRequests)
	require.NoDirExists(t, filepath.Join(repo, ".worktrees", "the-root"))
	require.NoDirExists(t, filepath.Join(repo, ".worktrees", "the-kid"))
	require.Equal(t, "main", git(t, repo, "branch", "--format=%(refname:short)"))
}

func TestDeleteRefusesAWorktreeWithWorkInIt(t *testing.T) {
	cases := []struct {
		name string
		make func(t *testing.T, path string)
		want string
	}{
		{
			name: "uncommitted changes",
			make: func(t *testing.T, path string) {
				require.NoError(t, os.WriteFile(filepath.Join(path, "new.txt"), []byte("x"), 0o644))
			},
			want: "has uncommitted changes",
		},
		{
			name: "an unpushed commit",
			make: func(t *testing.T, path string) {
				git(t, path, "commit", "--quiet", "--allow-empty", "-m", "mine")
			},
			want: "has commits that are on no other branch",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			repo, root, kid := startFamily(t, w)
			kidPath := filepath.Join(repo, ".worktrees", "the-kid")
			c.make(t, kidPath)
			before := w.state(t)

			_, _, err := w.inbox.Delete(t.Context(), verbs.Target{Key: root})

			require.ErrorContains(t, err, "worktree "+kidPath+" "+c.want)
			require.Equal(t, before, w.state(t))
			require.Equal(t, []string{verbs.BlockLabel(root), verbs.BlockLabel(kid)}, w.labels(t))
			require.DirExists(t, filepath.Join(repo, ".worktrees", "the-root"))
			require.DirExists(t, kidPath)
		})
	}
}

func TestDeleteAFoundAgent(t *testing.T) {
	w := newWorld(t)
	block := w.find(t)
	_, _, err := w.inbox.TogglePin(t.Context(), verbs.Target{Block: block})
	require.NoError(t, err)

	_, _, err = w.inbox.Delete(t.Context(), verbs.Target{Block: block})

	require.NoError(t, err)
	require.Empty(t, w.labels(t))
	require.Empty(t, w.state(t).Entries)
}
