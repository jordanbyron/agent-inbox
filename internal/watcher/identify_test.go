package watcher_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/recipe"
	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/state"
	"github.com/jordanbyron/agent-inbox/internal/watcher"
)

func TestIdentify(t *testing.T) {
	held := map[string]state.Record{"a1": {Harness: "codex"}}
	claude := []rex.StatusRecord{{State: "working", App: "claude-code"}}
	cases := []struct {
		name    string
		label   string
		records []rex.StatusRecord
		want    watcher.Identity
		ok      bool
	}{
		{
			name:  "a block labeled for a record we hold is that launched agent",
			label: "agent-inbox:a1",
			want:  watcher.Identity{Key: "a1", Launched: true, Harness: "codex"},
			ok:    true,
		},
		{
			name:    "its label wins over the app it reports",
			label:   "agent-inbox:a1",
			records: claude,
			want:    watcher.Identity{Key: "a1", Launched: true, Harness: "codex"},
			ok:      true,
		},
		{
			name:    "a label for a record we don't hold falls back to the app",
			label:   "agent-inbox:gone",
			records: claude,
			want:    watcher.Identity{Key: "block:1", Harness: "claude"},
			ok:      true,
		},
		{
			name:    "a block whose root names a recipe's app is a found agent",
			records: claude,
			want:    watcher.Identity{Key: "block:1", Harness: "claude"},
			ok:      true,
		},
		{
			name:    "the effective app counts when the root names none",
			records: []rex.StatusRecord{{State: "idle", EffectiveApp: "antigravity"}},
			want:    watcher.Identity{Key: "block:1", Harness: "agy"},
			ok:      true,
		},
		{
			name:    "a build tool is not an agent",
			records: []rex.StatusRecord{{State: "working", App: "cargo"}},
		},
		{
			name:    "only the root record decides",
			records: []rex.StatusRecord{{ID: "task", State: "working", App: "claude-code"}},
		},
		{
			name: "a plain shell is not an agent",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := watcher.Identify("block:1", c.label, c.records, held, recipe.Builtins())
			require.Equal(t, c.ok, ok)
			require.Equal(t, c.want, got)
		})
	}
}
