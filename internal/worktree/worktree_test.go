package worktree_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

var git = worktree.ExecGit{}

func TestCreateFromOriginsDefaultBranch(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) string
	}{
		{name: "a clone", setup: cloneWithUnpushedCommit},
		{name: "a remote added by hand", setup: remoteAddedWithUnpushedCommit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := c.setup(t)

			wt, err := worktree.Create(t.Context(), git, repo, "fix-login", defaults())

			require.NoError(t, err)
			require.Equal(t, worktree.Worktree{Path: filepath.Join(repo, ".worktrees", "fix-login"), Branch: "fix-login"}, wt)
			require.Equal(t, "fix-login", run(t, wt.Path, "branch", "--show-current"))
			require.Equal(t, run(t, repo, "rev-parse", "origin/main"), run(t, wt.Path, "rev-parse", "HEAD"))
			_, err = git.Run(t.Context(), wt.Path, "rev-parse", "--abbrev-ref", "@{upstream}")
			require.Error(t, err, "the branch must not track origin/main")
			require.Empty(t, run(t, repo, "status", "--porcelain"))
		})
	}
}

func TestCreateFromHead(t *testing.T) {
	repo := cloneWithUnpushedCommit(t)
	cfg := defaults()
	cfg.Base = config.BaseHead

	wt, err := worktree.Create(t.Context(), git, repo, "fix-login", cfg)

	require.NoError(t, err)
	require.Equal(t, run(t, repo, "rev-parse", "HEAD"), run(t, wt.Path, "rev-parse", "HEAD"))
}

func TestCreateWithoutOriginUsesHead(t *testing.T) {
	repo := newRepo(t)
	commit(t, repo, "first")

	wt, err := worktree.Create(t.Context(), git, repo, "fix-login", defaults())

	require.NoError(t, err)
	require.Equal(t, run(t, repo, "rev-parse", "HEAD"), run(t, wt.Path, "rev-parse", "HEAD"))
}

func TestCreateFromAnotherWorktree(t *testing.T) {
	repo := newRepo(t)
	commit(t, repo, "first")
	parent, err := worktree.Create(t.Context(), git, repo, "parent", defaults())
	require.NoError(t, err)

	child, err := worktree.Create(t.Context(), git, parent.Path, "child", defaults())

	require.NoError(t, err)
	require.Equal(t, filepath.Join(repo, ".worktrees", "child"), child.Path)
}

func TestCreateSuffixesATakenName(t *testing.T) {
	cases := []struct {
		name string
		take func(t *testing.T, repo string)
		want string
	}{
		{
			name: "a taken branch",
			take: func(t *testing.T, repo string) { run(t, repo, "branch", "fix-login") },
			want: "fix-login-2",
		},
		{
			name: "a taken path",
			take: func(t *testing.T, repo string) {
				require.NoError(t, os.MkdirAll(filepath.Join(repo, ".worktrees", "fix-login"), 0o755))
			},
			want: "fix-login-2",
		},
		{
			name: "two taken",
			take: func(t *testing.T, repo string) {
				_, err := worktree.Create(t.Context(), git, repo, "fix-login", defaults())
				require.NoError(t, err)
				run(t, repo, "branch", "fix-login-2")
			},
			want: "fix-login-3",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := newRepo(t)
			commit(t, repo, "first")
			c.take(t, repo)

			wt, err := worktree.Create(t.Context(), git, repo, "fix-login", defaults())

			require.NoError(t, err)
			require.Equal(t, worktree.Worktree{Path: filepath.Join(repo, ".worktrees", c.want), Branch: c.want}, wt)
		})
	}
}

func TestCreateExcludesAPathInsideTheRepositoryOnce(t *testing.T) {
	repo := newRepo(t)
	commit(t, repo, "first")
	exclude := filepath.Join(repo, ".git", "info", "exclude")
	require.NoError(t, os.WriteFile(exclude, []byte("*.log"), 0o644))

	for _, name := range []string{"one", "two"} {
		_, err := worktree.Create(t.Context(), git, repo, name, defaults())
		require.NoError(t, err)
	}

	content, err := os.ReadFile(exclude)
	require.NoError(t, err)
	require.Equal(t, "*.log\n/.worktrees/\n", string(content))
	require.Empty(t, run(t, repo, "status", "--porcelain"))
}

func TestCreateOutsideTheRepositoryExcludesNothing(t *testing.T) {
	repo := newRepo(t)
	commit(t, repo, "first")
	exclude := filepath.Join(repo, ".git", "info", "exclude")
	before, err := os.ReadFile(exclude)
	require.NoError(t, err)
	cfg := defaults()
	cfg.Path = "{repo}-worktrees/{name}"
	cfg.Branch = "agent/{name}"

	wt, err := worktree.Create(t.Context(), git, repo, "fix-login", cfg)

	require.NoError(t, err)
	require.Equal(t, worktree.Worktree{Path: repo + "-worktrees/fix-login", Branch: "agent/fix-login"}, wt)
	after, err := os.ReadFile(exclude)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
}

