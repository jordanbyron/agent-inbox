package notify_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/notify"
)

func TestDesktopPicksWhatTheMachineHas(t *testing.T) {
	n := notify.Notification{Title: `fix "login"`, Body: `approve Bash: rm -rf 'tmp'`, Group: "a1"}
	cases := []struct {
		name      string
		goos      string
		installed []string
		want      []string
	}{
		{
			name:      "terminal-notifier on macOS",
			goos:      "darwin",
			installed: []string{"terminal-notifier", "osascript"},
			want:      []string{"terminal-notifier", "-title", `fix "login"`, "-message", `approve Bash: rm -rf 'tmp'`, "-group", "a1"},
		},
		{
			name:      "osascript on macOS without terminal-notifier",
			goos:      "darwin",
			installed: []string{"osascript"},
			want: []string{
				"osascript",
				"-e", "on run argv",
				"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
				"-e", "end run",
				`fix "login"`, `approve Bash: rm -rf 'tmp'`,
			},
		},
		{
			name:      "notify-send on Linux",
			goos:      "linux",
			installed: []string{"notify-send", "osascript"},
			want:      []string{"notify-send", "--app-name=agent-inbox", "--", `fix "login"`, `approve Bash: rm -rf 'tmp'`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := notify.NewDesktop(c.goos, func(name string) bool { return slices.Contains(c.installed, name) })

			require.NoError(t, err)
			require.Equal(t, c.want, d.Command(n))
		})
	}
}

func TestDesktopWithNothingToNotifyWith(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			_, err := notify.NewDesktop(goos, func(string) bool { return false })

			require.ErrorIs(t, err, notify.ErrNoProgram)
		})
	}
}

func TestAZeroDesktopRefuses(t *testing.T) {
	require.ErrorIs(t, notify.Desktop{}.Notify(t.Context(), notify.Notification{}), notify.ErrNoProgram)
}
