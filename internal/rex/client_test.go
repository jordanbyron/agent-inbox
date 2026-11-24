package rex_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

type reply struct {
	match  string
	file   string
	stderr string
	exit   int
	exec   string
}

// fakeRex stands in for the rex binary so Client is tested without a server.
func fakeRex(t *testing.T, replies ...reply) (*rex.Client, func() []string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	testdata, err := filepath.Abs("testdata")
	require.NoError(t, err)

	var script strings.Builder
	fmt.Fprintf(&script, "#!/bin/sh\nprintf '%%s\\n' \"$*\" >> '%s'\ncase \"$*\" in\n", log)
	for _, r := range replies {
		fmt.Fprintf(&script, "  *'%s'*)\n", r.match)
		if r.file != "" {
			fmt.Fprintf(&script, "    cat '%s'\n", filepath.Join(testdata, r.file))
		}
		if r.stderr != "" {
			fmt.Fprintf(&script, "    printf '%%s\\n' '%s' >&2\n", r.stderr)
		}
		if r.exec != "" {
			fmt.Fprintf(&script, "    exec %s\n", r.exec)
		}
		fmt.Fprintf(&script, "    exit %d ;;\n", r.exit)
	}
	script.WriteString("esac\necho \"Error: no reply for $*\" >&2\nexit 1\n")

	bin := filepath.Join(dir, "rex")
	require.NoError(t, os.WriteFile(bin, []byte(script.String()), 0o755))
	calls := func() []string {
		data, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err)
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
	return rex.New(bin), calls
}

const (
	testSession = "session:01a20b37-aed8-73d8-b215-20b7f9590d3d"
	testBlock   = "block:01a20b3e-5241-75e7-b413-68522863a41c"
	testWindow  = "window:01a20b3e-5257-7975-ab27-6e9bf86256f9"
	testApp     = "client:01a1f6c3-434f-7e9a-a081-3478a39840ac"
)

func TestServer(t *testing.T) {
	client, calls := fakeRex(t, reply{match: "api call server.status", file: "server_status.json"})

	server, err := client.Server(t.Context())
	require.NoError(t, err)
	require.Equal(t, rex.Server{
		HostID:      "0e9d6c1f-4a3b-4a52-9c85-2f6d3f6f2f10",
		InstanceID:  "9f140995-6b70-44d6-9cfc-e963ce9f9c32",
		PID:         54626,
		StartedAt:   time.Date(2026, 11, 3, 2, 21, 58, 162867000, time.UTC),
		APIRevision: 10,
	}, server)
	require.Equal(t, server.HostID, server.StateID())
	require.Equal(t, []string{"api call server.status"}, calls())
}

func TestStateIDWithoutHostID(t *testing.T) {
	require.Equal(t, "instance", rex.Server{InstanceID: "instance"}.StateID())
}

func TestSessions(t *testing.T) {
	client, _ := fakeRex(t, reply{match: "api call session.list", file: "session_list.json"})

	sessions, err := client.Sessions(t.Context())
	require.NoError(t, err)
	require.Equal(t, []rex.Session{
		{ID: "session:01a1a144-2083-7042-8020-5c7531963abe", Label: "work"},
		{ID: testSession, Label: "agent-inbox-test-a"},
		{ID: "session:01a20b3a-205b-7280-a51a-aa0edbfa686f", Label: "agent-inbox-test-c"},
	}, sessions)
}

func TestView(t *testing.T) {
	client, calls := fakeRex(t, reply{match: "session.view", file: "session_view.json"})

	view, err := client.View(t.Context(), testSession)
	require.NoError(t, err)
	require.Equal(t, testSession, view.SessionID)
	require.Equal(t, "agent-inbox-test-a", view.Label)
	require.Equal(t, testWindow, view.ActiveWindowID)
	require.Equal(t, 28, view.Revision)

	labels := make([]string, 0, len(view.Windows))
	for _, w := range view.Windows {
		labels = append(labels, w.Label)
	}
	require.Equal(t, []string{"probe", "zsh", "agent"}, labels)

	agent := view.Windows[2]
	require.Equal(t, testWindow, agent.ID)
	require.Equal(t, testBlock, agent.FocusedBlockID)
	require.Equal(t, []rex.Block{{ID: testBlock, Label: "agent-inbox:a1"}}, agent.Blocks)
	require.Equal(t, "client:01a20b37-aed4-7a64-a47c-61f696de9aab", view.Windows[0].Blocks[0].Owner)
	require.Equal(t, []string{"api call -s " + testSession + " session.view"}, calls())
}

