package rex_test

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

func TestParseError(t *testing.T) {
	notFound, err := os.ReadFile("testdata/error_not_found.txt")
	require.NoError(t, err)

	cases := []struct {
		name          string
		stderr        string
		code          string
		message       string
		notFound      bool
		remoteControl bool
	}{
		{
			name:     "an API error keeps its message and code",
			stderr:   string(notFound),
			code:     "not_found",
			message:  "block block:01a20b3e-a554-780a-b83d-88124f83eaa5 not found in any window",
			notFound: true,
		},
		{
			name:     "the CLI's own lookup failure has no code but still reads as not found",
			stderr:   "Error: session target \"nosuchsession\" not found\n",
			message:  "session target \"nosuchsession\" not found",
			notFound: true,
		},
		{
			name:    "a bad request is neither",
			stderr:  "Error: stream response \"control-1\" failed: \"Studio\" does not offer \"no.such.action\" (status=400 code=not_supported)\n",
			code:    "not_supported",
			message: "\"Studio\" does not offer \"no.such.action\"",
		},
		{
			// The refusal text is from claude-inbox on Rex 0.1.0; this server had
			// Remote Control on, so its code and status are assumed.
			name:          "an app with Remote Control off",
			stderr:        "Error: stream response \"control-1\" failed: Rex.app is not accepting actions. Turn on Remote Control in its Settings under Rex Server. (status=403 code=refused)\n",
			code:          "refused",
			message:       "Rex.app is not accepting actions. Turn on Remote Control in its Settings under Rex Server.",
			remoteControl: true,
		},
		{
			name:    "text that isn't in the CLI's form is kept whole",
			stderr:  "panic: something\n",
			message: "panic: something",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := rex.ParseError("client.perform", tc.stderr)
			require.Equal(t, tc.code, err.Code)
			require.Equal(t, tc.message, err.Message)
			require.Equal(t, tc.notFound, errors.Is(err, rex.ErrNotFound))
			require.Equal(t, tc.remoteControl, errors.Is(err, rex.ErrRemoteControlOff))
		})
	}
}