func TestCreateFailureLeavesNothing(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, repo string) config.Worktree
	}{
		{
			name: "a bad branch name",
			setup: func(*testing.T, string) config.Worktree {
				cfg := defaults()
				cfg.Branch = "bad..{name}"
				return cfg
			},
		},
		{
			name: "an exclude file that cannot be written",
			setup: func(t *testing.T, repo string) config.Worktree {
				require.NoError(t, os.Chmod(filepath.Join(repo, ".git", "info", "exclude"), 0o444))
				return defaults()
			},
		},
		{
			name: "git failing part way through",
			setup: func(t *testing.T, repo string) config.Worktree {
				exclude := filepath.Join(repo, ".git", "info", "exclude")
				require.NoError(t, os.Remove(exclude))
				require.NoError(t, os.Mkdir(exclude, 0o755))
				return defaults()
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := newRepo(t)
			commit(t, repo, "first")
			cfg := c.setup(t, repo)

			_, err := worktree.Create(t.Context(), git, repo, "fix-login", cfg)

			require.Error(t, err)
			require.Equal(t, "main", run(t, repo, "branch", "--format=%(refname:short)"))
			require.Len(t, strings.Split(run(t, repo, "worktree", "list", "--porcelain"), "\n\n"), 1)
			require.NoDirExists(t, filepath.Join(repo, ".worktrees", "fix-login"))
		})
	}
}

func TestRemove(t *testing.T) {
	repo := cloneWithUnpushedCommit(t)
	wt, err := worktree.Create(t.Context(), git, repo, "fix-login", defaults())
	require.NoError(t, err)
	commit(t, wt.Path, "work")
	run(t, wt.Path, "push", "--quiet", "origin", "fix-login")

	require.NoError(t, worktree.Remove(t.Context(), git, wt.Path, wt.Branch))

	require.NoDirExists(t, wt.Path)
	require.Equal(t, "main", run(t, repo, "branch", "--format=%(refname:short)"))
}

func TestRemoveWithoutARemoteOnceMerged(t *testing.T) {
	repo := newRepo(t)
	commit(t, repo, "first")
	wt, err := worktree.Create(t.Context(), git, repo, "fix-login", defaults())
	require.NoError(t, err)

	require.NoError(t, worktree.Remove(t.Context(), git, wt.Path, wt.Branch))

	require.NoDirExists(t, wt.Path)
}

func TestRemoveRefuses(t *testing.T) {
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
			name: "a commit on no remote branch",
			make: func(t *testing.T, path string) { commit(t, path, "unpushed") },
			want: "has commits that are on no other branch",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := cloneWithUnpushedCommit(t)
			wt, err := worktree.Create(t.Context(), git, repo, "fix-login", defaults())
			require.NoError(t, err)
			c.make(t, wt.Path)

			err = worktree.Remove(t.Context(), git, wt.Path, wt.Branch)

			require.ErrorContains(t, err, "worktree "+wt.Path+" "+c.want)
			require.DirExists(t, wt.Path)
			require.Equal(t, "fix-login", run(t, wt.Path, "branch", "--show-current"))
		})
	}
}

func TestBranches(t *testing.T) {
	repo := newRepo(t)
	commit(t, repo, "first")
	wt, err := worktree.Create(t.Context(), git, repo, "stack", defaults())
	require.NoError(t, err)

	require.Equal(t, []string{"stack"}, branches(t, wt.Path))

	run(t, wt.Path, "switch", "--quiet", "-c", "stack-2")
	run(t, wt.Path, "switch", "--quiet", "stack")
	run(t, wt.Path, "switch", "--quiet", "--detach")
	run(t, wt.Path, "switch", "--quiet", "-c", "stack-3")
	run(t, repo, "switch", "--quiet", "-c", "elsewhere")

	require.Equal(t, []string{"stack-3", "stack", "stack-2"}, branches(t, wt.Path))
}

func TestInRepo(t *testing.T) {
	repo := newRepo(t)
	sub := filepath.Join(repo, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))

	require.True(t, worktree.InRepo(t.Context(), git, repo))
	require.True(t, worktree.InRepo(t.Context(), git, sub))
	require.False(t, worktree.InRepo(t.Context(), git, tempDir(t)))
}

func defaults() config.Worktree {
	return config.Default().Worktree
}

func branches(t *testing.T, path string) []string {
	t.Helper()
	got, err := worktree.Branches(t.Context(), git, path)
	require.NoError(t, err)
	return got
}

// tempDir is t.TempDir with symlinks resolved, as git reports paths.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
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

func newRepo(t *testing.T) string {
	t.Helper()
	isolateGit(t)
	repo := tempDir(t)
	run(t, repo, "init", "--quiet", "--initial-branch=main")
	return repo
}

func commit(t *testing.T, dir, message string) {
	t.Helper()
	run(t, dir, "commit", "--quiet", "--allow-empty", "-m", message)
}

func newOrigin(t *testing.T) string {
	t.Helper()
	isolateGit(t)
	origin := tempDir(t)
	run(t, origin, "init", "--quiet", "--bare", "--initial-branch=main")
	seed := newRepo(t)
	commit(t, seed, "on origin")
	run(t, seed, "push", "--quiet", origin, "main")
	return origin
}

func cloneWithUnpushedCommit(t *testing.T) string {
	t.Helper()
	origin := newOrigin(t)
	repo := tempDir(t)
	run(t, repo, "clone", "--quiet", origin, ".")
	commit(t, repo, "not pushed")
	return repo
}

func remoteAddedWithUnpushedCommit(t *testing.T) string {
	t.Helper()
	origin := newOrigin(t)
	repo := newRepo(t)
	run(t, repo, "remote", "add", "origin", origin)
	run(t, repo, "fetch", "--quiet", "origin")
	run(t, repo, "reset", "--quiet", "--hard", "origin/main")
	commit(t, repo, "not pushed")
	return repo
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git.Run(t.Context(), dir, args...)
	require.NoError(t, err)
	return out
}