func TestStatus(t *testing.T) {
	client, calls := fakeRex(t, reply{match: "block.method", file: "program_status.json"})

	records, err := client.Status(t.Context(), testSession, testBlock)
	require.NoError(t, err)
	forty := 40
	require.Equal(t, []rex.StatusRecord{
		{
			State:        "blocked",
			Kind:         "permission",
			App:          "claude-code",
			EffectiveApp: "claude-code",
			Msg:          "approve Bash: ls",
			UpdatedAt:    parseTime(t, "2026-11-23T20:49:23.446887-05:00"),
		},
		{
			ID:           "task1",
			State:        "working",
			EffectiveApp: "claude-code",
			Title:        "Run tests",
			Progress:     &forty,
			UpdatedAt:    parseTime(t, "2026-11-23T20:49:23.4469-05:00"),
		},
	}, records)
	require.Equal(t, []string{
		"api call -s " + testSession + ` block.method {"block_id":"` + testBlock + `","method":"program_status"}`,
	}, calls())
}

func parseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	require.NoError(t, err)
	return parsed
}

func TestProcess(t *testing.T) {
	three := 3
	cases := []struct {
		name string
		file string
		want rex.Process
	}{
		{name: "a running program", file: "process_running.json", want: rex.Process{Alive: true, Dir: "/private/tmp"}},
		{name: "a program that exited in a kept-open block", file: "process_exited.json", want: rex.Process{ExitCode: &three}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := fakeRex(t, reply{match: "block.method", file: tc.file})
			process, err := client.Process(t.Context(), testSession, testBlock)
			require.NoError(t, err)
			require.Equal(t, tc.want, process)
		})
	}
}

func TestApps(t *testing.T) {
	client, calls := fakeRex(t, reply{match: "client.list", file: "client_list.json"})

	apps, err := client.Apps(t.Context())
	require.NoError(t, err)
	require.Equal(t, []rex.App{{
		ClientID: testApp,
		Label:    "Studio",
		SessionIDs: []string{
			"session:01a1a144-2083-7042-8020-5c7531963abe",
			testSession,
			"session:01a20b3a-205b-7280-a51a-aa0edbfa686f",
		},
	}}, apps)
	require.Equal(t, []string{`api call client.list {"kinds":["app"]}`}, calls())
}

func TestStart(t *testing.T) {
	opts := rex.StartOptions{
		Command:     []string{"claude", "--name", "fix login"},
		Dir:         "/tmp/repo",
		BlockLabel:  "agent-inbox:a1",
		WindowLabel: "fix login",
	}
	newWindowArgs := `{"focus":false,"layout":{"block":{"flavor":"com.superlogical.terminal.shell",` +
		`"label":"agent-inbox:a1","options":{"command":["claude","--name","fix login"],"cwd":"/tmp/repo"}}},` +
		`"window_label":"fix login"}`
	started := rex.Started{
		SessionID: testSession,
		WindowID:  "window:01a20b3e-5257-7975-ab27-6e9bf86256f9",
		BlockID:   "block:01a20b3e-5241-75e7-b413-68522863a41c",
	}

	t.Run("in an existing session", func(t *testing.T) {
		client, calls := fakeRex(t, reply{match: "session.new_window", file: "session_new_window.json"})
		existing := opts
		existing.SessionID = testSession

		got, err := client.Start(t.Context(), existing)
		require.NoError(t, err)
		require.Equal(t, started, got)
		require.Equal(t, []string{"api call -s " + testSession + " session.new_window " + newWindowArgs}, calls())
	})

	t.Run("in a new session, made empty so the window's block is let go", func(t *testing.T) {
		created := "session:01a20b3e-a2e5-7530-95f5-3a2db6a9092d"
		client, calls := fakeRex(t,
			reply{match: "session.create", file: "session_create.json"},
			reply{match: "session.new_window", file: "session_new_window.json"},
		)
		fresh := opts
		fresh.SessionLabel = "agents"

		got, err := client.Start(t.Context(), fresh)
		require.NoError(t, err)
		require.Equal(t, created, got.SessionID)
		require.Equal(t, []string{
			`api call session.create {"label":"agents"}`,
			"api call -s " + created + " session.new_window " + newWindowArgs,
		}, calls())
	})

	t.Run("a new session is destroyed again when its window fails", func(t *testing.T) {
		created := "session:01a20b3e-a2e5-7530-95f5-3a2db6a9092d"
		client, calls := fakeRex(t,
			reply{match: "session.create", file: "session_create.json"},
			reply{match: "session.new_window", stderr: `Error: stream response "control-1" failed: no creator provides it (status=404 code=not_found)`, exit: 1},
			reply{match: "session.destroy"},
		)

		_, err := client.Start(t.Context(), opts)
		require.ErrorIs(t, err, rex.ErrNotFound)
		require.Equal(t, "api call -s "+created+" session.destroy", calls()[2])
	})
}

