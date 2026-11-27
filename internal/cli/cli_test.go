package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/cli"
	"github.com/jordanbyron/agent-inbox/internal/config"
)

func TestVersion(t *testing.T) {
	var out bytes.Buffer
	cmd := cli.New()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"version"})

	require.NoError(t, cmd.Execute())
	require.NotEmpty(t, bytes.TrimSpace(out.Bytes()))
}

func TestUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	cmd := cli.New()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"nope"})

	require.ErrorContains(t, cmd.Execute(), `unknown command "nope"`)
}

func TestLsNeedsJSON(t *testing.T) {
	var out bytes.Buffer
	cmd := cli.New()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"ls"})

	require.ErrorContains(t, cmd.Execute(), "ls needs --json")
}

func TestListSaysWhichFixtureItCannotRead(t *testing.T) {
	var out bytes.Buffer
	cmd := cli.New()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"list", "--fixture", "testdata/missing.json"})

	require.ErrorContains(t, cmd.Execute(), "reading testdata/missing.json")
}

func TestModalIsAFlagOfTheListAndTheRoot(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"list", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			cmd := cli.New()
			cmd.SetOut(&out)
			cmd.SetArgs(args)

			require.NoError(t, cmd.Execute())
			require.Contains(t, out.String(), "--modal")
		})
	}
}

func TestHoldFocusIsHidden(t *testing.T) {
	var out bytes.Buffer
	cmd := cli.New()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})

	require.NoError(t, cmd.Execute())
	require.NotContains(t, out.String(), "hold-focus")
}

func TestReapDeleterDoesNotStartWatcher(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REX_BIN_DIR", "")
	t.Setenv("PATH", "")

	d, err := cli.ReapDeleter(t.TempDir(), config.Config{Reap: false})
	require.NoError(t, err)
	require.Nil(t, d)

	d, err = cli.ReapDeleter(t.TempDir(), config.Config{Reap: true})
	require.NoError(t, err)
	require.NotNil(t, d)
}
