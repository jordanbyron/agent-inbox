package worktree_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

func TestNameFromPrompt(t *testing.T) {
	cases := []struct {
		prompt string
		want   string
	}{
		{prompt: "Fix the login bug on Safari please", want: "Fix the login bug on Safari please"},
		{prompt: "fix the flaky spec.", want: "Fix the flaky spec"},
		{
			prompt: `The "name" of these sessions is terrible. Why do they have to be one word?`,
			want:   `The "name" of these sessions is terrible`,
		},
		{prompt: "Why is CI red?\nIt passed yesterday.", want: "Why is CI red?"},
		{prompt: "Bump v2.3 and tag it", want: "Bump v2.3 and tag it"},
		{prompt: "@src/app.go: why?", want: "@src/app.go: why?"},
		{
			prompt: "Refactor the whole authentication module into smaller, well-named pieces",
			want:   "Refactor the whole authentication module into…",
		},
		{prompt: "  lots   of\tspace  ", want: "Lots of space"},
		{prompt: "¿qué? 🚀", want: "¿qué?"},
		{prompt: "", want: "Agent"},
		{prompt: "🚀", want: "Agent"},
	}
	for _, c := range cases {
		t.Run(c.prompt, func(t *testing.T) {
			require.Equal(t, c.want, worktree.NameFromPrompt(c.prompt))
		})
	}
}

func TestSlug(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{name: "Fix the login bug on Safari please", want: "fix-login-bug-safari"},
		{name: `The "name" of these sessions is terrible.`, want: "name-sessions-terrible"},
		{name: "fix-login", want: "fix-login"},
		{name: "  fix   flaky_spec.rb!  ", want: "fix-flaky-spec-rb"},
		{name: "Bump v2.3", want: "bump-v2-3"},
		{name: "Why is it?", want: "why-is-it"},
		{name: "refactorthewholeauthenticationmodule into smaller pieces", want: "refactorthewholeauthenticationmodule"},
		{name: "internationalization localization globalization", want: "internationalization-localization"},
		{name: "", want: "agent"},
		{name: "¿qué? 🚀", want: "qu"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, worktree.Slug(c.name))
		})
	}
}
