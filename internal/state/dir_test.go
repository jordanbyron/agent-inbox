package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/state"
)

func TestDir(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("HOME", home)
	cases := []struct {
		name string
		xdg  string
		want string
	}{
		{name: "under XDG_STATE_HOME", xdg: xdg, want: filepath.Join(xdg, "agent-inbox", "srv")},
		{name: "under the home directory without it", xdg: "", want: filepath.Join(home, ".local", "state", "agent-inbox", "srv")},
		{name: "under the home directory when it is relative", xdg: "state", want: filepath.Join(home, ".local", "state", "agent-inbox", "srv")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", c.xdg)

			dir, err := state.Dir("srv")

			require.NoError(t, err)
			require.Equal(t, c.want, dir)
			info, err := os.Stat(dir)
			require.NoError(t, err)
			require.Equal(t, os.ModeDir|0o700, info.Mode())
		})
	}
}

func TestDirRefusesBadServerID(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, server := range []string{"", ".", "..", "a/b", "../escape"} {
		t.Run(server, func(t *testing.T) {
			_, err := state.Dir(server)
			require.Error(t, err)
		})
	}
}
