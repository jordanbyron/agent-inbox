package rex_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

func TestPaneFromEnv(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		want   rex.Pane
		inside bool
	}{
		{
			name:   "inside a pane",
			env:    map[string]string{"REX_SESSION": "session:1", "REX_BLOCK": "block:2"},
			want:   rex.Pane{SessionID: "session:1", BlockID: "block:2"},
			inside: true,
		},
		{name: "outside Rex", env: map[string]string{}},
		{name: "a session without a block", env: map[string]string{"REX_SESSION": "session:1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pane, inside := rex.PaneFromEnv(func(key string) string { return tc.env[key] })
			require.Equal(t, tc.inside, inside)
			require.Equal(t, tc.want, pane)
		})
	}
}
