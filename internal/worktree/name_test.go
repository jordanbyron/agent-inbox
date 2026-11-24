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
		{prompt: "Fix the login bug on Safari please", want: "fix-the-login-bug"},
		{prompt: "  fix   flaky_spec.rb!  ", want: "fix-flaky-spec-rb"},
		{prompt: "Bump v2.3", want: "bump-v2-3"},
		{prompt: "@src/app.go: why?", want: "src-app-go-why"},
		{prompt: "refactorthewholeauthenticationmodule into smaller pieces", want: "refactorthewholeauthenticationmodule"},
		{prompt: "internationalization localization", want: "internationalization-localization"},
		{prompt: "internationalization localization globalization", want: "internationalization-localization"},
		{prompt: "", want: "agent"},
		{prompt: "¿qué? 🚀", want: "qu"},
		{prompt: "🚀", want: "agent"},
	}
	for _, c := range cases {
		t.Run(c.prompt, func(t *testing.T) {
			require.Equal(t, c.want, worktree.NameFromPrompt(c.prompt))
		})
	}
}