func TestOpenLayer(t *testing.T) {
	client, calls := fakeRex(t, reply{match: "session.new_layer", file: "session_new_layer.json"})

	layer, err := client.OpenLayer(t.Context(), rex.LayerOptions{
		SessionID: testSession,
		WindowID:  testWindow,
		Command:   []string{"/usr/local/bin/agent-inbox", "new"},
		Dir:       "/tmp/repo",
		Bounds:    [4]float64{0.1, 0.1, 0.8, 0.8},
	})

	require.NoError(t, err)
	require.Equal(t, rex.Layer{
		ID:      "layer:01a20b81-ef81-75b1-a06a-83db70920d24",
		BlockID: "block:01a20b81-ef76-7c8e-b5a0-6af771912eb4",
	}, layer)
	require.Equal(t, []string{"api call -s " + testSession + ` session.new_layer {"bounds":[0.1,0.1,0.8,0.8],"focus":true,` +
		`"layout":{"block":{"flavor":"com.superlogical.terminal.shell","options":{"command":["/usr/local/bin/agent-inbox","new"],` +
		`"cwd":"/tmp/repo"}}},"window_id":"` + testWindow + `"}`}, calls())
}

func TestClose(t *testing.T) {
	client, calls := fakeRex(t, reply{match: "block.close"}, reply{match: "session.close_window"})

	require.NoError(t, client.CloseBlock(t.Context(), testSession, testBlock))
	require.NoError(t, client.CloseWindow(t.Context(), testSession, testWindow))
	require.Equal(t, []string{
		"api call -s " + testSession + ` block.close {"block_id":"` + testBlock + `"}`,
		"api call -s " + testSession + ` session.close_window {"window_id":"` + testWindow + `"}`,
	}, calls())
}

func TestShow(t *testing.T) {
	describe := reply{match: "session.describe_block", file: "describe_block.json"}
	focus := reply{match: "session.focus_block"}
	apps := reply{match: "client.list", file: "client_list.json"}

	t.Run("focuses the block and asks each app attached to the session to select its window", func(t *testing.T) {
		client, calls := fakeRex(t, describe, focus, apps, reply{match: "client.perform"})

		require.NoError(t, client.Show(t.Context(), testSession, testBlock))
		require.Equal(t, []string{
			"api call -s " + testSession + ` session.describe_block {"block_id":"` + testBlock + `"}`,
			"api call -s " + testSession + ` session.focus_block {"block_id":"` + testBlock + `"}`,
			`api call client.list {"kinds":["app"]}`,
			`api call client.perform {"action":"session.select","args":{"session_id":"` + testSession +
				`","window_id":"` + testWindow + `"},"client_id":"` + testApp + `"}`,
		}, calls())
	})

	t.Run("an app with Remote Control off", func(t *testing.T) {
		refusal := `Error: stream response "control-1" failed: Rex.app is not accepting actions. ` +
			`Turn on Remote Control in its Settings under Rex Server. (status=403 code=refused)`
		client, _ := fakeRex(t, describe, focus, apps, reply{match: "client.perform", stderr: refusal, exit: 1})

		err := client.Show(t.Context(), testSession, testBlock)
		require.ErrorIs(t, err, rex.ErrRemoteControlOff)
		require.ErrorContains(t, err, "Turn on Remote Control in its Settings under Rex Server.")
	})
}

func TestCapture(t *testing.T) {
	client, calls := fakeRex(t, reply{match: "capture", file: "capture.txt"})

	text, err := client.Capture(t.Context(), testSession, testBlock)
	require.NoError(t, err)
	require.Equal(t, "https://github.com/example/repo/pull/12\n", text)
	require.Equal(t, []string{"capture -s " + testSession + " -b " + testBlock + " --trim --unwrap"}, calls())
}

func TestCallHonorsContext(t *testing.T) {
	client, _ := fakeRex(t, reply{match: "server.status", exec: "sleep 30"})
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	_, err := client.Server(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
