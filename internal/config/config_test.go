package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/recipe"
)

func TestPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cases := []struct {
		name string
		xdg  string
		want string
	}{
		{name: "under XDG_CONFIG_HOME", xdg: "/xdg", want: "/xdg/agent-inbox/config.toml"},
		{name: "under ~/.config without it", xdg: "", want: filepath.Join(home, ".config", "agent-inbox", "config.toml")},
		{name: "under ~/.config when it is relative", xdg: "conf", want: filepath.Join(home, ".config", "agent-inbox", "config.toml")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", c.xdg)

			path, err := config.Path()

			require.NoError(t, err)
			require.Equal(t, c.want, path)
		})
	}
}

func TestLoadMissingFileIsTheDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.toml"))

	require.NoError(t, err)
	require.Equal(t, "default", cfg.Sort)
	require.False(t, cfg.FoldWorking)
	require.False(t, cfg.Reap)
	require.Equal(t, config.Worktree{
		Default: true,
		Path:    "{repo}/.worktrees/{name}",
		Branch:  "{name}",
		Base:    "fresh",
	}, cfg.Worktree)
	require.Equal(t, []string{"claude", "codex", "agy"}, names(cfg.Recipes))
}

func TestLoadOverrides(t *testing.T) {
	path := write(t, `
sort = "activity"
fold_working = true
reap = true

[worktree]
default = false
path = "~/worktrees/{name}"
base = "head"
`)

	cfg, err := config.Load(path)

	require.NoError(t, err)
	require.Equal(t, "activity", cfg.Sort)
	require.True(t, cfg.FoldWorking)
	require.True(t, cfg.Reap)
	require.Equal(t, config.Worktree{
		Default: false,
		Path:    "~/worktrees/{name}",
		Branch:  "{name}",
		Base:    "head",
	}, cfg.Worktree)
}

func TestLoadRecipes(t *testing.T) {
	path := write(t, `
[recipes.codex]
app = "codex"
start = ["codex", "{prompt}"]
resume = ["codex", "resume", "{session_id}"]

[recipes.aider]
app = "aider"
start = ["aider", "--model", "{model}"]
resume = ["aider", "--restore-chat-history"]
mention = "/add {path}"
commands = ["~/.aider/commands"]

[recipes.aider.choices]
model = ["default", "sonnet"]
permissions = ["default", "yolo"]

[recipes.aider.permission_args]
yolo = ["--yes-always", "--no-git"]
`)

	cfg, err := config.Load(path)

	require.NoError(t, err)
	require.Equal(t, []string{"claude", "codex", "agy", "aider"}, names(cfg.Recipes))
	codex, ok := cfg.Recipes.Named("codex")
	require.True(t, ok)
	require.Equal(t, []string{"codex", "{prompt}"}, codex.Start)
	require.Nil(t, codex.Sessions)
	require.Nil(t, codex.ResumeLatest)
	require.Nil(t, codex.PermissionArgs, "a replaced built-in keeps none of its own")
	aider, ok := cfg.Recipes.Named("aider")
	require.True(t, ok)
	require.Equal(t, recipe.Recipe{
		Name:           "aider",
		App:            "aider",
		Start:          []string{"aider", "--model", "{model}"},
		Resume:         []string{"aider", "--restore-chat-history"},
		Mention:        "/add {path}",
		Commands:       []string{"~/.aider/commands"},
		Choices:        recipe.Choices{Model: []string{"default", "sonnet"}, Permissions: []string{"default", "yolo"}},
		PermissionArgs: map[string][]string{"yolo": {"--yes-always", "--no-git"}},
	}, aider)
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name string
		toml string
		want string
	}{
		{name: "unknown key", toml: `color = true`, want: "unknown key color"},
		{name: "unknown worktree key", toml: "[worktree]\nroot = \"x\"", want: "unknown key worktree.root"},
		{name: "unknown recipe key", toml: "[recipes.x]\napp = \"x\"\nstart = [\"x\"]\nresume = [\"x\"]\nbegin = []", want: "unknown key recipes.x.begin"},
		{name: "bad sort", toml: `sort = "newest"`, want: `sort: "newest" is not one of`},
		{name: "bad base", toml: "[worktree]\nbase = \"main\"", want: `worktree.base: "main" is not one of`},
		{name: "wrong type", toml: `reap = "yes"`, want: "reap"},
		{name: "bad path placeholder", toml: "[worktree]\npath = \"{root}/{name}\"", want: "worktree.path: unknown placeholder {root}"},
		{name: "empty branch", toml: "[worktree]\nbranch = \"\"", want: "worktree.branch: is empty"},
		{name: "bad recipe placeholder", toml: "[recipes.x]\napp = \"x\"\nstart = [\"x\", \"{sesion_id}\"]\nresume = [\"x\"]", want: "recipes.x.start: unknown placeholder {sesion_id}"},
		{name: "recipe without app", toml: "[recipes.x]\nstart = [\"x\"]\nresume = [\"x\"]", want: "recipes.x.app: is empty"},
		{name: "not TOML", toml: `sort = `, want: "toml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := write(t, c.toml)

			_, err := config.Load(path)

			require.ErrorContains(t, err, path)
			require.ErrorContains(t, err, c.want)
		})
	}
}

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func names(set recipe.Set) []string {
	var out []string
	for _, r := range set {
		out = append(out, r.Name)
	}
	return out
}
