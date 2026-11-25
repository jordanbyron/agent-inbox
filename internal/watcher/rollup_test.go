package watcher_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/triage"
	"github.com/jordanbyron/agent-inbox/internal/watcher"
)

func TestRollUp(t *testing.T) {
	root := func(state string) rex.StatusRecord {
		return rex.StatusRecord{State: state, App: "claude-code", Kind: "", Msg: "root says", Title: "Fix login"}
	}
	child := func(id, state string) rex.StatusRecord {
		return rex.StatusRecord{ID: id, State: state, Title: "sub-agent " + id}
	}
	cases := []struct {
		name    string
		records []rex.StatusRecord
		want    watcher.Rolled
	}{
		{
			name: "no records yet is starting",
			want: watcher.Rolled{Status: triage.StatusStarting},
		},
		{
			name:    "the root alone gives the state, msg and title",
			records: []rex.StatusRecord{root("idle")},
			want:    watcher.Rolled{Status: triage.StatusIdle, Msg: "root says", Title: "Fix login"},
		},
		{
			name: "blocked on the root gives its kind",
			records: []rex.StatusRecord{
				{State: "blocked", Kind: "permission", App: "claude-code", Msg: "approve Bash: ls"},
			},
			want: watcher.Rolled{Status: triage.StatusBlocked, Kind: "permission", Msg: "approve Bash: ls"},
		},
		{
			name:    "a working child keeps a quiet root at work, waiting on it",
			records: []rex.StatusRecord{root("idle"), child("a", "working")},
			want:    watcher.Rolled{Status: triage.StatusWorking, WaitingOnWork: true, Msg: "root says", Title: "Fix login"},
		},
		{
			name:    "a working root with working children is just working",
			records: []rex.StatusRecord{root("working"), child("a", "working")},
			want:    watcher.Rolled{Status: triage.StatusWorking, Msg: "root says", Title: "Fix login"},
		},
		{
			name: "a blocked child needs you even while the root works, and says why",
			records: []rex.StatusRecord{
				root("working"),
				{ID: "a", State: "blocked", Kind: "question", Msg: "which branch?"},
			},
			want: watcher.Rolled{Status: triage.StatusBlocked, Kind: "question", Msg: "which branch?", Title: "Fix login"},
		},
		{
			name:    "a child in error needs you",
			records: []rex.StatusRecord{root("idle"), child("a", "done"), child("b", "error")},
			want:    watcher.Rolled{Status: triage.StatusError, Title: "Fix login"},
		},
		{
			name: "the root needing you speaks before a child that does",
			records: []rex.StatusRecord{
				{ID: "a", State: "blocked", Kind: "question", Msg: "which branch?"},
				{State: "blocked", Kind: "auth", Msg: "log in"},
			},
			want: watcher.Rolled{Status: triage.StatusBlocked, Kind: "auth", Msg: "log in"},
		},
		{
			name: "an error outranks a blocked root, since error is never auto-settled",
			records: []rex.StatusRecord{
				{ID: "a", State: "error", Msg: "child failed"},
				{State: "blocked", Kind: "auth", Msg: "log in"},
			},
			want: watcher.Rolled{Status: triage.StatusError, Msg: "child failed"},
		},
		{
			name:    "children only, all done, is still starting",
			records: []rex.StatusRecord{child("a", "done")},
			want:    watcher.Rolled{Status: triage.StatusStarting},
		},
		{
			name:    "a state this build doesn't know reads as idle",
			records: []rex.StatusRecord{root("thinking")},
			want:    watcher.Rolled{Status: triage.StatusIdle, Msg: "root says", Title: "Fix login"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, watcher.RollUp(c.records))
		})
	}
}
