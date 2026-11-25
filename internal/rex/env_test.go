package rex_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/rex"
	"github.com/jordanbyron/agent-inbox/internal/rex/rextest"
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
		{
			name:   "a block without a session",
			env:    map[string]string{"REX_BLOCK": "block:2"},
			want:   rex.Pane{BlockID: "block:2"},
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

func TestFindPane(t *testing.T) {
	fake := rextest.New(rex.Server{})
	sessionID := fake.AddSession("work")
	windowID, err := fake.AddWindow(sessionID, "tab")
	require.NoError(t, err)
	blockID, err := fake.AddBlock(sessionID, windowID, "")
	require.NoError(t, err)

	cases := []struct {
		name    string
		env     map[string]string
		want    rex.Pane
		inside  bool
		wantErr bool
	}{
		{
			name:   "the session is looked up from the block",
			env:    map[string]string{"REX_BLOCK": blockID},
			want:   rex.Pane{SessionID: sessionID, BlockID: blockID},
			inside: true,
		},
		{
			name:   "REX_SESSION is taken as given",
			env:    map[string]string{"REX_SESSION": "session:given", "REX_BLOCK": blockID},
			want:   rex.Pane{SessionID: "session:given", BlockID: blockID},
			inside: true,
		},
		{name: "outside Rex", env: map[string]string{}},
		{name: "a block Rex doesn't know", env: map[string]string{"REX_BLOCK": "block:gone"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pane, inside, err := rex.FindPane(context.Background(), fake, func(key string) string { return tc.env[key] })
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.inside, inside)
			require.Equal(t, tc.want, pane)
		})
	}
}
