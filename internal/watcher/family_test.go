package watcher_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/triage"
	"github.com/jordanbyron/agent-inbox/internal/watcher"
)

func TestFamilySection(t *testing.T) {
	const (
		snoozed  = triage.SectionSnoozed
		pinned   = triage.SectionPinned
		settled  = triage.SectionSettled
		needsYou = triage.SectionNeedsYou
		working  = triage.SectionWorking
		active   = triage.SectionActive
	)
	cases := []struct {
		name    string
		root    triage.Section
		members []triage.Section
		want    triage.Section
	}{
		{name: "a lone root keeps its section", root: working, want: working},
		{name: "a child needing you brings the family to Needs you", root: active, members: []triage.Section{working, needsYou}, want: needsYou},
		{name: "the root needing you does too", root: needsYou, members: []triage.Section{working}, want: needsYou},
		{name: "a finished child outranks a working root", root: working, members: []triage.Section{active}, want: active},
		{name: "all at work is Working", root: working, members: []triage.Section{working}, want: working},
		{name: "a settled child counts for nothing", root: working, members: []triage.Section{settled}, want: working},
		{name: "a snoozed root holds the family, blocked child and all", root: snoozed, members: []triage.Section{needsYou}, want: snoozed},
		{name: "a pinned root holds it too", root: pinned, members: []triage.Section{needsYou}, want: pinned},
		{name: "a settled root settles the family", root: settled, members: []triage.Section{needsYou}, want: settled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, watcher.FamilySection(c.root, c.members))
		})
	}
}

func TestFamilyAgent(t *testing.T) {
	began := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	pr := func(n string) triage.PullRequest {
		return triage.PullRequest{URL: "https://github.com/o/r/pull/" + n, Known: true}
	}
	root := triage.Agent{Status: triage.StatusIdle, HasBlock: true, Began: began, PullRequests: []triage.PullRequest{pr("1")}}
	cases := []struct {
		name     string
		children []triage.Agent
		status   triage.Status
		prs      []triage.PullRequest
	}{
		{name: "a lone root is itself", status: triage.StatusIdle, prs: []triage.PullRequest{pr("1")}},
		{
			name:     "a working child keeps the family at work",
			children: []triage.Agent{{Status: triage.StatusWorking, HasBlock: true}},
			status:   triage.StatusWorking,
			prs:      []triage.PullRequest{pr("1")},
		},
		{
			name:     "a child in error puts it in error",
			children: []triage.Agent{{Status: triage.StatusError, HasBlock: true}},
			status:   triage.StatusError,
			prs:      []triage.PullRequest{pr("1")},
		},
		{
			name:     "working outranks error",
			children: []triage.Agent{{Status: triage.StatusError, HasBlock: true}, {Status: triage.StatusWorking, HasBlock: true}},
			status:   triage.StatusWorking,
			prs:      []triage.PullRequest{pr("1")},
		},
		{
			name:     "a closed child's last status holds nothing, but its pull requests count",
			children: []triage.Agent{{Status: triage.StatusWorking, PullRequests: []triage.PullRequest{pr("2"), pr("1")}}},
			status:   triage.StatusIdle,
			prs:      []triage.PullRequest{pr("1"), pr("2")},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := watcher.FamilyAgent(root, c.children)
			require.Equal(t, c.status, got.Status)
			require.Equal(t, c.prs, got.PullRequests)
			require.Equal(t, began, got.Began)
			require.True(t, got.HasBlock)
		})
	}
}

func TestLabel(t *testing.T) {
	cases := []struct {
		name                             string
		alias, record, title, dir, harns string
		want                             string
	}{
		{name: "the alias first", alias: "login", record: "fix-login", title: "Fix", dir: "/r/app", harns: "claude", want: "login"},
		{name: "then the record's name", record: "fix-login", title: "Fix", dir: "/r/app", harns: "claude", want: "fix-login"},
		{name: "then the status title", title: "Fix the login form", dir: "/r/app", harns: "claude", want: "Fix the login form"},
		{name: "then the directory's base name", dir: "/r/app", harns: "claude", want: "app"},
		{name: "then the harness", harns: "codex", want: "codex"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, watcher.Label(c.alias, c.record, c.title, c.dir, c.harns))
		})
	}
}
