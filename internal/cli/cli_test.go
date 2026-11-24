package cli_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/cli"
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
