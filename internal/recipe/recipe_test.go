package recipe_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/recipe"
)

func TestStartArgs(t *testing.T) {
	cases := []struct {
		name     string
		template []string
		values   recipe.Values
		want     []string
	}{
		{
			name:     "fills every placeholder",
			template: []string{"x", "--id", "{session_id}", "--name", "{name}", "--cd", "{dir}", "--", "{prompt}"},
			values:   recipe.Values{SessionID: "s1", Name: "fix-login", Dir: "/repo", Prompt: "fix it"},
			want:     []string{"x", "--id", "s1", "--name", "fix-login", "--cd", "/repo", "--", "fix it"},
		},
		{
			name:     "leaves out a choice set to default with its flag",
			template: []string{"x", "--model", "{model}", "--effort", "{effort}", "--mode", "{permissions}"},
			values:   recipe.Values{Model: "default", Effort: "high", Permissions: "default"},
			want:     []string{"x", "--effort", "high"},
		},
		{
			name:     "leaves out an empty value with its flag",
			template: []string{"x", "--name", "{name}", "--", "{prompt}"},
			values:   recipe.Values{},
			want:     []string{"x"},
		},
		{
			name:     "keeps a prompt that says default",
			template: []string{"x", "--", "{prompt}"},
			values:   recipe.Values{Prompt: "default"},
			want:     []string{"x", "--", "default"},
		},
		{
			name:     "leaves out a value inside a longer argument with its flag",
			template: []string{"x", "-c", "effort={effort}", "-c", "model={model}"},
			values:   recipe.Values{Effort: "default", Model: "o9"},
			want:     []string{"x", "-c", "model=o9"},
		},
		{
			name:     "leaves out a flag that carries its own value alone",
			template: []string{"x", "--search", "--model={model}"},
			values:   recipe.Values{},
			want:     []string{"x", "--search"},
		},
		{
			name:     "leaves out a bare argument without touching the one before",
			template: []string{"x", "resume", "{session_id}"},
			values:   recipe.Values{},
			want:     []string{"x", "resume"},
		},
		{
			name:     "does not expand a placeholder inside a value",
			template: []string{"x", "{prompt}"},
			values:   recipe.Values{Prompt: "say {name}", Name: "n"},
			want:     []string{"x", "say {name}"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := recipe.Recipe{Start: c.template}

			require.Equal(t, c.want, r.StartArgs(c.values))
		})
	}
}

func TestBuiltinStartArgs(t *testing.T) {
	cases := []struct {
		recipe string
		values recipe.Values
		want   []string
	}{
		{
			recipe: "claude",
			values: recipe.Values{SessionID: "u1", Name: "fix-login", Model: "opus", Effort: "high", Permissions: "plan", Prompt: "-fix it"},
			want: []string{
				"claude", "--session-id", "u1", "--name", "fix-login", "--model", "opus",
				"--effort", "high", "--permission-mode", "plan", "--", "-fix it",
			},
		},
		{
			recipe: "claude",
			values: recipe.Values{SessionID: "u1", Name: "n", Model: "default", Effort: "default", Permissions: "default"},
			want:   []string{"claude", "--session-id", "u1", "--name", "n"},
		},
		{
			recipe: "codex",
			values: recipe.Values{Name: "n", Model: "gpt-6-luna", Effort: "xhigh", Permissions: "workspace-write", Prompt: "fix it"},
			want: []string{
				"codex", "--model", "gpt-6-luna", "-c", "model_reasoning_effort=xhigh",
				"--sandbox", "workspace-write", "--", "fix it",
			},
		},
		{
			recipe: "codex",
			values: recipe.Values{Model: "default", Effort: "default", Permissions: "default", Prompt: "fix it"},
			want:   []string{"codex", "--", "fix it"},
		},
		{
			recipe: "agy",
			values: recipe.Values{Effort: "low", Permissions: "accept-edits", Prompt: "fix it"},
			want:   []string{"agy", "--effort", "low", "--mode", "accept-edits", "--prompt-interactive", "fix it"},
		},
	}
	for _, c := range cases {
		t.Run(c.recipe, func(t *testing.T) {
			r, ok := recipe.Builtins().Named(c.recipe)
			require.True(t, ok)

			require.Equal(t, c.want, r.StartArgs(c.values))
		})
	}
}

