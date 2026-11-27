package pulls_test

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/pulls"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/triage"
)

func TestFindByBranch(t *testing.T) {
	branches := []string{
		"bagtoad/oauth-logout-revocation",
		"fix-percent-encoded-url",
		"bagtoad/expiring-login-tokens",
		"dependabot/go_modules/github.com/charmbracelet/x/ansi-0.11.10",
		"tidy-dev-cli-assessor-prerequisites",
		"no-such-branch-for-agent-inbox",
	}
	var calls [][]string
	var dirs []string
	gh := ghFunc(func(dir string, args []string) ([]byte, error) {
		calls = append(calls, args)
		dirs = append(dirs, dir)
		return []byte(testdata(t, "branches.json")), nil
	})

	found, err := pulls.FindByBranch(t.Context(), gh, "/src/cli", branches)

	require.NoError(t, err)
	pr := func(n int, title string, st state.PRState, h triage.Health) state.PullRequest {
		return state.PullRequest{
			URL: "https://github.com/cli/cli/pull/" + strconv.Itoa(n), Number: n, Title: title,
			State: st, Health: h, Source: state.SourceBranch,
		}
	}
	merged := pr(15145, "chore(deps): Bump github.com/charmbracelet/x/ansi from 0.11.9 to 0.11.10", state.PRStateMerged, triage.HealthUnknown)
	merged.ResolvedAt = time.Date(2026, 11, 23, 18, 24, 26, 0, time.UTC)
	require.Equal(t, []state.PullRequest{
		pr(15100, "Revoke CLI OAuth tokens on logout", state.PRStateOpen, triage.HealthChangesRequested),
		pr(13788, "fix: preserve percent-encoded path in DisplayURL", state.PRStateOpen, triage.HealthFailing),
		pr(15108, "Default to expiring login tokens", state.PRStateOpen, triage.HealthConflicting),
		merged,
		pr(15149, "Add bug readiness assessment skill", state.PRStateDraft, triage.HealthClean),
	}, found)

	require.Len(t, calls, 1, "every branch goes in one gh call")
	require.Equal(t, []string{"api", "graphql"}, calls[0][:2])
	require.Equal(t, []string{"/src/cli"}, dirs)
	require.Equal(t, "{owner}", fieldValue(calls[0], "owner"))
	require.Equal(t, "{repo}", fieldValue(calls[0], "name"))
	for i, b := range branches {
		require.Equal(t, b, fieldValue(calls[0], "b"+strconv.Itoa(i)))
	}
	query := fieldValue(calls[0], "query")
	for _, field := range []string{"state", "isDraft", "closedAt", "statusCheckRollup", "reviewDecision", "mergeable", "title"} {
		require.Contains(t, query, field, "state and health come in the one call")
	}
}

func TestFindByBranchWithNoBranchesAsksNothing(t *testing.T) {
	gh := ghFunc(func(string, []string) ([]byte, error) {
		t.Fatal("asked gh about no branches")
		return nil, nil
	})

	found, err := pulls.FindByBranch(t.Context(), gh, "/src", nil)

	require.NoError(t, err)
	require.Empty(t, found)
}

func TestFindByBranchWhenGHFails(t *testing.T) {
	offline := errors.New("offline")

	_, err := pulls.FindByBranch(t.Context(), replying("", offline), "/src", []string{"main"})

	require.ErrorIs(t, err, offline)
}

func TestFindInText(t *testing.T) {
	link := func(repo string, n int) state.PullRequest {
		return state.PullRequest{URL: "https://github.com/" + repo + "/pull/" + strconv.Itoa(n), Number: n, Source: state.SourceCapture}
	}
	tests := []struct {
		name string
		text string
		want []state.PullRequest
	}{
		{name: "nothing", text: "all done, no links here", want: nil},
		{name: "a bare link", text: "https://github.com/o/r/pull/12", want: []state.PullRequest{link("o/r", 12)}},
		{
			name: "a trailing path",
			text: "see https://github.com/o/r/pull/12/files#diff-1 for the diff",
			want: []state.PullRequest{link("o/r", 12)},
		},
		{
			name: "punctuation after it",
			text: "Opened (https://github.com/jordanbyron/agent-inbox/pull/4). Done.",
			want: []state.PullRequest{link("jordanbyron/agent-inbox", 4)},
		},
		{
			name: "several, de-duplicated in order",
			text: "https://github.com/o/r/pull/2\n✓ Created PR #1 (https://github.com/o/r/pull/1)\nhttps://github.com/o/r/pull/2/checks",
			want: []state.PullRequest{link("o/r", 2), link("o/r", 1)},
		},
		{name: "a dotted repository", text: "https://github.com/o/my.repo_x/pull/7", want: []state.PullRequest{link("o/my.repo_x", 7)}},
		{name: "an issue", text: "https://github.com/o/r/issues/12", want: nil},
		{name: "another host", text: "https://gitlab.com/o/r/-/merge_requests/3413", want: nil},
		{name: "a number running into a word", text: "https://github.com/o/r/pull/12abc", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, pulls.FindInText(tt.text))
		})
	}
}

func TestLink(t *testing.T) {
	linked := func(repo string, n int) state.PullRequest {
		return state.PullRequest{URL: "https://github.com/" + repo + "/pull/" + strconv.Itoa(n), Number: n, Source: state.SourceLinked}
	}
	tests := []struct {
		name    string
		input   string
		want    state.PullRequest
		wantErr error
	}{
		{name: "a link", input: "https://github.com/o/r/pull/12", want: linked("o/r", 12)},
		{name: "a link with a trailing path", input: " https://github.com/o/r/pull/12/files\n", want: linked("o/r", 12)},
		{name: "owner/repo#n", input: "cli/cli#15100", want: linked("cli/cli", 15100)},
		{name: "#n in the directory's repository", input: "#4", want: linked("jordanbyron/agent-inbox", 4)},
		{name: "n in the directory's repository", input: "885", want: linked("jordanbyron/agent-inbox", 885)},
		{name: "an issue link", input: "https://github.com/o/r/issues/12", wantErr: pulls.ErrNotALink},
		{name: "another host", input: "https://gitlab.com/o/r/-/merge_requests/4", wantErr: pulls.ErrNotALink},
		{name: "not a number", input: "#12a", wantErr: pulls.ErrNotALink},
		{name: "number zero", input: "#0", wantErr: pulls.ErrNotALink},
		{name: "nothing", input: "  ", wantErr: pulls.ErrNotALink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := ghFunc(func(dir string, args []string) ([]byte, error) {
				require.Equal(t, "/src/agent-inbox", dir)
				require.Equal(t, []string{"repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner"}, args)
				return []byte("jordanbyron/agent-inbox\n"), nil
			})

			got, err := pulls.Link(t.Context(), gh, "/src/agent-inbox", tt.input)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLinkWhenGHCannotNameTheRepository(t *testing.T) {
	noRepo := errors.New("none of the git remotes point to a known GitHub host")

	_, err := pulls.Link(t.Context(), replying("", noRepo), "/tmp", "#4")

	require.ErrorIs(t, err, noRepo)
}
