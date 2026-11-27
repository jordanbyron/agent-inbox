package release_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/release"
)

func TestParsePullRequest(t *testing.T) {
	pr, err := release.ParsePullRequest([]byte(`{
		"number": 29, "title": "Add a Start button",
		"labels": [{"name": "ai-generated"}],
		"commits": [{"messageHeadline": "feat: add a Start button"}]
	}`))
	require.NoError(t, err)
	require.Equal(t, release.PullRequest{
		Number: 29, Title: "Add a Start button",
		LabelNames: []string{"ai-generated"}, CommitSubjects: []string{"feat: add a Start button"},
	}, pr)
}

func TestMissingLabel(t *testing.T) {
	cases := []struct {
		name string
		pr   release.PullRequest
		want string
	}{
		{name: "feat title", pr: release.PullRequest{Title: "feat: a"}, want: "enhancement"},
		{name: "fix title", pr: release.PullRequest{Title: "fix: a"}, want: "bug"},
		{name: "refactor title", pr: release.PullRequest{Title: "refactor: a"}, want: "refactor"},
		{name: "docs title", pr: release.PullRequest{Title: "docs: a"}, want: "documentation"},
		{name: "already labeled", pr: release.PullRequest{Title: "feat: a", LabelNames: []string{"bug"}}},
		{
			name: "other labels only", want: "enhancement",
			pr: release.PullRequest{Title: "feat: a", LabelNames: []string{"ai-generated"}},
		},
		{
			name: "plain title takes the best commit", want: "enhancement",
			pr: release.PullRequest{Title: "Add a button", CommitSubjects: []string{"fix: a", "feat: b"}},
		},
		{name: "plain title and commits", pr: release.PullRequest{Title: "Add", CommitSubjects: []string{"wip"}}},
		{name: "dependabot", pr: release.PullRequest{Title: "build(deps): bump x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, c.pr.MissingLabel())
		})
	}
}

func TestPullRequestNumbers(t *testing.T) {
	require.Equal(t, []int{29, 12}, release.PullRequestNumbers([]string{
		"Merge pull request #29 from jordanbyron/add-start\n\nAdd a Start button",
		"\nfeat: add a Start button",
		"\nfix: squashed (#12)\n\nbody (#99)",
		"\nMerge pull request #29 from jordanbyron/add-start",
		"",
	}))
}