func TestBuiltinResumeArgs(t *testing.T) {
	cases := []struct {
		recipe    string
		sessionID string
		want      []string
	}{
		{recipe: "claude", sessionID: "u1", want: []string{"claude", "--resume", "u1"}},
		{recipe: "codex", sessionID: "c1", want: []string{"codex", "resume", "c1"}},
		{recipe: "codex", sessionID: "", want: []string{"codex", "resume", "--last"}},
		{recipe: "agy", sessionID: "a1", want: []string{"agy", "--conversation", "a1"}},
		{recipe: "agy", sessionID: "", want: []string{"agy", "--continue"}},
	}
	for _, c := range cases {
		t.Run(c.recipe+" "+c.sessionID, func(t *testing.T) {
			r, ok := recipe.Builtins().Named(c.recipe)
			require.True(t, ok)

			require.Equal(t, c.want, r.ResumeArgs(recipe.Values{SessionID: c.sessionID, Name: "n"}))
		})
	}
}

func TestTakesSessionID(t *testing.T) {
	set := recipe.Builtins()
	for name, want := range map[string]bool{"claude": true, "codex": false, "agy": false} {
		r, ok := set.Named(name)
		require.True(t, ok)
		require.Equal(t, want, r.TakesSessionID(), name)
	}
}

func TestBuiltinsSessionFinders(t *testing.T) {
	set := recipe.Builtins()
	claude, _ := set.Named("claude")
	codex, _ := set.Named("codex")
	agy, _ := set.Named("agy")

	require.Nil(t, claude.Sessions)
	require.Equal(t, recipe.CodexSessions{}, codex.Sessions)
	require.Equal(t, recipe.AgySessions{}, agy.Sessions)
}

func TestForApp(t *testing.T) {
	set, err := recipe.WithBuiltins(map[string]recipe.Recipe{
		"aider":  {App: "aider", Start: []string{"aider"}, Resume: []string{"aider"}},
		"claude": {App: "claude-code", Start: []string{"my-claude"}, Resume: []string{"my-claude"}},
	})
	require.NoError(t, err)
	cases := []struct {
		app  string
		want string
	}{
		{app: "claude-code", want: "claude"},
		{app: "codex", want: "codex"},
		{app: "antigravity", want: "agy"},
		{app: "aider", want: "aider"},
		{app: "cargo", want: ""},
		{app: "", want: ""},
	}
	for _, c := range cases {
		t.Run(c.app, func(t *testing.T) {
			r, ok := set.ForApp(c.app)

			require.Equal(t, c.want != "", ok)
			require.Equal(t, c.want, r.Name)
		})
	}
	claude, _ := set.ForApp("claude-code")
	require.Equal(t, []string{"my-claude"}, claude.Start)
}

func TestWithBuiltinsChecksPlaceholders(t *testing.T) {
	cases := []struct {
		name   string
		recipe recipe.Recipe
		want   string
	}{
		{
			name:   "in start",
			recipe: recipe.Recipe{App: "x", Start: []string{"x", "--m={size}"}, Resume: []string{"x"}},
			want:   "recipes.x.start: unknown placeholder {size}",
		},
		{
			name:   "in resume",
			recipe: recipe.Recipe{App: "x", Start: []string{"x"}, Resume: []string{"x", "{path}"}},
			want:   "recipes.x.resume: unknown placeholder {path}",
		},
		{
			name:   "in mention",
			recipe: recipe.Recipe{App: "x", Start: []string{"x"}, Resume: []string{"x"}, Mention: "@{name}"},
			want:   "recipes.x.mention: unknown placeholder {name}",
		},
		{
			name:   "no start",
			recipe: recipe.Recipe{App: "x", Resume: []string{"x"}},
			want:   "recipes.x.start: is empty",
		},
		{
			name:   "no resume",
			recipe: recipe.Recipe{App: "x", Start: []string{"x"}},
			want:   "recipes.x.resume: is empty",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := recipe.WithBuiltins(map[string]recipe.Recipe{"x": c.recipe})

			require.EqualError(t, err, c.want)
		})
	}
}

func TestNewSessionID(t *testing.T) {
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	a, b := recipe.NewSessionID(), recipe.NewSessionID()

	require.Regexp(t, uuid, a)
	require.NotEqual(t, a, b)
}
