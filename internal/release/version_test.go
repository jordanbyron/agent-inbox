package release_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/release"
)

func TestBumpFor(t *testing.T) {
	cases := []struct {
		name     string
		current  string
		messages []string
		want     release.Bump
	}{
		{name: "nothing", current: "v1.2.3", messages: []string{"docs: say how", "refactor: tidy"}},
		{name: "fix", current: "v1.2.3", messages: []string{"fix: a", "docs: b"}, want: release.Patch},
		{name: "feat beats fix", current: "v1.2.3", messages: []string{"fix: a", "feat: b"}, want: release.Minor},
		{name: "scoped feat", current: "v1.2.3", messages: []string{"feat(list): a"}, want: release.Minor},
		{name: "bang", current: "v1.2.3", messages: []string{"feat!: drop the old flag"}, want: release.Major},
		{
			name: "breaking footer", current: "v1.2.3",
			messages: []string{"refactor: rename\n\nBREAKING CHANGE: the flag is gone"},
			want:     release.Major,
		},
		{name: "breaking before 1.0", current: "v0.4.1", messages: []string{"fix!: a"}, want: release.Minor},
		{name: "after a NUL split", current: "v1.2.3", messages: []string{"\nfix: a\n"}, want: release.Patch},
		{name: "merge commit", current: "v1.2.3", messages: []string{"Merge pull request #3 from x/y\n\nfeat: a"}},
		{name: "dependabot", current: "v1.2.3", messages: []string{"build(deps): bump x"}},
		{
			name: "squash with a plain title", current: "v1.2.3",
			messages: []string{"Add a Start button (#29)\n\n* feat: add a Start button\n\n* fix: focus it"},
			want:     release.Minor,
		},
		{
			name: "squash title beats its bullets", current: "v1.2.3",
			messages: []string{"fix: focus the form (#30)\n\n* feat: add a flag"},
			want:     release.Patch,
		},
		{name: "squash of plain commits", current: "v1.2.3", messages: []string{"Tidy (#31)\n\n* wip\n\n* more"}},
		{
			name: "breaking footer on a docs commit", current: "v1.2.3",
			messages: []string{"docs: a\n\nBREAKING CHANGE: b"},
			want:     release.Major,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, release.BumpFor(c.current, c.messages))
		})
	}
}

func TestNext(t *testing.T) {
	cases := []struct {
		bump release.Bump
		want string
	}{
		{release.Patch, "v1.2.4"},
		{release.Minor, "v1.3.0"},
		{release.Major, "v2.0.0"},
		{release.None, "v1.2.3"},
	}
	for _, c := range cases {
		t.Run(c.bump.String(), func(t *testing.T) {
			got, err := release.Next("v1.2.3", c.bump)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestNextRejectsAnOddTag(t *testing.T) {
	for _, tag := range []string{"1.2.3", "v1.2.3-rc1", "v1.2"} {
		t.Run(tag, func(t *testing.T) {
			_, err := release.Next(tag, release.Patch)
			require.Error(t, err)
		})
	}
}
